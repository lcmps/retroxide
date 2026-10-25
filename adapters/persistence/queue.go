package persistence

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lcmps/retroxide/internal/features/jobs"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (repositories *Repositories) Enqueue(ctx context.Context, payload jobs.Payload) error {
	if err := repositories.DB.WithContext(ctx).Exec(
		"INSERT INTO job_queue (kind, upload_id, rom_file_id, attempt, available_at) VALUES (?, ?, ?, ?, ?)",
		payload.Kind, payload.UploadID, payload.ROMFileID, payload.Attempt, sqlTimestamp(time.Now()),
	).Error; err != nil {
		return databaseError("enqueueing job", err)
	}
	return nil
}

func (repositories *Repositories) Claim(ctx context.Context, now time.Time, claimTimeout time.Duration) (*jobs.Job, error) {
	var claimed *jobs.Job
	currentTimestamp := sqlTimestamp(now)
	expiredBefore := sqlTimestamp(now.Add(-claimTimeout))
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("available_at <= ? AND (claimed_at IS NULL OR claimed_at <= ?)", currentTimestamp, expiredBefore).Order("id ASC").Limit(1)
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var model JobQueueModel
		if err := query.Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("selecting available queue job: %w", err)
		}
		result := tx.Model(&JobQueueModel{}).Where("id = ?", model.ID).Update("claimed_at", currentTimestamp)
		if result.Error != nil {
			return fmt.Errorf("marking queue job claimed: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return nil
		}
		claimed = &jobs.Job{
			ID: strconv.FormatUint(model.ID, 10),
			Payload: jobs.Payload{
				Kind: model.Kind, UploadID: model.UploadID, ROMFileID: model.ROMFileID, Attempt: model.Attempt,
			},
		}
		return nil
	})
	if err != nil {
		return nil, databaseError("claiming queue job", err)
	}
	return claimed, nil
}

func (repositories *Repositories) Complete(ctx context.Context, jobID string) error {
	id, err := parseQueueID(jobID)
	if err != nil {
		return fault.Validation("invalid job id")
	}
	result := repositories.DB.WithContext(ctx).Where("id = ?", id).Delete(&JobQueueModel{})
	if result.Error != nil {
		return databaseError("completing queue job", result.Error)
	}
	return nil
}

func (repositories *Repositories) Reschedule(ctx context.Context, jobID string, attempt uint8, notBefore time.Time) error {
	id, err := parseQueueID(jobID)
	if err != nil {
		return fault.Validation("invalid job id")
	}
	updates := map[string]any{"attempt": attempt, "claimed_at": nil, "available_at": sqlTimestamp(notBefore)}
	if err := repositories.DB.WithContext(ctx).Model(&JobQueueModel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return databaseError("rescheduling queue job", err)
	}
	return nil
}

func (repositories *Repositories) DeadLetter(ctx context.Context, job jobs.Job, attempt uint8, reason string) error {
	id, err := parseQueueID(job.ID)
	if err != nil {
		return fault.Validation("invalid job id")
	}
	return repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deadLetter := JobDeadLetterModel{
			Kind: job.Payload.Kind, UploadID: job.Payload.UploadID, ROMFileID: job.Payload.ROMFileID,
			Attempt: attempt, Reason: reason, DeadLetteredAt: sqlTimestamp(time.Now()),
		}
		if err := tx.Create(&deadLetter).Error; err != nil {
			return databaseError("inserting dead-letter job", err)
		}
		if err := tx.Where("id = ?", id).Delete(&JobQueueModel{}).Error; err != nil {
			return databaseError("removing dead-lettered queue job", err)
		}
		return nil
	})
}

func (repositories *Repositories) ListDeadLetter(ctx context.Context, limit int, cursor *string) (jobs.DeadLetterPage, error) {
	if limit < 1 {
		return jobs.DeadLetterPage{}, fault.Validation("limit must be greater than zero")
	}
	if limit > 100 {
		limit = 100
	}
	query := repositories.DB.WithContext(ctx).Model(&JobDeadLetterModel{})
	if cursor != nil {
		cursorID, err := parseQueueID(*cursor)
		if err != nil {
			return jobs.DeadLetterPage{}, fault.Validation("invalid dead-letter cursor")
		}
		query = query.Where("id > ?", cursorID)
	}
	var models []JobDeadLetterModel
	if err := query.Order("id ASC").Limit(limit + 1).Find(&models).Error; err != nil {
		return jobs.DeadLetterPage{}, databaseError("listing dead-letter jobs", err)
	}
	page := jobs.DeadLetterPage{Item: make([]jobs.DeadLetterJobRecord, 0, min(len(models), limit))}
	if len(models) > limit {
		lastID := strconv.FormatUint(models[limit-1].ID, 10)
		page.NextCursor = &lastID
		models = models[:limit]
	}
	for _, model := range models {
		page.Item = append(page.Item, jobs.DeadLetterJobRecord{
			ID:      strconv.FormatUint(model.ID, 10),
			Payload: jobs.Payload{Kind: model.Kind, UploadID: model.UploadID, ROMFileID: model.ROMFileID, Attempt: model.Attempt},
			Reason:  model.Reason,
		})
	}
	return page, nil
}

func (repositories *Repositories) RetryDeadLetter(ctx context.Context, deadLetterID string) (bool, error) {
	id, err := parseQueueID(deadLetterID)
	if err != nil {
		return false, fault.Validation("invalid dead-letter id")
	}
	retried := false
	err = repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model JobDeadLetterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("finding dead-letter job: %w", err)
		}
		queued := JobQueueModel{Kind: model.Kind, UploadID: model.UploadID, ROMFileID: model.ROMFileID, Attempt: 0, AvailableAt: sqlTimestamp(time.Now())}
		if err := tx.Create(&queued).Error; err != nil {
			return fmt.Errorf("re-enqueueing dead-letter job: %w", err)
		}
		if err := tx.Delete(&model).Error; err != nil {
			return fmt.Errorf("removing retried dead-letter job: %w", err)
		}
		retried = true
		return nil
	})
	if err != nil {
		return false, databaseError("retrying dead-letter job", err)
	}
	return retried, nil
}

func sqlTimestamp(value time.Time) string {
	return value.UTC().Format(sqlTimestampLayout)
}

func parseQueueID(value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid queue id %q", value)
	}
	return id, nil
}

var _ jobs.Queue = (*Repositories)(nil)

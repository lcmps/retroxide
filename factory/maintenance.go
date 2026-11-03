package factory

import (
	"context"
	"log/slog"
	"time"

	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const (
	// maintenanceDelay lets startup settle before the first round of maintenance.
	maintenanceDelay = time.Minute
	// maintenanceInterval is how often maintenance runs afterwards.
	maintenanceInterval = 6 * time.Hour
	// unreferencedGrace is how long a stored file may go unreferenced before it is removed. An
	// upload stores its file a moment before recording it, so recent files are left alone.
	unreferencedGrace = time.Hour
	// idleSessionAge is how long an upload sent in parts may go without a new part before it is
	// abandoned and its reserved space freed.
	idleSessionAge = 24 * time.Hour
)

// maintenanceTask is one periodic chore.
type maintenanceTask func(ctx context.Context, logger *slog.Logger)

// runMaintenance runs tasks after maintenanceDelay and then every maintenanceInterval, until
// ctx ends.
func runMaintenance(ctx context.Context, logger *slog.Logger, tasks ...maintenanceTask) {
	timer := time.NewTimer(maintenanceDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for _, task := range tasks {
			if ctx.Err() != nil {
				return
			}
			task(ctx, logger)
		}
		timer.Reset(maintenanceInterval)
	}
}

// sessionExpirer ends idle uploads sent in parts.
type sessionExpirer interface {
	ExpireSessions(ctx context.Context, before time.Time) (int, error)
}

// expireUploadSessions returns a task ending uploads sent in parts that have been idle for
// idleSessionAge, freeing the space reserved for them.
func expireUploadSessions(uploads sessionExpirer) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		expired, err := uploads.ExpireSessions(ctx, time.Now().Add(-idleSessionAge))
		if err != nil {
			logger.Warn("could not expire idle uploads", "error", err)
			return
		}
		if expired > 0 {
			logger.Info("expired idle uploads", "sessions", expired)
		}
	}
}

// sweepUnreferencedFiles returns a task removing stored files that no ROM or image references
// any more, such as a cover and its thumbnail after the cover was replaced, or a file whose
// deletion failed after its ROM was deleted.
func sweepUnreferencedFiles(repositories *persistence.Repositories, files *storage.Filesystem) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		removed, err := removeUnreferencedFiles(ctx, repositories, files, time.Now().Add(-unreferencedGrace))
		if err != nil {
			logger.Warn("could not sweep unreferenced files", "error", err, "removed", removed)
			return
		}
		if removed > 0 {
			logger.Info("removed unreferenced files", "files", removed)
		}
	}
}

// removeUnreferencedFiles removes stored files last written before cutoff that nothing
// references, checking references a batch at a time, and returns how many it removed.
func removeUnreferencedFiles(ctx context.Context, repositories *persistence.Repositories, files *storage.Filesystem, cutoff time.Time) (int, error) {
	removed := 0
	var batch []string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		referenced, err := repositories.ReferencedStoragePaths(ctx, batch)
		if err != nil {
			return err
		}
		for _, key := range batch {
			if !referenced[key] && files.Delete(ctx, key) == nil {
				removed++
			}
		}
		batch = batch[:0]
		return nil
	}
	err := files.WalkKeys(ctx, func(stored storage.StoredKey) error {
		if stored.Modified.After(cutoff) {
			return nil
		}
		batch = append(batch, stored.Key)
		if len(batch) < relocateBatchSize {
			return nil
		}
		return flush()
	})
	if err == nil {
		err = flush()
	}
	return removed, err
}

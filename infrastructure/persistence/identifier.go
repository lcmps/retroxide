package persistence

import (
	"context"
	"errors"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/uploads"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Identifier struct {
	database *gorm.DB
}

func NewIdentifier(database *gorm.DB) *Identifier {
	return &Identifier{database: database}
}

func (identifier *Identifier) Identify(ctx context.Context, uploadID, romFileID string) error {
	err := identifier.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var file ROMFileModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", romFileID).Take(&file).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var upload UploadModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", uploadID).Take(&upload).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		if upload.Status != string(uploadsdomain.UploadStatusProcessing) {
			return nil
		}
		repositories := NewRepositories(tx)
		return uploads.NewIdentifyService(repositories, repositories).Identify(ctx, uploadID, romFileID)
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return err
		}
		return databaseError("identifying ROM transactionally", err)
	}
	return nil
}

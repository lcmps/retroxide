package persistence

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/uploads"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Identifier struct {
	database *gorm.DB
	discs    DiscIndexer
}

// DiscIndexer recognizes disc images among uploaded files.
type DiscIndexer interface {
	// Index records a CHD's tracks; a disc the library already has is ROMAlreadyExists.
	Index(ctx context.Context, romFile romdomain.ROMFile, base string) error
}

func NewIdentifier(database *gorm.DB) *Identifier {
	return &Identifier{database: database}
}

// WithDiscs makes identification index disc images first, refusing discs already in the library.
func (identifier *Identifier) WithDiscs(discs DiscIndexer) *Identifier {
	identifier.discs = discs
	return identifier
}

func (identifier *Identifier) Identify(ctx context.Context, uploadID, romFileID string) error {
	if identifier.discs != nil {
		// Indexing reads the whole image, so it runs before, not inside, the transaction below:
		// holding its locks for minutes would stall every other write.
		duplicate, err := identifier.indexDisc(ctx, uploadID, romFileID)
		if err != nil {
			return err
		}
		if duplicate {
			return identifier.refuseDuplicateDisc(ctx, uploadID, romFileID)
		}
	}
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

// indexDisc indexes the uploaded file if it may be a disc image, reporting a disc the library
// already holds in another file.
func (identifier *Identifier) indexDisc(ctx context.Context, uploadID, romFileID string) (bool, error) {
	repositories := NewRepositories(identifier.database)
	romFile, err := repositories.FindROMFileByID(ctx, romFileID)
	if err != nil || romFile == nil {
		return false, err
	}
	upload, err := repositories.FindUpload(ctx, uploadID)
	if err != nil || upload == nil || upload.Status != uploadsdomain.UploadStatusProcessing {
		return false, err
	}
	base := strings.TrimSuffix(upload.OriginalFilename, path.Ext(upload.OriginalFilename))
	err = identifier.discs.Index(ctx, *romFile, base)
	if fault.IsCode(err, fault.CodeROMAlreadyExists) {
		return true, nil
	}
	return false, err
}

// refuseDuplicateDisc fails an upload whose disc the library already has, and forgets its file.
func (identifier *Identifier) refuseDuplicateDisc(ctx context.Context, uploadID, romFileID string) error {
	err := identifier.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repositories := NewRepositories(tx)
		upload, err := repositories.FindUpload(ctx, uploadID)
		if err != nil || upload == nil || upload.Status != uploadsdomain.UploadStatusProcessing {
			return err
		}
		if err := upload.TransitionTo(uploadsdomain.UploadStatusFailed); err != nil {
			return fault.Internal(err)
		}
		message := "This disc is already in the library."
		upload.ErrorMessage = &message
		if err := repositories.SaveUpload(ctx, *upload); err != nil {
			return err
		}
		return repositories.DeleteUnusedROMFile(ctx, romFileID)
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return err
		}
		return databaseError("refusing a duplicate disc", err)
	}
	return nil
}

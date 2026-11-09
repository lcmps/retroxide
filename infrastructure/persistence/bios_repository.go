package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveBIOS records a new BIOS entry. A file already recorded as a BIOS is bios_already_exists.
func (repositories *Repositories) SaveBIOS(ctx context.Context, entry biosdomain.BIOS) error {
	model := biosModel(entry)
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		if existing, lookupErr := repositories.FindBIOSByROMFileID(ctx, entry.ROMFileID); lookupErr == nil && existing != nil {
			return fault.BIOSAlreadyExists(existing.ID, existing.Hash.SHA256)
		}
		return databaseError("saving bios", err)
	}
	return nil
}

// UpdateBIOS writes an entry's platform, labels and recognition. It never changes the file, the
// uploader or the uploaded filename.
func (repositories *Repositories) UpdateBIOS(ctx context.Context, entry biosdomain.BIOS) error {
	model := biosModel(entry)
	result := repositories.DB.WithContext(ctx).Model(&BIOSModel{}).Where("id = ?", entry.ID).
		Select("platform", "expected_path", "recognized", "region", "version", "model", "release_date", "note").
		Updates(&model)
	if result.Error != nil {
		return databaseError("updating bios", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func (repositories *Repositories) FindBIOS(ctx context.Context, id string) (*biosdomain.BIOS, error) {
	return repositories.findOneBIOS(ctx, "b.id = ?", id)
}

func (repositories *Repositories) FindBIOSByROMFileID(ctx context.Context, romFileID string) (*biosdomain.BIOS, error) {
	return repositories.findOneBIOS(ctx, "b.rom_file_id = ?", romFileID)
}

// ListBIOS lists BIOS entries, of one platform when platform is set, by platform, region,
// version and filename.
func (repositories *Repositories) ListBIOS(ctx context.Context, platform *string) ([]biosdomain.BIOS, error) {
	query := repositories.biosQuery(ctx)
	if platform != nil {
		query = query.Where("b.platform = ?", *platform)
	}
	return scanBIOS(query.Order("b.platform, b.region, b.version, b.filename"), "listing bios")
}

// ListUnrecognizedBIOS lists the entries the known list did not recognize, with their hashes, to
// be checked again against a newer list.
func (repositories *Repositories) ListUnrecognizedBIOS(ctx context.Context) ([]biosdomain.BIOS, error) {
	return scanBIOS(repositories.biosQuery(ctx).Where("b.recognized = ?", false).Order("b.id"), "listing unrecognized bios")
}

// HardDeleteBIOS deletes a BIOS entry and its uploads, and the stored file with its jobs unless a
// game still uses it.
func (repositories *Repositories) HardDeleteBIOS(ctx context.Context, id string, deletePath func(string) error) error {
	var orphanPaths []string
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry BIOSModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var file ROMFileModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", entry.ROMFileID).Take(&file).Error; err != nil {
			return fmt.Errorf("finding the bios file: %w", err)
		}
		if err := tx.Delete(&entry).Error; err != nil {
			return err
		}
		games, _, err := fileUses(tx, file.ID, "")
		if err != nil {
			return err
		}
		var unshared []string
		if games == 0 {
			unshared = []string{file.ID}
		}
		if err := deleteJobsAndUploads(tx, tx.Model(&UploadModel{}).Where("bios_id = ?", id), unshared); err != nil {
			return err
		}
		paths, err := deleteUnsharedDiscFile(tx, file)
		if err != nil {
			return err
		}
		orphanPaths, err = orphanedPaths(tx, paths)
		return err
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return err
		}
		return databaseError("deleting bios", err)
	}
	repositories.removeOrphans(ctx, orphanPaths, deletePath)
	return nil
}

func (repositories *Repositories) findOneBIOS(ctx context.Context, condition string, value string) (*biosdomain.BIOS, error) {
	entries, err := scanBIOS(repositories.biosQuery(ctx).Where(condition, value).Limit(1), "finding bios")
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return &entries[0], nil
}

func (repositories *Repositories) biosQuery(ctx context.Context) *gorm.DB {
	return repositories.DB.WithContext(ctx).Table("bios AS b").
		Joins("JOIN rom_file AS f ON f.id = b.rom_file_id").
		Select("b.*, f.sha256, f.sha1, f.md5, f.crc32, f.size AS file_size")
}

type biosRow struct {
	BIOSModel
	SHA256   string `gorm:"column:sha256"`
	SHA1     string `gorm:"column:sha1"`
	MD5      string `gorm:"column:md5"`
	CRC32    string `gorm:"column:crc32"`
	FileSize uint64 `gorm:"column:file_size"`
}

func scanBIOS(query *gorm.DB, operation string) ([]biosdomain.BIOS, error) {
	var rows []biosRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, databaseError(operation, err)
	}
	entries := make([]biosdomain.BIOS, 0, len(rows))
	for _, row := range rows {
		hash, err := romdomain.NewROMHash(row.SHA256, row.SHA1, row.MD5, row.CRC32)
		if err != nil {
			return nil, fault.Internal(fmt.Errorf("invalid persisted bios hash: %w", err))
		}
		entry := biosEntity(row.BIOSModel)
		entry.Hash, entry.Size = hash, row.FileSize
		entries = append(entries, entry)
	}
	return entries, nil
}

func biosModel(entry biosdomain.BIOS) BIOSModel {
	return BIOSModel{
		ID: entry.ID, ROMFileID: entry.ROMFileID, UploaderID: entry.UploaderID, Platform: entry.Platform,
		Filename: entry.Filename, ExpectedPath: entry.ExpectedPath, Recognized: entry.Recognized,
		Region: entry.Labels.Region, Version: entry.Labels.Version, Model: entry.Labels.Model,
		ReleaseDate: entry.Labels.Date, Note: entry.Labels.Note,
	}
}

func biosEntity(model BIOSModel) biosdomain.BIOS {
	return biosdomain.BIOS{
		ID: model.ID, ROMFileID: model.ROMFileID, UploaderID: model.UploaderID, Platform: model.Platform,
		Filename: model.Filename, ExpectedPath: model.ExpectedPath, Recognized: model.Recognized,
		Labels: biosdomain.Labels{Region: model.Region, Version: model.Version, Model: model.Model, Date: model.ReleaseDate, Note: model.Note},
	}
}

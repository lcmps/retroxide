package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HardDeleteROM deletes a ROM and everything recorded for it, then calls deletePath for each
// stored file nothing references any more. Files are removed only after the deletion commits:
// a failed commit then leaves every record with its file, never a record whose file is gone. A
// file deletePath fails to remove is left for the storage sweep, and does not fail the deletion.
func (repositories *Repositories) HardDeleteROM(ctx context.Context, romID string, deletePath func(string) error) error {
	var orphanPaths []string
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rom ROMModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", romID).Take(&rom).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var file ROMFileModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rom.ROMFileID).Take(&file).Error; err != nil {
			return fmt.Errorf("finding ROM file for hard delete: %w", err)
		}
		var remaining int64
		if err := tx.Model(&ROMModel{}).Where("rom_file_id = ? AND id <> ?", file.ID, romID).Count(&remaining).Error; err != nil {
			return err
		}
		uploadQuery := tx.Model(&UploadModel{}).Where("rom_id = ?", romID)
		if remaining == 0 {
			queuedUploads := tx.Model(&JobQueueModel{}).Select("upload_id").Where("rom_file_id = ?", file.ID)
			deadUploads := tx.Model(&JobDeadLetterModel{}).Select("upload_id").Where("rom_file_id = ?", file.ID)
			uploadQuery = uploadQuery.Or("id IN (?) OR id IN (?)", queuedUploads, deadUploads)
		}
		var uploadIDs []string
		if err := uploadQuery.Pluck("id", &uploadIDs).Error; err != nil {
			return err
		}
		for _, model := range []any{&JobQueueModel{}, &JobDeadLetterModel{}} {
			query := tx.Where("upload_id IN ?", uploadIDs)
			if remaining == 0 {
				query = query.Or("rom_file_id = ?", file.ID)
			}
			if err := query.Delete(model).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("id IN ?", uploadIDs).Delete(&UploadModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rom_id = ?", romID).Delete(&LibraryItemModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rom_id = ?", romID).Delete(&ROMDiscModel{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&rom).Error; err != nil {
			return err
		}
		var trackPaths []string
		if remaining == 0 {
			// A disc's tracks go with its file, along with any track files stored separately.
			if err := tx.Model(&DiscTrackModel{}).Where("rom_file_id = ? AND storage_path IS NOT NULL", file.ID).Pluck("storage_path", &trackPaths).Error; err != nil {
				return err
			}
			if err := tx.Where("rom_file_id = ?", file.ID).Delete(&DiscTrackModel{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&file).Error; err != nil {
				return err
			}
		}
		paths := []string{}
		for _, path := range []*string{rom.CoverArtPath, rom.CoverThumbPath, rom.TitleScreenPath, rom.ScreenshotPath} {
			if path != nil && *path != "" {
				paths = append(paths, *path)
			}
		}
		if remaining == 0 {
			paths = append(paths, file.StoragePath)
			paths = append(paths, trackPaths...)
			if file.SheetPath != nil {
				paths = append(paths, *file.SheetPath)
			}
		}
		seen := make(map[string]bool)
		for _, path := range paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			var references int64
			if err := tx.Model(&ROMModel{}).Where("cover_art_path = ? OR cover_thumb_path = ? OR title_screen_path = ? OR screenshot_path = ?", path, path, path, path).Count(&references).Error; err != nil {
				return err
			}
			if references != 0 {
				continue
			}
			if err := tx.Model(&ROMFileModel{}).Where("storage_path = ? OR sheet_path = ?", path, path).Count(&references).Error; err != nil {
				return err
			}
			if references == 0 {
				if err := tx.Model(&DiscTrackModel{}).Where("storage_path = ?", path).Count(&references).Error; err != nil {
					return err
				}
			}
			if references == 0 {
				orphanPaths = append(orphanPaths, path)
			}
		}
		return nil
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return err
		}
		return databaseError("hard deleting ROM", err)
	}
	// An upload of the same content may have stored and recorded the file again since.
	referenced, err := repositories.ReferencedStoragePaths(context.WithoutCancel(ctx), orphanPaths)
	if err != nil {
		return nil // the deletion stands; the storage sweep reclaims what is left
	}
	for _, path := range orphanPaths {
		if !referenced[path] {
			_ = deletePath(path)
		}
	}
	return nil
}

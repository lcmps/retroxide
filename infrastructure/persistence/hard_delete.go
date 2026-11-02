package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (repositories *Repositories) HardDeleteROM(ctx context.Context, romID string, deletePath func(string) error) error {
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
		if err := tx.Delete(&rom).Error; err != nil {
			return err
		}
		if remaining == 0 {
			if err := tx.Delete(&file).Error; err != nil {
				return err
			}
		}
		paths := []string{}
		for _, path := range []*string{rom.CoverArtPath, rom.TitleScreenPath, rom.ScreenshotPath} {
			if path != nil && *path != "" {
				paths = append(paths, *path)
			}
		}
		if remaining == 0 {
			paths = append(paths, file.StoragePath)
		}
		seen := make(map[string]bool)
		var orphanPaths []string
		for _, path := range paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			var references int64
			if err := tx.Model(&ROMModel{}).Where("cover_art_path = ? OR title_screen_path = ? OR screenshot_path = ?", path, path, path).Count(&references).Error; err != nil {
				return err
			}
			if references != 0 {
				continue
			}
			if err := tx.Model(&ROMFileModel{}).Where("storage_path = ?", path).Count(&references).Error; err != nil {
				return err
			}
			if references == 0 {
				orphanPaths = append(orphanPaths, path)
			}
		}
		for _, path := range orphanPaths {
			if err := deletePath(path); err != nil {
				return err
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
	return nil
}

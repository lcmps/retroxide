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
// Every disc of the ROM goes with it, except a file another ROM still has as a disc.
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
		var discs []ROMDiscModel
		if err := tx.Where("rom_id = ?", romID).Order("number ASC").Find(&discs).Error; err != nil {
			return err
		}
		files := make([]ROMFileModel, 0, len(discs))
		for _, disc := range discs {
			var file ROMFileModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", disc.ROMFileID).Take(&file).Error; err != nil {
				return fmt.Errorf("finding ROM file for hard delete: %w", err)
			}
			files = append(files, file)
		}
		if len(files) == 0 {
			// A ROM with no disc rows still has its primary file.
			var file ROMFileModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rom.ROMFileID).Take(&file).Error; err != nil {
				return fmt.Errorf("finding ROM file for hard delete: %w", err)
			}
			files = append(files, file)
		}
		// Uploads of the ROM go whichever disc they brought.
		unshared, err := unsharedFileIDs(tx, romID, files)
		if err != nil {
			return err
		}
		if err := deleteJobsAndUploads(tx, tx.Model(&UploadModel{}).Where("rom_id = ?", romID), unshared); err != nil {
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
		paths := []string{}
		for _, path := range []*string{rom.CoverArtPath, rom.CoverThumbPath, rom.TitleScreenPath, rom.ScreenshotPath} {
			if path != nil && *path != "" {
				paths = append(paths, *path)
			}
		}
		for _, file := range files {
			filePaths, err := deleteUnsharedDiscFile(tx, file)
			if err != nil {
				return err
			}
			paths = append(paths, filePaths...)
		}
		orphanPaths, err = orphanedPaths(tx, paths)
		return err
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return err
		}
		return databaseError("hard deleting ROM", err)
	}
	repositories.removeOrphans(ctx, orphanPaths, deletePath)
	return nil
}

// HardDeleteROMDisc removes one disc from a ROM that has several: its record, its tracks and its
// file, unless another ROM still has that file as a disc. The ROM keeps its other discs, their
// numbers and its covers; its primary file becomes the lowest-numbered remaining disc's. The last
// disc is refused, because that is deleting the ROM. Files go only after the commit.
func (repositories *Repositories) HardDeleteROMDisc(ctx context.Context, romID, romFileID string, deletePath func(string) error) error {
	var orphanPaths []string
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rom ROMModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", romID).Take(&rom).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var discs []ROMDiscModel
		if err := tx.Where("rom_id = ?", romID).Order("number ASC").Find(&discs).Error; err != nil {
			return err
		}
		var disc *ROMDiscModel
		for index := range discs {
			if discs[index].ROMFileID == romFileID {
				disc = &discs[index]
			}
		}
		if disc == nil {
			return fault.NotFound()
		}
		if len(discs) == 1 {
			return fault.Validation("a ROM keeps at least one disc; delete the ROM instead")
		}
		var file ROMFileModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", romFileID).Take(&file).Error; err != nil {
			return fmt.Errorf("finding ROM file for hard delete: %w", err)
		}
		// Only the upload that brought this disc: the others brought the ROM's other discs.
		uploads := uploadNamedForDisc(tx, romID, *disc, discs)
		unshared, err := unsharedFileIDs(tx, romID, []ROMFileModel{file})
		if err != nil {
			return err
		}
		if err := deleteJobsAndUploads(tx, uploads, unshared); err != nil {
			return err
		}
		if err := tx.Where("rom_id = ? AND rom_file_id = ?", romID, romFileID).Delete(&ROMDiscModel{}).Error; err != nil {
			return err
		}
		// The ROM must stop pointing at the file before the file row can go.
		if err := setPrimaryDisc(tx, romID); err != nil {
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
		return databaseError("hard deleting ROM disc", err)
	}
	repositories.removeOrphans(ctx, orphanPaths, deletePath)
	return nil
}

// unsharedFileIDs returns the files of files that no ROM other than romID has as a disc.
func unsharedFileIDs(tx *gorm.DB, romID string, files []ROMFileModel) ([]string, error) {
	ids := []string{}
	for _, file := range files {
		var others int64
		if err := tx.Model(&ROMDiscModel{}).Where("rom_file_id = ? AND rom_id <> ?", file.ID, romID).Count(&others).Error; err != nil {
			return nil, err
		}
		if others == 0 {
			ids = append(ids, file.ID)
		}
	}
	return ids, nil
}

// deleteJobsAndUploads deletes the uploads uploadQuery selects, the jobs of those uploads, and the
// jobs of unsharedFiles, with the uploads those jobs belong to.
func deleteJobsAndUploads(tx *gorm.DB, uploadQuery *gorm.DB, unsharedFiles []string) error {
	if len(unsharedFiles) > 0 {
		queuedUploads := tx.Model(&JobQueueModel{}).Select("upload_id").Where("rom_file_id IN ?", unsharedFiles)
		deadUploads := tx.Model(&JobDeadLetterModel{}).Select("upload_id").Where("rom_file_id IN ?", unsharedFiles)
		uploadQuery = uploadQuery.Or("id IN (?) OR id IN (?)", queuedUploads, deadUploads)
	}
	var uploadIDs []string
	if err := uploadQuery.Pluck("id", &uploadIDs).Error; err != nil {
		return err
	}
	for _, model := range []any{&JobQueueModel{}, &JobDeadLetterModel{}} {
		query := tx.Where("upload_id IN ?", uploadIDs)
		if len(unsharedFiles) > 0 {
			query = query.Or("rom_file_id IN ?", unsharedFiles)
		}
		if err := query.Delete(model).Error; err != nil {
			return err
		}
	}
	return tx.Where("id IN ?", uploadIDs).Delete(&UploadModel{}).Error
}

// deleteUnsharedDiscFile deletes a file's tracks and record, and returns the paths it stored,
// unless a ROM still has the file as a disc: then it deletes nothing. The caller has already
// removed its own disc row, so any row left belongs to another ROM.
func deleteUnsharedDiscFile(tx *gorm.DB, file ROMFileModel) ([]string, error) {
	var remaining int64
	if err := tx.Model(&ROMDiscModel{}).Where("rom_file_id = ?", file.ID).Count(&remaining).Error; err != nil {
		return nil, err
	}
	if remaining != 0 {
		return nil, nil
	}
	// A disc's tracks go with its file, along with any track files stored separately.
	var trackPaths []string
	if err := tx.Model(&DiscTrackModel{}).Where("rom_file_id = ? AND storage_path IS NOT NULL", file.ID).Pluck("storage_path", &trackPaths).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("rom_file_id = ?", file.ID).Delete(&DiscTrackModel{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Delete(&file).Error; err != nil {
		return nil, err
	}
	paths := append([]string{file.StoragePath}, trackPaths...)
	if file.SheetPath != nil {
		paths = append(paths, *file.SheetPath)
	}
	return paths, nil
}

// orphanedPaths returns the paths nothing in the database references any more.
func orphanedPaths(tx *gorm.DB, paths []string) ([]string, error) {
	var orphans []string
	seen := make(map[string]bool)
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		var references int64
		if err := tx.Model(&ROMModel{}).Where("cover_art_path = ? OR cover_thumb_path = ? OR title_screen_path = ? OR screenshot_path = ?", path, path, path, path).Count(&references).Error; err != nil {
			return nil, err
		}
		if references != 0 {
			continue
		}
		if err := tx.Model(&ROMFileModel{}).Where("storage_path = ? OR sheet_path = ?", path, path).Count(&references).Error; err != nil {
			return nil, err
		}
		if references == 0 {
			if err := tx.Model(&DiscTrackModel{}).Where("storage_path = ?", path).Count(&references).Error; err != nil {
				return nil, err
			}
		}
		if references == 0 {
			orphans = append(orphans, path)
		}
	}
	return orphans, nil
}

// removeOrphans deletes the committed deletion's orphaned files.
func (repositories *Repositories) removeOrphans(ctx context.Context, orphanPaths []string, deletePath func(string) error) {
	// An upload of the same content may have stored and recorded the file again since.
	referenced, err := repositories.ReferencedStoragePaths(context.WithoutCancel(ctx), orphanPaths)
	if err != nil {
		return // the deletion stands; the storage sweep reclaims what is left
	}
	for _, path := range orphanPaths {
		if !referenced[path] {
			_ = deletePath(path)
		}
	}
}

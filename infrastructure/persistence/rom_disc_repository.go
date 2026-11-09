package persistence

import (
	"context"
	"errors"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FindROMDiscs returns a ROM's discs ordered by number.
func (repositories *Repositories) FindROMDiscs(ctx context.Context, romID string) ([]romdomain.ROMDisc, error) {
	var models []ROMDiscModel
	if err := repositories.DB.WithContext(ctx).Where("rom_id = ?", romID).Order("number ASC").Find(&models).Error; err != nil {
		return nil, databaseError("finding rom discs", err)
	}
	discs := make([]romdomain.ROMDisc, 0, len(models))
	for _, model := range models {
		discs = append(discs, romdomain.ROMDisc{ROMID: model.ROMID, ROMFileID: model.ROMFileID, Number: model.Number, Filename: model.Filename})
	}
	return discs, nil
}

// FindROMByDiscGroup returns the uploader's ROM that has the group key, or nil.
func (repositories *Repositories) FindROMByDiscGroup(ctx context.Context, uploaderID, group string) (*romdomain.ROM, error) {
	model, err := takeOne[ROMModel](repositories.DB.WithContext(ctx).Where("uploader_id = ? AND disc_group = ?", uploaderID, group), "finding rom by disc group")
	return mapOptional(model, err, infallible(romEntity))
}

// RecordDisc inserts a disc or, when the ROM already has that file as a disc, updates its number
// and filename. The ROM's primary file then becomes the lowest-numbered disc's.
func (repositories *Repositories) RecordDisc(ctx context.Context, disc romdomain.ROMDisc) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rom_id = ? AND rom_file_id = ?", disc.ROMID, disc.ROMFileID).Delete(&ROMDiscModel{}).Error; err != nil {
			return err
		}
		model := ROMDiscModel{ROMID: disc.ROMID, ROMFileID: disc.ROMFileID, Number: disc.Number, Filename: disc.Filename}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return setPrimaryDisc(tx, disc.ROMID)
	})
	if err != nil {
		return databaseError("recording rom disc", err)
	}
	return nil
}

// setPrimaryDisc points rom.rom_file_id at the lowest-numbered disc's file.
func setPrimaryDisc(tx *gorm.DB, romID string) error {
	var first ROMDiscModel
	if err := tx.Where("rom_id = ?", romID).Order("number ASC").Take(&first).Error; err != nil {
		return err
	}
	return tx.Model(&ROMModel{}).Where("id = ?", romID).Update("rom_file_id", first.ROMFileID).Error
}

// lockROMs locks the ROMs in ID order, so two merges of the same pair cannot deadlock, and
// returns them by ID. A missing ROM is not found.
func lockROMs(tx *gorm.DB, ids ...string) (map[string]ROMModel, error) {
	var models []ROMModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	roms := make(map[string]ROMModel, len(models))
	for _, model := range models {
		roms[model.ID] = model
	}
	for _, id := range ids {
		if _, ok := roms[id]; !ok {
			return nil, fault.NotFound()
		}
	}
	return roms, nil
}

func findDiscs(tx *gorm.DB, romID string) ([]ROMDiscModel, error) {
	var discs []ROMDiscModel
	err := tx.Where("rom_id = ?", romID).Order("number ASC").Find(&discs).Error
	return discs, err
}

// uploadNamedForDisc selects the upload that brought a disc: the ROM's upload with the disc's
// filename. When another disc of the ROM has the same filename (after a merge, two discs can both
// come from game.iso) the upload cannot be told apart, and nothing is selected: the upload stays
// attached to the ROM rather than risk another disc's.
func uploadNamedForDisc(tx *gorm.DB, romID string, disc ROMDiscModel, discs []ROMDiscModel) *gorm.DB {
	if disc.Filename == "" {
		return tx.Model(&UploadModel{}).Where("1 = 0")
	}
	for _, other := range discs {
		if other.ROMFileID != disc.ROMFileID && other.Filename == disc.Filename {
			return tx.Model(&UploadModel{}).Where("1 = 0")
		}
	}
	return tx.Model(&UploadModel{}).Where("rom_id = ? AND original_filename = ?", romID, disc.Filename)
}

func asDatabaseError(action string, err error) error {
	var appError *fault.Error
	if errors.As(err, &appError) {
		return err
	}
	return databaseError(action, err)
}

// MergeROMs moves every disc of the source ROM onto the target and deletes the source. A disc
// the target already has is dropped; a disc whose number the target uses takes the lowest free
// number, and the others keep theirs. The source's uploads and library entries move to the
// target, and its group key too if the target has none. Images only the source used go to
// deletePath after the commit.
func (repositories *Repositories) MergeROMs(ctx context.Context, targetID, sourceID string, deletePath func(string) error) error {
	var orphanPaths []string
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roms, err := lockROMs(tx, targetID, sourceID)
		if err != nil {
			return err
		}
		target, source := roms[targetID], roms[sourceID]
		targetDiscs, err := findDiscs(tx, targetID)
		if err != nil {
			return err
		}
		sourceDiscs, err := findDiscs(tx, sourceID)
		if err != nil {
			return err
		}
		used := map[int]bool{}
		hasFile := map[string]bool{}
		for _, disc := range targetDiscs {
			used[disc.Number] = true
			hasFile[disc.ROMFileID] = true
		}
		var moving, renumbered []ROMDiscModel
		for _, disc := range sourceDiscs {
			switch {
			case hasFile[disc.ROMFileID]:
			case used[disc.Number]:
				renumbered = append(renumbered, disc)
			default:
				used[disc.Number] = true
				moving = append(moving, disc)
			}
		}
		next := 1
		for _, disc := range renumbered {
			for used[next] {
				next++
			}
			used[next] = true
			disc.Number = next
			moving = append(moving, disc)
		}
		if err := tx.Where("rom_id = ?", sourceID).Delete(&ROMDiscModel{}).Error; err != nil {
			return err
		}
		for _, disc := range moving {
			disc.ROMID = targetID
			if err := tx.Create(&disc).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&UploadModel{}).Where("rom_id = ?", sourceID).Update("rom_id", targetID).Error; err != nil {
			return err
		}
		var libraryIDs []string
		if err := tx.Model(&LibraryItemModel{}).Where("rom_id = ?", sourceID).Pluck("library_id", &libraryIDs).Error; err != nil {
			return err
		}
		for _, libraryID := range libraryIDs {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&LibraryItemModel{LibraryID: libraryID, ROMID: targetID}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("rom_id = ?", sourceID).Delete(&LibraryItemModel{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&source).Error; err != nil {
			return err
		}
		// The source's key is free now; a later disc of the release finds the merged title by it.
		if target.DiscGroup == nil && source.DiscGroup != nil {
			if err := tx.Model(&ROMModel{}).Where("id = ?", targetID).Update("disc_group", *source.DiscGroup).Error; err != nil {
				return err
			}
		}
		if err := setPrimaryDisc(tx, targetID); err != nil {
			return err
		}
		paths := []string{}
		for _, path := range []*string{source.CoverArtPath, source.CoverThumbPath, source.TitleScreenPath, source.ScreenshotPath} {
			if path != nil && *path != "" {
				paths = append(paths, *path)
			}
		}
		orphanPaths, err = orphanedPaths(tx, paths)
		return err
	})
	if err != nil {
		return asDatabaseError("merging ROMs", err)
	}
	repositories.removeOrphans(ctx, orphanPaths, deletePath)
	return nil
}

// SplitROMDisc moves one disc of a ROM with several onto a new ROM, newROMID, as its disc 1. The
// new ROM copies the old one's identity, covers included, and its owner and libraries, and has no
// group key. The other discs keep their numbers. The disc's upload moves with it when it can be
// told from the other discs' uploads.
func (repositories *Repositories) SplitROMDisc(ctx context.Context, romID, romFileID, newROMID string) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roms, err := lockROMs(tx, romID)
		if err != nil {
			return err
		}
		discs, err := findDiscs(tx, romID)
		if err != nil {
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
			return fault.Validation("a ROM keeps at least one disc; there is nothing to split")
		}
		split := roms[romID]
		split.ID, split.ROMFileID, split.DiscGroup = newROMID, romFileID, nil
		if err := tx.Create(&split).Error; err != nil {
			return err
		}
		var uploadIDs []string
		if err := uploadNamedForDisc(tx, romID, *disc, discs).Pluck("id", &uploadIDs).Error; err != nil {
			return err
		}
		if len(uploadIDs) > 0 {
			if err := tx.Model(&UploadModel{}).Where("id IN ?", uploadIDs).Update("rom_id", newROMID).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("rom_id = ? AND rom_file_id = ?", romID, romFileID).Delete(&ROMDiscModel{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&ROMDiscModel{ROMID: newROMID, ROMFileID: romFileID, Number: 1, Filename: disc.Filename}).Error; err != nil {
			return err
		}
		var libraryIDs []string
		if err := tx.Model(&LibraryItemModel{}).Where("rom_id = ?", romID).Pluck("library_id", &libraryIDs).Error; err != nil {
			return err
		}
		for _, libraryID := range libraryIDs {
			if err := tx.Create(&LibraryItemModel{LibraryID: libraryID, ROMID: newROMID}).Error; err != nil {
				return err
			}
		}
		return setPrimaryDisc(tx, romID)
	})
	if err != nil {
		return asDatabaseError("splitting ROM disc", err)
	}
	return nil
}

// ReorderROMDiscs numbers a ROM's discs 1..n in the order of romFileIDs, which must name every
// disc once, and makes the first the ROM's primary file.
func (repositories *Repositories) ReorderROMDiscs(ctx context.Context, romID string, romFileIDs []string) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockROMs(tx, romID); err != nil {
			return err
		}
		discs, err := findDiscs(tx, romID)
		if err != nil {
			return err
		}
		have := make(map[string]bool, len(discs))
		for _, disc := range discs {
			have[disc.ROMFileID] = true
		}
		named := make(map[string]bool, len(romFileIDs))
		for _, id := range romFileIDs {
			if !have[id] || named[id] {
				return fault.Validation("the order must name every disc of the ROM exactly once")
			}
			named[id] = true
		}
		if len(named) != len(discs) {
			return fault.Validation("the order must name every disc of the ROM exactly once")
		}
		// Park every number out of range first: (rom_id, number) is the primary key.
		if err := tx.Model(&ROMDiscModel{}).Where("rom_id = ?", romID).Update("number", gorm.Expr("number + ?", 1000)).Error; err != nil {
			return err
		}
		for index, id := range romFileIDs {
			if err := tx.Model(&ROMDiscModel{}).Where("rom_id = ? AND rom_file_id = ?", romID, id).Update("number", index+1).Error; err != nil {
				return err
			}
		}
		return setPrimaryDisc(tx, romID)
	})
	if err != nil {
		return asDatabaseError("reordering ROM discs", err)
	}
	return nil
}

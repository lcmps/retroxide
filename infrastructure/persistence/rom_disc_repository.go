package persistence

import (
	"context"

	romdomain "github.com/lcmps/retroxide/domain/rom"
	"gorm.io/gorm"
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

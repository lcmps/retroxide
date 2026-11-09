package persistence

import (
	"context"

	"gorm.io/gorm"
)

// fileUses counts what uses a stored file: games that have it as a disc (other than exceptROMID,
// when set) and BIOS entries. A file is unused only when both are zero; every deletion decides
// with this, so a file a BIOS still uses is never removed with a game, or the other way round.
func fileUses(tx *gorm.DB, romFileID, exceptROMID string) (games, bios int64, err error) {
	query := tx.Model(&ROMDiscModel{}).Where("rom_file_id = ?", romFileID)
	if exceptROMID != "" {
		query = query.Where("rom_id <> ?", exceptROMID)
	}
	if err := query.Count(&games).Error; err != nil {
		return 0, 0, err
	}
	if err := tx.Model(&BIOSModel{}).Where("rom_file_id = ?", romFileID).Count(&bios).Error; err != nil {
		return 0, 0, err
	}
	return games, bios, nil
}

// CountBIOSByROMFileID counts the BIOS entries using a stored file (zero or one).
func (repositories *Repositories) CountBIOSByROMFileID(ctx context.Context, romFileID string) (uint64, error) {
	var count int64
	if err := repositories.DB.WithContext(ctx).Model(&BIOSModel{}).Where("rom_file_id = ?", romFileID).Count(&count).Error; err != nil {
		return 0, databaseError("counting bios references", err)
	}
	return uint64(count), nil
}

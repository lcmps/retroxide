package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateShareLink records a link to romID, kept by its token's hash.
func (repositories *Repositories) CreateShareLink(ctx context.Context, tokenHash, romID, createdBy string) error {
	link := ROMShareLinkModel{TokenHash: tokenHash, ROMID: romID, CreatedBy: createdBy}
	// created_at comes from the column default (GORM would send its tag default as a string).
	if err := repositories.DB.WithContext(ctx).Omit("created_at").Create(&link).Error; err != nil {
		return databaseError("creating share link", err)
	}
	return nil
}

// FindUnusedShareLink returns the ROM an unused link offers. A link that was used, never existed,
// or whose ROM is gone is not found.
func (repositories *Repositories) FindUnusedShareLink(ctx context.Context, tokenHash string) (string, bool, error) {
	var romIDs []string
	err := repositories.DB.WithContext(ctx).Table("rom_share_link AS l").
		Joins("JOIN rom AS r ON r.id = l.rom_id").
		Where("l.token_hash = ? AND l.used_at IS NULL", tokenHash).
		Limit(1).Pluck("l.rom_id", &romIDs).Error
	if err != nil {
		return "", false, databaseError("finding share link", err)
	}
	if len(romIDs) == 0 {
		return "", false, nil
	}
	return romIDs[0], true, nil
}

// UserHasROMFile reports whether one of userID's ROMs has the stored file as a disc.
func (repositories *Repositories) UserHasROMFile(ctx context.Context, userID, romFileID string) (bool, error) {
	has, err := userHasROMFile(repositories.DB.WithContext(ctx), userID, romFileID)
	if err != nil {
		return false, databaseError("checking a library for a rom file", err)
	}
	return has, nil
}

func userHasROMFile(tx *gorm.DB, userID, romFileID string) (bool, error) {
	var count int64
	err := tx.Table("rom_disc AS d").Joins("JOIN rom AS r ON r.id = d.rom_id").
		Where("r.uploader_id = ? AND d.rom_file_id = ?", userID, romFileID).Count(&count).Error
	return count > 0, err
}

// RedeemShareLink spends an unused link, adding a copy of its ROM (every disc, the same stored
// files and metadata) to userID's library as newROMID. In one transaction with the link locked,
// so exactly one of several concurrent redeems succeeds. A user who already has the ROM's first
// disc is refused with already_in_library and the link stays unused.
func (repositories *Repositories) RedeemShareLink(ctx context.Context, tokenHash, userID, newROMID string) (string, error) {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var link ROMShareLinkModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND used_at IS NULL", tokenHash).Take(&link).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var original ROMModel
		if err := tx.Where("id = ?", link.ROMID).Take(&original).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fault.NotFound()
			}
			return err
		}
		var discs []ROMDiscModel
		if err := tx.Where("rom_id = ?", original.ID).Order("number ASC").Find(&discs).Error; err != nil {
			return err
		}
		if len(discs) == 0 {
			discs = []ROMDiscModel{{ROMID: original.ID, ROMFileID: original.ROMFileID, Number: 1}}
		}
		has, err := userHasROMFile(tx, userID, discs[0].ROMFileID)
		if err != nil {
			return err
		}
		if has {
			return fault.AlreadyInLibrary()
		}
		copied := original
		copied.ID, copied.UploaderID = newROMID, userID
		if copied.DiscGroup != nil {
			// A disc group is unique per uploader; a clashing one is dropped.
			var clashes int64
			if err := tx.Model(&ROMModel{}).Where("uploader_id = ? AND disc_group = ?", userID, *copied.DiscGroup).Count(&clashes).Error; err != nil {
				return err
			}
			if clashes > 0 {
				copied.DiscGroup = nil
			}
		}
		if err := tx.Create(&copied).Error; err != nil {
			return err
		}
		for _, disc := range discs {
			disc.ROMID = newROMID
			if err := tx.Create(&disc).Error; err != nil {
				return err
			}
		}
		usedAt := sqlTimestamp(time.Now())
		result := tx.Model(&ROMShareLinkModel{}).Where("token_hash = ? AND used_at IS NULL", tokenHash).
			Updates(map[string]any{"used_by": userID, "used_at": usedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fault.NotFound()
		}
		return nil
	})
	if err != nil {
		var appError *fault.Error
		if errors.As(err, &appError) {
			return "", err
		}
		return "", databaseError("redeeming share link", err)
	}
	return newROMID, nil
}

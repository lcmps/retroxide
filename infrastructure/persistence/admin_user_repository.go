package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/users"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"gorm.io/gorm"
)

var _ users.Store = (*Repositories)(nil)

// ListUsers returns every account ordered by email.
func (repositories *Repositories) ListUsers(ctx context.Context) ([]authdomain.User, error) {
	var models []UserModel
	if err := repositories.DB.WithContext(ctx).Order("email ASC").Find(&models).Error; err != nil {
		return nil, databaseError("listing users", err)
	}
	accounts := make([]authdomain.User, 0, len(models))
	for _, model := range models {
		account, err := userEntity(model)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

// UserActivity summarizes every user with a few grouped queries rather than one per user. The
// ROM counts read only idx_rom_uploader_platform.
func (repositories *Repositories) UserActivity(ctx context.Context) (map[string]users.Activity, error) {
	db := repositories.DB.WithContext(ctx)
	activity := make(map[string]users.Activity)
	update := func(userID string, change func(*users.Activity)) {
		entry := activity[userID]
		change(&entry)
		activity[userID] = entry
	}

	var platforms []struct {
		UploaderID string
		Platform   string
		Count      uint64
	}
	if err := db.Model(&ROMModel{}).Select("uploader_id, MIN(platform) AS platform, COUNT(*) AS count").
		Group("uploader_id, platform_key").Scan(&platforms).Error; err != nil {
		return nil, databaseError("counting roms per user", err)
	}
	for _, row := range platforms {
		update(row.UploaderID, func(entry *users.Activity) {
			entry.ROMCount += row.Count
			entry.Platforms = append(entry.Platforms, users.PlatformCount{Platform: row.Platform, Count: row.Count})
		})
	}

	var storage []struct {
		UploaderID string
		Bytes      uint64
	}
	if err := db.Table("rom AS r").Joins("JOIN rom_disc AS d ON d.rom_id = r.id").Joins("JOIN rom_file AS f ON f.id = d.rom_file_id").
		Select("r.uploader_id AS uploader_id, COALESCE(SUM(f.size), 0) AS bytes").Group("r.uploader_id").Scan(&storage).Error; err != nil {
		return nil, databaseError("summing storage per user", err)
	}
	for _, row := range storage {
		update(row.UploaderID, func(entry *users.Activity) { entry.StorageBytes = row.Bytes })
	}

	var uploads []struct {
		UploaderID string
		Count      uint64
	}
	if err := db.Model(&UploadModel{}).Select("uploader_id, COUNT(*) AS count").Group("uploader_id").Scan(&uploads).Error; err != nil {
		return nil, databaseError("counting uploads per user", err)
	}
	for _, row := range uploads {
		update(row.UploaderID, func(entry *users.Activity) { entry.UploadCount = row.Count })
	}

	var sessions []struct {
		UserID string
		Count  uint64
	}
	active := db.Model(&RefreshTokenModel{}).Select("user_id, COUNT(*) AS count").Where("revoked = ?", false)
	now := uint64(time.Now().Unix())
	if isMySQL(db) {
		active = active.Where("expires_at > FROM_UNIXTIME(?)", now)
	} else {
		cutoff, err := formatUnixTimestamp(now)
		if err != nil {
			return nil, err
		}
		active = active.Where("expires_at > ?", cutoff)
	}
	if err := active.Group("user_id").Scan(&sessions).Error; err != nil {
		return nil, databaseError("counting sessions per user", err)
	}
	for _, row := range sessions {
		update(row.UserID, func(entry *users.Activity) { entry.ActiveSessions = row.Count })
	}
	return activity, nil
}

// DeleteUserTransferringData hands the user's ROMs, libraries, and uploads to heirID and deletes
// the account in one transaction; foreign keys delete their API clients and refresh tokens.
func (repositories *Repositories) DeleteUserTransferringData(ctx context.Context, userID, heirID string) (users.Transfer, error) {
	var transfer users.Transfer
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A disc group is unique per uploader, so a ROM whose group the heir already uses would
		// collide on the move. It leaves the group instead and stops gathering new discs.
		var heirGroups []string
		if err := tx.Model(&ROMModel{}).Where("uploader_id = ? AND disc_group IS NOT NULL", heirID).Pluck("disc_group", &heirGroups).Error; err != nil {
			return err
		}
		if len(heirGroups) > 0 {
			if err := tx.Model(&ROMModel{}).Where("uploader_id = ? AND disc_group IN ?", userID, heirGroups).Update("disc_group", nil).Error; err != nil {
				return err
			}
		}
		for _, move := range []struct {
			model  any
			column string
			count  *uint64
		}{
			{&ROMModel{}, "uploader_id", &transfer.ROMs},
			{&LibraryModel{}, "user_id", &transfer.Libraries},
			{&UploadModel{}, "uploader_id", &transfer.Uploads},
		} {
			result := tx.Model(move.model).Where(move.column+" = ?", userID).Update(move.column, heirID)
			if result.Error != nil {
				return result.Error
			}
			*move.count = uint64(result.RowsAffected)
		}
		return expectDeleted(tx.Where("id = ?", userID).Delete(&UserModel{}), "deleting user")
	})
	var appError *fault.Error
	if errors.As(err, &appError) {
		return users.Transfer{}, err
	}
	if err != nil {
		return users.Transfer{}, databaseError("deleting user", err)
	}
	return transfer, nil
}

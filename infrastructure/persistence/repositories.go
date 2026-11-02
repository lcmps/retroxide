package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sqlTimestampLayout = "2006-01-02 15:04:05"

type Repositories struct {
	DB *gorm.DB
}

func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{DB: db}
}

func (repositories *Repositories) UpsertROMFile(ctx context.Context, romFile romdomain.ROMFile) (romdomain.ROMFile, error) {
	model := ROMFileModel{
		ID: romFile.ID, SHA256: romFile.Hash.SHA256, SHA1: romFile.Hash.SHA1,
		MD5: romFile.Hash.MD5, CRC32: romFile.Hash.CRC32, Size: romFile.Size,
		StoragePath: romFile.StoragePath,
	}
	err := repositories.DB.WithContext(ctx).Create(&model).Error
	if err != nil {
		existing, lookupErr := repositories.FindROMFileBySHA256(ctx, model.SHA256)
		if lookupErr == nil && existing != nil {
			return romdomain.ROMFile{}, classifyROMFileConflict(existing.ID, existing.Hash, romFile.Hash)
		}
		return romdomain.ROMFile{}, databaseError("upserting rom file", err)
	}
	saved, err := repositories.FindROMFileBySHA256(ctx, model.SHA256)
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	if saved == nil {
		return romdomain.ROMFile{}, fault.Internal(errors.New("rom file was not visible after upsert"))
	}
	return *saved, nil
}

func classifyROMFileConflict(existingID string, existingHash, incomingHash romdomain.ROMHash) error {
	if !existingHash.MatchesContent(incomingHash) {
		return fault.Internal(errors.New("sha256 collision with differing rom hash bundle"))
	}
	return fault.ROMAlreadyExists(existingID, incomingHash.SHA256)
}

func (repositories *Repositories) SaveROM(ctx context.Context, rom romdomain.ROM) error {
	model := romModel(rom)
	if err := repositories.DB.WithContext(ctx).Save(&model).Error; err != nil {
		return databaseError("saving rom", err)
	}
	return nil
}

func (repositories *Repositories) ListROMsWithFiles(ctx context.Context, limit int, afterCursor, platform *string) ([]romdomain.ROMWithFile, error) {
	query := repositories.DB.WithContext(ctx).
		Table("rom AS r").
		Select("r.id, r.rom_file_id, r.uploader_id, r.title, r.platform, r.region, r.version, r.identified_by, r.confidence, r.cover_art_path, f.sha256").
		Joins("JOIN rom_file AS f ON f.id = r.rom_file_id")
	if afterCursor != nil {
		query = query.Where("r.id > ?", *afterCursor)
	}
	if platform != nil && *platform != "" {
		query = query.Where("LOWER(r.platform) = ?", strings.ToLower(*platform))
	}
	var rows []struct {
		ROMModel
		SHA256 string `gorm:"column:sha256"`
	}
	if err := query.Order("r.id ASC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, databaseError("listing roms", err)
	}
	result := make([]romdomain.ROMWithFile, 0, len(rows))
	for _, row := range rows {
		result = append(result, romdomain.ROMWithFile{ROM: romEntity(row.ROMModel), SHA256: row.SHA256})
	}
	return result, nil
}

func (repositories *Repositories) FindROMByID(ctx context.Context, romID string) (*romdomain.ROM, error) {
	var model ROMModel
	if err := repositories.DB.WithContext(ctx).Where("id = ?", romID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding rom", err)
	}
	rom := romEntity(model)
	return &rom, nil
}

func (repositories *Repositories) ROMExists(ctx context.Context, romID string) (bool, error) {
	var count int64
	if err := repositories.DB.WithContext(ctx).Model(&ROMModel{}).Where("id = ?", romID).Count(&count).Error; err != nil {
		return false, databaseError("checking rom existence", err)
	}
	return count != 0, nil
}

func (repositories *Repositories) FindROMFileByID(ctx context.Context, romFileID string) (*romdomain.ROMFile, error) {
	var model ROMFileModel
	if err := repositories.DB.WithContext(ctx).Where("id = ?", romFileID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding rom file", err)
	}
	romFile, err := romFileEntity(model)
	if err != nil {
		return nil, err
	}
	return &romFile, nil
}

func (repositories *Repositories) FindROMFileBySHA256(ctx context.Context, sha256 string) (*romdomain.ROMFile, error) {
	var model ROMFileModel
	if err := repositories.DB.WithContext(ctx).Where("sha256 = ?", sha256).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding rom file by sha256", err)
	}
	romFile, err := romFileEntity(model)
	if err != nil {
		return nil, err
	}
	return &romFile, nil
}

func (repositories *Repositories) FindROMFileByROMID(ctx context.Context, romID string) (*romdomain.ROMFile, error) {
	var model ROMFileModel
	err := repositories.DB.WithContext(ctx).Table("rom_file AS f").
		Select("f.*").Joins("JOIN rom AS r ON r.rom_file_id = f.id").
		Where("r.id = ?", romID).Take(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding rom file by rom id", err)
	}
	romFile, err := romFileEntity(model)
	if err != nil {
		return nil, err
	}
	return &romFile, nil
}

func (repositories *Repositories) CountROMsByROMFileID(ctx context.Context, romFileID string) (uint64, error) {
	var count int64
	if err := repositories.DB.WithContext(ctx).Model(&ROMModel{}).Where("rom_file_id = ?", romFileID).Count(&count).Error; err != nil {
		return 0, databaseError("counting rom references", err)
	}
	return uint64(count), nil
}

func (repositories *Repositories) DeleteROM(ctx context.Context, romID string) error {
	result := repositories.DB.WithContext(ctx).Where("id = ?", romID).Delete(&ROMModel{})
	if result.Error != nil {
		return databaseError("deleting rom", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func (repositories *Repositories) DeleteROMFile(ctx context.Context, romFileID string) error {
	result := repositories.DB.WithContext(ctx).Where("id = ?", romFileID).Delete(&ROMFileModel{})
	if result.Error != nil {
		return databaseError("deleting rom file", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func (repositories *Repositories) SaveUpload(ctx context.Context, upload uploadsdomain.Upload) error {
	model := UploadModel{
		ID: upload.ID, UploaderID: upload.UploaderID, OriginalFilename: upload.OriginalFilename,
		Status: string(upload.Status), ErrorMessage: upload.ErrorMessage, ROMID: upload.ROMID,
	}
	if err := repositories.DB.WithContext(ctx).Save(&model).Error; err != nil {
		return databaseError("saving upload", err)
	}
	return nil
}

func (repositories *Repositories) FindUpload(ctx context.Context, uploadID string) (*uploadsdomain.Upload, error) {
	var model UploadModel
	if err := repositories.DB.WithContext(ctx).Where("id = ?", uploadID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding upload", err)
	}
	upload := uploadsdomain.Upload{ID: model.ID, UploaderID: model.UploaderID, OriginalFilename: model.OriginalFilename, Status: uploadsdomain.UploadStatus(model.Status), ErrorMessage: model.ErrorMessage, ROMID: model.ROMID}
	return &upload, nil
}

func (repositories *Repositories) SaveLibrary(ctx context.Context, library librariesdomain.Library) error {
	if err := repositories.DB.WithContext(ctx).Exec(
		"INSERT INTO library (id, user_id, name) VALUES (?, ?, ?)",
		library.ID, library.UserID, library.Name,
	).Error; err != nil {
		return databaseError("saving library", err)
	}
	return nil
}

func (repositories *Repositories) ListLibrary(ctx context.Context) ([]librariesdomain.Library, error) {
	var models []LibraryModel
	if err := repositories.DB.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, databaseError("listing libraries", err)
	}
	rows := make([]librariesdomain.Library, 0, len(models))
	for _, model := range models {
		rows = append(rows, librariesdomain.Library{ID: model.ID, UserID: model.UserID, Name: model.Name})
	}
	return rows, nil
}

func (repositories *Repositories) FindLibraryByID(ctx context.Context, libraryID string) (*librariesdomain.Library, error) {
	var model LibraryModel
	if err := repositories.DB.WithContext(ctx).Where("id = ?", libraryID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding library", err)
	}
	library := librariesdomain.Library{ID: model.ID, UserID: model.UserID, Name: model.Name}
	return &library, nil
}

func (repositories *Repositories) ListLibraryROMIDs(ctx context.Context, libraryID string) ([]string, error) {
	var romIDs []string
	if err := repositories.DB.WithContext(ctx).Model(&LibraryItemModel{}).
		Where("library_id = ?", libraryID).Order("rom_id ASC").Pluck("rom_id", &romIDs).Error; err != nil {
		return nil, databaseError("listing library ROMs", err)
	}
	return romIDs, nil
}

func (repositories *Repositories) AddLibraryItem(ctx context.Context, libraryID, romID string) error {
	item := LibraryItemModel{LibraryID: libraryID, ROMID: romID}
	if err := repositories.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return databaseError("adding library item", err)
	}
	return nil
}

func (repositories *Repositories) RemoveLibraryItem(ctx context.Context, libraryID, romID string) (bool, error) {
	result := repositories.DB.WithContext(ctx).Where("library_id = ? AND rom_id = ?", libraryID, romID).Delete(&LibraryItemModel{})
	if result.Error != nil {
		return false, databaseError("removing library item", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (repositories *Repositories) FindByEmail(ctx context.Context, email string) (*authdomain.User, error) {
	var model UserModel
	if err := repositories.DB.WithContext(ctx).Where("email = ?", email).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding user by email", err)
	}
	user, err := userEntity(model)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (repositories *Repositories) FindByID(ctx context.Context, userID string) (*authdomain.User, error) {
	var model UserModel
	if err := repositories.DB.WithContext(ctx).Where("id = ?", userID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding user", err)
	}
	user, err := userEntity(model)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (repositories *Repositories) SaveUser(ctx context.Context, user authdomain.User) error {
	role, err := roleString(user.Role)
	if err != nil {
		return err
	}
	model := UserModel{ID: user.ID, Email: user.Email, PasswordHash: user.PasswordHash, Role: role}
	if err := repositories.DB.WithContext(ctx).Save(&model).Error; err != nil {
		return databaseError("saving user", err)
	}
	return nil
}

func (repositories *Repositories) FindByTokenHash(ctx context.Context, tokenHash string) (*authdomain.Client, error) {
	var model ClientModel
	if err := repositories.DB.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding client by token hash", err)
	}
	client := authdomain.Client{ID: model.ID, UserID: model.UserID, Name: model.Name, TokenHash: model.TokenHash}
	return &client, nil
}

func (repositories *Repositories) FindByUserID(ctx context.Context, userID string) ([]authdomain.Client, error) {
	var models []ClientModel
	if err := repositories.DB.WithContext(ctx).Where("user_id = ?", userID).Order("id ASC").Find(&models).Error; err != nil {
		return nil, databaseError("listing clients", err)
	}
	clients := make([]authdomain.Client, 0, len(models))
	for _, model := range models {
		clients = append(clients, authdomain.Client{ID: model.ID, UserID: model.UserID, Name: model.Name, TokenHash: model.TokenHash})
	}
	return clients, nil
}

func (repositories *Repositories) SaveClient(ctx context.Context, client authdomain.Client) error {
	model := ClientModel{ID: client.ID, UserID: client.UserID, Name: client.Name, TokenHash: client.TokenHash}
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		return databaseError("saving client", err)
	}
	return nil
}

func (repositories *Repositories) DeleteClient(ctx context.Context, clientID string) error {
	result := repositories.DB.WithContext(ctx).Where("id = ?", clientID).Delete(&ClientModel{})
	if result.Error != nil {
		return databaseError("deleting client", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func (repositories *Repositories) Store(ctx context.Context, token authdomain.RefreshToken) error {
	if err := insertRefreshToken(repositories.DB.WithContext(ctx), token); err != nil {
		return databaseError("storing refresh token", err)
	}
	return nil
}

func (repositories *Repositories) FindByJTI(ctx context.Context, jti string) (*authdomain.RefreshToken, error) {
	if repositories.DB.Dialector.Name() == "mysql" {
		var row struct {
			JTI       string `gorm:"column:jti"`
			UserID    string `gorm:"column:user_id"`
			TokenHash string `gorm:"column:token_hash"`
			ExpiresAt uint64 `gorm:"column:expires_at"`
			Revoked   bool   `gorm:"column:revoked"`
			CreatedAt uint64 `gorm:"column:created_at"`
		}
		err := repositories.DB.WithContext(ctx).Table("refresh_tokens").
			Select("jti, user_id, token_hash, UNIX_TIMESTAMP(expires_at) AS expires_at, revoked, UNIX_TIMESTAMP(created_at) AS created_at").
			Where("jti = ?", jti).Take(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, databaseError("finding refresh token", err)
		}
		token := authdomain.RefreshToken{JTI: row.JTI, UserID: row.UserID, TokenHash: row.TokenHash, ExpiresAt: row.ExpiresAt, Revoked: row.Revoked, CreatedAt: row.CreatedAt}
		return &token, nil
	}
	var model RefreshTokenModel
	if err := repositories.DB.WithContext(ctx).Where("jti = ?", jti).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError("finding refresh token", err)
	}
	token, err := refreshTokenEntity(model)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (repositories *Repositories) Rotate(ctx context.Context, oldJTI string, replacement authdomain.RefreshToken) (bool, error) {
	rotated := false
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&RefreshTokenModel{}).
			Where("jti = ? AND revoked = ? AND user_id = ?", oldJTI, false, replacement.UserID).
			Update("revoked", true)
		if result.Error != nil {
			return fmt.Errorf("consuming previous refresh token: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			if err := tx.Model(&RefreshTokenModel{}).
				Where("user_id = ?", replacement.UserID).
				Update("revoked", true).Error; err != nil {
				return fmt.Errorf("revoking reused refresh token family: %w", err)
			}
			return nil
		}
		if err := insertRefreshToken(tx, replacement); err != nil {
			return fmt.Errorf("storing replacement refresh token: %w", err)
		}
		rotated = true
		return nil
	})
	if err != nil {
		return false, databaseError("rotating refresh token", err)
	}
	return rotated, nil
}

func insertRefreshToken(db *gorm.DB, token authdomain.RefreshToken) error {
	if db.Dialector.Name() == "mysql" {
		_, err := formatUnixTimestamp(token.ExpiresAt)
		if err != nil {
			return err
		}
		_, err = formatUnixTimestamp(token.CreatedAt)
		if err != nil {
			return err
		}
		return db.Exec(`INSERT INTO refresh_tokens (jti, user_id, token_hash, expires_at, revoked, created_at)
VALUES (?, ?, ?, FROM_UNIXTIME(?), ?, FROM_UNIXTIME(?))`, token.JTI, token.UserID, token.TokenHash, token.ExpiresAt, token.Revoked, token.CreatedAt).Error
	}
	expiresAt, err := formatUnixTimestamp(token.ExpiresAt)
	if err != nil {
		return err
	}
	createdAt, err := formatUnixTimestamp(token.CreatedAt)
	if err != nil {
		return err
	}
	return db.Create(&RefreshTokenModel{
		JTI: token.JTI, UserID: token.UserID, TokenHash: token.TokenHash,
		ExpiresAt: expiresAt, Revoked: token.Revoked, CreatedAt: createdAt,
	}).Error
}

func (repositories *Repositories) Revoke(ctx context.Context, jti string) error {
	result := repositories.DB.WithContext(ctx).Model(&RefreshTokenModel{}).Where("jti = ?", jti).Update("revoked", true)
	if result.Error != nil {
		return databaseError("revoking refresh token", result.Error)
	}
	return nil
}

func (repositories *Repositories) RevokeAllForUser(ctx context.Context, userID string) error {
	if err := repositories.DB.WithContext(ctx).Model(&RefreshTokenModel{}).Where("user_id = ?", userID).Update("revoked", true).Error; err != nil {
		return databaseError("revoking user refresh tokens", err)
	}
	return nil
}

func romModel(rom romdomain.ROM) ROMModel {
	return ROMModel{
		ID: rom.ID, ROMFileID: rom.ROMFileID, UploaderID: rom.UploaderID,
		Title: rom.Identity.Title, Platform: rom.Identity.Platform, Region: rom.Identity.Region,
		Version: rom.Identity.Version, IdentifiedBy: rom.Identity.IdentifiedBy,
		Confidence: rom.Identity.Confidence, CoverArtPath: rom.Identity.CoverArtPath,
		Publisher: rom.Identity.Publisher, ReleaseYear: rom.Identity.ReleaseYear, Description: rom.Identity.Description,
		TitleScreenPath: rom.Identity.TitleScreenPath, ScreenshotPath: rom.Identity.ScreenshotPath,
	}
}

func romEntity(model ROMModel) romdomain.ROM {
	return romdomain.ROM{
		ID: model.ID, ROMFileID: model.ROMFileID, UploaderID: model.UploaderID,
		Identity: romdomain.ROMIdentity{
			Title: model.Title, Platform: model.Platform, Region: model.Region, Version: model.Version,
			CoverArtPath: model.CoverArtPath, IdentifiedBy: model.IdentifiedBy, Confidence: model.Confidence,
			Publisher: model.Publisher, ReleaseYear: model.ReleaseYear, Description: model.Description,
			TitleScreenPath: model.TitleScreenPath, ScreenshotPath: model.ScreenshotPath,
		},
	}
}

func romFileEntity(model ROMFileModel) (romdomain.ROMFile, error) {
	hash, err := romdomain.NewROMHash(model.SHA256, model.SHA1, model.MD5, model.CRC32)
	if err != nil {
		return romdomain.ROMFile{}, fault.Internal(fmt.Errorf("invalid persisted rom hash: %w", err))
	}
	return romdomain.ROMFile{ID: model.ID, Hash: hash, Size: model.Size, StoragePath: model.StoragePath}, nil
}

func userEntity(model UserModel) (authdomain.User, error) {
	var role identity.RequesterRole
	switch model.Role {
	case "admin":
		role = identity.RoleAdmin
	case "user":
		role = identity.RoleUser
	default:
		return authdomain.User{}, fault.Internal(fmt.Errorf("unknown persisted user role %q", model.Role))
	}
	return authdomain.User{ID: model.ID, Email: model.Email, PasswordHash: model.PasswordHash, Role: role}, nil
}

func refreshTokenEntity(model RefreshTokenModel) (authdomain.RefreshToken, error) {
	expiresAt, err := parseUnixTimestamp(model.ExpiresAt)
	if err != nil {
		return authdomain.RefreshToken{}, fault.Internal(fmt.Errorf("parsing refresh token expiry: %w", err))
	}
	createdAt, err := parseUnixTimestamp(model.CreatedAt)
	if err != nil {
		return authdomain.RefreshToken{}, fault.Internal(fmt.Errorf("parsing refresh token creation: %w", err))
	}
	return authdomain.RefreshToken{JTI: model.JTI, UserID: model.UserID, TokenHash: model.TokenHash, ExpiresAt: expiresAt, Revoked: model.Revoked, CreatedAt: createdAt}, nil
}

func formatUnixTimestamp(seconds uint64) (string, error) {
	if seconds > uint64(1<<63-1) {
		return "", fault.Validation("timestamp is outside supported range")
	}
	return time.Unix(int64(seconds), 0).UTC().Format(sqlTimestampLayout), nil
}

func parseUnixTimestamp(timestamp string) (uint64, error) {
	parsed, err := time.ParseInLocation(sqlTimestampLayout, timestamp, time.UTC)
	if err != nil {
		return 0, err
	}
	seconds := parsed.Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("timestamp predates unix epoch")
	}
	return uint64(seconds), nil
}

func roleString(role identity.RequesterRole) (string, error) {
	switch role {
	case identity.RoleAdmin:
		return "admin", nil
	case identity.RoleUser:
		return "user", nil
	default:
		return "", fault.Validation("invalid user role")
	}
}

func databaseError(operation string, err error) error {
	return fault.Internal(fmt.Errorf("%s: %w", operation, err))
}

var _ rom.Repository = (*Repositories)(nil)
var _ uploads.Repository = (*Repositories)(nil)
var _ libraries.Repository = (*Repositories)(nil)
var _ auth.UserRepository = (*Repositories)(nil)
var _ auth.ClientRepository = (*Repositories)(nil)
var _ auth.RefreshTokenRepository = (*Repositories)(nil)

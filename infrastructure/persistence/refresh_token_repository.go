package persistence

import (
	"context"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"gorm.io/gorm"
)

// mysqlRefreshTokenRow reads MySQL TIMESTAMP columns as unix seconds, avoiding session time-zone conversion.
type mysqlRefreshTokenRow struct {
	JTI       string `gorm:"column:jti"`
	UserID    string `gorm:"column:user_id"`
	TokenHash string `gorm:"column:token_hash"`
	ExpiresAt uint64 `gorm:"column:expires_at"`
	Revoked   bool   `gorm:"column:revoked"`
	CreatedAt uint64 `gorm:"column:created_at"`
}

func (repositories *Repositories) Store(ctx context.Context, token authdomain.RefreshToken) error {
	if err := insertRefreshToken(repositories.DB.WithContext(ctx), token); err != nil {
		return databaseError("storing refresh token", err)
	}
	return nil
}

func (repositories *Repositories) FindByJTI(ctx context.Context, jti string) (*authdomain.RefreshToken, error) {
	db := repositories.DB.WithContext(ctx)
	if isMySQL(db) {
		query := db.Table("refresh_tokens").
			Select("jti, user_id, token_hash, UNIX_TIMESTAMP(expires_at) AS expires_at, revoked, UNIX_TIMESTAMP(created_at) AS created_at").
			Where("jti = ?", jti)
		row, err := takeOne[mysqlRefreshTokenRow](query, "finding refresh token")
		return mapOptional(row, err, infallible(func(row mysqlRefreshTokenRow) authdomain.RefreshToken {
			return authdomain.RefreshToken{
				JTI: row.JTI, UserID: row.UserID, TokenHash: row.TokenHash,
				ExpiresAt: row.ExpiresAt, Revoked: row.Revoked, CreatedAt: row.CreatedAt,
			}
		}))
	}
	model, err := takeOne[RefreshTokenModel](db.Where("jti = ?", jti), "finding refresh token")
	return mapOptional(model, err, refreshTokenEntity)
}

// Rotate atomically consumes oldJTI and stores replacement. When oldJTI was already consumed, the
// whole token family is revoked and Rotate reports false.
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
			if err := revokeUserRefreshTokens(tx, replacement.UserID); err != nil {
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

func (repositories *Repositories) Revoke(ctx context.Context, jti string) error {
	result := repositories.DB.WithContext(ctx).Model(&RefreshTokenModel{}).Where("jti = ?", jti).Update("revoked", true)
	if result.Error != nil {
		return databaseError("revoking refresh token", result.Error)
	}
	return nil
}

func (repositories *Repositories) RevokeAllForUser(ctx context.Context, userID string) error {
	if err := revokeUserRefreshTokens(repositories.DB.WithContext(ctx), userID); err != nil {
		return databaseError("revoking user refresh tokens", err)
	}
	return nil
}

func revokeUserRefreshTokens(db *gorm.DB, userID string) error {
	return db.Model(&RefreshTokenModel{}).Where("user_id = ?", userID).Update("revoked", true).Error
}

func insertRefreshToken(db *gorm.DB, token authdomain.RefreshToken) error {
	expiresAt, err := formatUnixTimestamp(token.ExpiresAt)
	if err != nil {
		return err
	}
	createdAt, err := formatUnixTimestamp(token.CreatedAt)
	if err != nil {
		return err
	}
	if isMySQL(db) {
		// FROM_UNIXTIME stores the instant independently of the connection's time zone.
		return db.Exec(`INSERT INTO refresh_tokens (jti, user_id, token_hash, expires_at, revoked, created_at)
VALUES (?, ?, ?, FROM_UNIXTIME(?), ?, FROM_UNIXTIME(?))`, token.JTI, token.UserID, token.TokenHash, token.ExpiresAt, token.Revoked, token.CreatedAt).Error
	}
	return db.Create(&RefreshTokenModel{
		JTI: token.JTI, UserID: token.UserID, TokenHash: token.TokenHash,
		ExpiresAt: expiresAt, Revoked: token.Revoked, CreatedAt: createdAt,
	}).Error
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
	return authdomain.RefreshToken{
		JTI: model.JTI, UserID: model.UserID, TokenHash: model.TokenHash,
		ExpiresAt: expiresAt, Revoked: model.Revoked, CreatedAt: createdAt,
	}, nil
}

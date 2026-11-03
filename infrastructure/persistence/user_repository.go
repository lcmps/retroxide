package persistence

import (
	"context"
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	"gorm.io/gorm"
)

const (
	persistedRoleAdmin = "admin"
	persistedRoleUser  = "user"
)

func (repositories *Repositories) FindByEmail(ctx context.Context, email string) (*authdomain.User, error) {
	model, err := takeOne[UserModel](repositories.DB.WithContext(ctx).Where("email = ?", email), "finding user by email")
	return mapOptional(model, err, userEntity)
}

// FindByUsername looks a user up by sign-in name; usernames are stored lowercase.
func (repositories *Repositories) FindByUsername(ctx context.Context, username string) (*authdomain.User, error) {
	model, err := takeOne[UserModel](repositories.DB.WithContext(ctx).Where("username = ?", strings.ToLower(username)), "finding user by username")
	return mapOptional(model, err, userEntity)
}

func (repositories *Repositories) FindByID(ctx context.Context, userID string) (*authdomain.User, error) {
	model, err := takeOne[UserModel](repositories.DB.WithContext(ctx).Where("id = ?", userID), "finding user")
	return mapOptional(model, err, userEntity)
}

// SaveUser inserts a new user or updates an existing one by ID. It does not use GORM's Save: on
// MySQL that becomes INSERT ... ON DUPLICATE KEY UPDATE, which fires on any unique key, so a new
// user with a taken email or username would overwrite the other account instead of failing.
func (repositories *Repositories) SaveUser(ctx context.Context, user authdomain.User) error {
	model, err := userModel(user)
	if err != nil {
		return err
	}
	err = repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&UserModel{}).Where("id = ?", model.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing == 0 {
			return tx.Create(&model).Error
		}
		return tx.Model(&UserModel{}).Where("id = ?", model.ID).Select("*").Updates(&model).Error
	})
	if isUniqueViolation(err) {
		return &fault.Error{Code: fault.CodeConflict, Message: "email or username is already in use"}
	}
	if err != nil {
		return databaseError("saving user", err)
	}
	return nil
}

// IncrementTokenVersion atomically bumps the user's token version, invalidating every token issued so far.
func (repositories *Repositories) IncrementTokenVersion(ctx context.Context, userID string) error {
	result := repositories.DB.WithContext(ctx).Model(&UserModel{}).Where("id = ?", userID).
		Update("token_version", gorm.Expr("token_version + 1"))
	if result.Error != nil {
		return databaseError("incrementing user token version", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func userModel(user authdomain.User) (UserModel, error) {
	role, err := roleString(user.Role)
	if err != nil {
		return UserModel{}, err
	}
	return UserModel{
		ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash,
		Role: role, TokenVersion: user.TokenVersion, Locale: user.Locale, Font: user.Font,
	}, nil
}

func userEntity(model UserModel) (authdomain.User, error) {
	var role identity.RequesterRole
	switch model.Role {
	case persistedRoleAdmin:
		role = identity.RoleAdmin
	case persistedRoleUser:
		role = identity.RoleUser
	default:
		return authdomain.User{}, fault.Internal(fmt.Errorf("unknown persisted user role %q", model.Role))
	}
	return authdomain.User{
		ID: model.ID, Email: model.Email, Username: model.Username, PasswordHash: model.PasswordHash,
		Role: role, TokenVersion: model.TokenVersion, Locale: model.Locale, Font: model.Font,
	}, nil
}

func roleString(role identity.RequesterRole) (string, error) {
	switch role {
	case identity.RoleAdmin:
		return persistedRoleAdmin, nil
	case identity.RoleUser:
		return persistedRoleUser, nil
	default:
		return "", fault.Validation("invalid user role")
	}
}

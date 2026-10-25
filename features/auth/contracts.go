package auth

import (
	"context"

	"github.com/lcmps/retroxide/internal/shared/identity"
)

type UserRepository interface {
	FindByEmail(context.Context, string) (*User, error)
	FindByID(context.Context, string) (*User, error)
	SaveUser(context.Context, User) error
}

type ClientRepository interface {
	FindByTokenHash(context.Context, string) (*Client, error)
	FindByUserID(context.Context, string) ([]Client, error)
	SaveClient(context.Context, Client) error
	DeleteClient(context.Context, string) error
}

type RefreshTokenRepository interface {
	Store(context.Context, RefreshToken) error
	FindByJTI(context.Context, string) (*RefreshToken, error)
	Rotate(context.Context, string, RefreshToken) (bool, error)
	Revoke(context.Context, string) error
	RevokeAllForUser(context.Context, string) error
}

type PasswordHasher interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
}

type AccessClaims struct {
	UserID string
	Role   string
	JTI    *string
}

type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        uint64
	RefreshJTI       string
	RefreshExpiresIn uint64
}

type TokenService interface {
	Issue(string, string) (TokenPair, error)
	VerifyAccess(string) (AccessClaims, error)
	VerifyRefresh(string) (AccessClaims, error)
}

type SessionAPI interface {
	Login(context.Context, LoginRequest) (SessionResponse, error)
	Refresh(context.Context, string) (SessionResponse, error)
	Logout(context.Context, identity.Requester) error
}

type ClientAPI interface {
	Create(context.Context, identity.Requester, string) (CreateClientResponse, error)
	Revoke(context.Context, identity.Requester, string) error
}

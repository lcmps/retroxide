package auth

import (
	"context"

	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

type UserRepository interface {
	FindByEmail(context.Context, string) (*authdomain.User, error)
	FindByID(context.Context, string) (*authdomain.User, error)
	SaveUser(context.Context, authdomain.User) error
}

type ClientRepository interface {
	FindByTokenHash(context.Context, string) (*authdomain.Client, error)
	FindByUserID(context.Context, string) ([]authdomain.Client, error)
	SaveClient(context.Context, authdomain.Client) error
	DeleteClient(context.Context, string) error
}

type RefreshTokenRepository interface {
	Store(context.Context, authdomain.RefreshToken) error
	FindByJTI(context.Context, string) (*authdomain.RefreshToken, error)
	Rotate(context.Context, string, authdomain.RefreshToken) (bool, error)
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
	List(context.Context, identity.Requester) (ClientListResponse, error)
	Create(context.Context, identity.Requester, string) (CreateClientResponse, error)
	Revoke(context.Context, identity.Requester, string) error
}

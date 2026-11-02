package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

const bearerPrefix = "Bearer "

type userReader interface {
	FindByID(ctx context.Context, userID string) (*authdomain.User, error)
}

type clientTokenReader interface {
	FindByTokenHash(ctx context.Context, tokenHash string) (*authdomain.Client, error)
}

// ExtractBearerToken returns the token from an "Authorization: Bearer <token>" header value.
func ExtractBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	return token, token != ""
}

// Authenticator resolves bearer tokens, either user access tokens or API client tokens, to requesters.
type Authenticator struct {
	tokens  TokenService
	users   userReader
	clients clientTokenReader
}

func NewAuthenticator(tokens TokenService, users userReader, clients clientTokenReader) *Authenticator {
	return &Authenticator{tokens: tokens, users: users, clients: clients}
}

// Authenticate verifies token and returns the requester it represents.
//
// Access tokens are checked against the user's current record, so a logout (which bumps the
// token version) or a role change applies to the very next request rather than at token expiry.
func (authenticator *Authenticator) Authenticate(ctx context.Context, token string) (identity.Requester, error) {
	claims, err := authenticator.tokens.VerifyAccess(token)
	if err != nil {
		return authenticator.authenticateClient(ctx, token)
	}
	user, err := authenticator.users.FindByID(ctx, claims.UserID)
	if err != nil {
		return identity.Requester{}, err
	}
	if user == nil || user.TokenVersion != claims.TokenVersion {
		return identity.Requester{}, fault.Unauthorized()
	}
	return identity.Requester{UserID: user.ID, Role: user.Role}, nil
}

func (authenticator *Authenticator) authenticateClient(ctx context.Context, token string) (identity.Requester, error) {
	digest := sha256.Sum256([]byte(token))
	client, err := authenticator.clients.FindByTokenHash(ctx, hex.EncodeToString(digest[:]))
	if err != nil || client == nil {
		return identity.Requester{}, fault.Unauthorized()
	}
	return identity.Requester{UserID: client.UserID, Role: identity.RoleClient}, nil
}

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

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

// UserChangeListener is told when a user's record changed in a way that affects their
// requests: a role change, a logout (token version bump), or deletion.
type UserChangeListener interface {
	UserChanged(userID string)
}

// Authenticator resolves bearer tokens, either user access tokens or API client tokens, to requesters.
type Authenticator struct {
	tokens  TokenService
	users   userReader
	clients clientTokenReader

	// cache holds what Authenticate needs of each user for cacheTTL, so a page that makes many
	// requests at once does not read the same user record for each. It is cleared for a user by
	// UserChanged, which the code paths that change users call, so a logout or a role change
	// still applies on the very next request.
	cacheTTL time.Duration
	cacheMu  sync.Mutex
	cache    map[string]cachedUser
	now      func() time.Time
}

type cachedUser struct {
	role         identity.RequesterRole
	tokenVersion uint64
	expires      time.Time
}

func NewAuthenticator(tokens TokenService, users userReader, clients clientTokenReader) *Authenticator {
	return &Authenticator{tokens: tokens, users: users, clients: clients, now: time.Now}
}

// WithUserCache keeps each authenticated user's role and token version for ttl.
func (authenticator *Authenticator) WithUserCache(ttl time.Duration) *Authenticator {
	authenticator.cacheTTL = ttl
	authenticator.cache = make(map[string]cachedUser)
	return authenticator
}

// UserChanged forgets a user, so their next request reads the current record.
func (authenticator *Authenticator) UserChanged(userID string) {
	if authenticator.cache == nil {
		return
	}
	authenticator.cacheMu.Lock()
	delete(authenticator.cache, userID)
	authenticator.cacheMu.Unlock()
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
	user, err := authenticator.lookup(ctx, claims.UserID)
	if err != nil {
		return identity.Requester{}, err
	}
	if user == nil || user.tokenVersion != claims.TokenVersion {
		return identity.Requester{}, fault.Unauthorized()
	}
	return identity.Requester{UserID: claims.UserID, Role: user.role}, nil
}

// lookup returns what Authenticate needs of a user, from the cache when it is fresh; nil for a
// user that does not exist (which is not cached, so a user created a moment later signs in).
func (authenticator *Authenticator) lookup(ctx context.Context, userID string) (*cachedUser, error) {
	if authenticator.cache != nil {
		authenticator.cacheMu.Lock()
		cached, ok := authenticator.cache[userID]
		authenticator.cacheMu.Unlock()
		if ok && authenticator.now().Before(cached.expires) {
			return &cached, nil
		}
	}
	user, err := authenticator.users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}
	entry := cachedUser{role: user.Role, tokenVersion: user.TokenVersion, expires: authenticator.now().Add(authenticator.cacheTTL)}
	if authenticator.cache != nil {
		authenticator.cacheMu.Lock()
		authenticator.cache[userID] = entry
		authenticator.cacheMu.Unlock()
	}
	return &entry, nil
}

func (authenticator *Authenticator) authenticateClient(ctx context.Context, token string) (identity.Requester, error) {
	digest := sha256.Sum256([]byte(token))
	client, err := authenticator.clients.FindByTokenHash(ctx, hex.EncodeToString(digest[:]))
	if err != nil || client == nil {
		return identity.Requester{}, fault.Unauthorized()
	}
	return identity.Requester{UserID: client.UserID, Role: identity.RoleClient}, nil
}

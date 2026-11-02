package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

const dummyPassword = "retroxide-dummy-password"

// SessionService handles password login and the access/refresh token lifecycle.
type SessionService struct {
	users        UserRepository
	hasher       PasswordHasher
	tokens       TokenService
	refreshes    RefreshTokenRepository
	dummyOnce    sync.Once
	dummyHash    string
	dummyHashErr error
}

func NewSessionService(
	users UserRepository,
	hasher PasswordHasher,
	tokens TokenService,
	refreshes RefreshTokenRepository,
) *SessionService {
	return &SessionService{users: users, hasher: hasher, tokens: tokens, refreshes: refreshes}
}

func (service *SessionService) Login(ctx context.Context, input LoginRequest) (SessionResponse, error) {
	user, err := service.users.FindByEmail(ctx, input.Email)
	if err != nil {
		return SessionResponse{}, err
	}
	if user == nil {
		dummyHash, err := service.dummyPasswordHash()
		if err != nil {
			return SessionResponse{}, fault.Internal(err)
		}
		_, _ = service.hasher.Verify(input.Password, dummyHash)
		return SessionResponse{}, fault.Unauthorized()
	}
	valid, err := service.hasher.Verify(input.Password, user.PasswordHash)
	if err != nil {
		return SessionResponse{}, fault.Internal(err)
	}
	if !valid {
		return SessionResponse{}, fault.Unauthorized()
	}
	return service.issueSession(ctx, *user)
}

func (service *SessionService) Refresh(ctx context.Context, refreshToken string) (SessionResponse, error) {
	claims, err := service.tokens.VerifyRefresh(refreshToken)
	if err != nil {
		return SessionResponse{}, err
	}
	if claims.JTI == nil {
		return SessionResponse{}, fault.Unauthorized()
	}
	record, err := service.refreshes.FindByJTI(ctx, *claims.JTI)
	if err != nil {
		return SessionResponse{}, err
	}
	if record == nil {
		return SessionResponse{}, fault.Unauthorized()
	}
	now, err := unixTimeNow()
	if err != nil {
		return SessionResponse{}, fault.Internal(err)
	}
	if record.ExpiresAt <= now {
		return SessionResponse{}, fault.Unauthorized()
	}
	if record.Revoked {
		// A consumed refresh token was replayed, so the session may be stolen: end every session.
		if err := service.endAllSessions(ctx, record.UserID); err != nil {
			return SessionResponse{}, err
		}
		return SessionResponse{}, fault.Unauthorized()
	}
	user, err := service.users.FindByID(ctx, record.UserID)
	if err != nil {
		return SessionResponse{}, err
	}
	if user == nil || user.TokenVersion != claims.TokenVersion {
		return SessionResponse{}, fault.Unauthorized()
	}
	return service.rotateSession(ctx, *user, *claims.JTI, now)
}

// Logout ends every session of the requester: outstanding access tokens stop working immediately
// and refresh tokens can no longer be exchanged.
func (service *SessionService) Logout(ctx context.Context, requester identity.Requester) error {
	return service.endAllSessions(ctx, requester.UserID)
}

func (service *SessionService) endAllSessions(ctx context.Context, userID string) error {
	// Bump the version first so access tokens die even if revoking refresh tokens fails.
	if err := service.users.IncrementTokenVersion(ctx, userID); err != nil {
		return err
	}
	return service.refreshes.RevokeAllForUser(ctx, userID)
}

func (service *SessionService) dummyPasswordHash() (string, error) {
	service.dummyOnce.Do(func() {
		service.dummyHash, service.dummyHashErr = service.hasher.Hash(dummyPassword)
	})
	return service.dummyHash, service.dummyHashErr
}

func (service *SessionService) issueSession(ctx context.Context, user authdomain.User) (SessionResponse, error) {
	now, err := unixTimeNow()
	if err != nil {
		return SessionResponse{}, fault.Internal(err)
	}
	tokens, err := service.issueTokens(user)
	if err != nil {
		return SessionResponse{}, err
	}
	record, err := refreshTokenRecord(user.ID, tokens, now)
	if err != nil {
		return SessionResponse{}, err
	}
	if err := service.refreshes.Store(ctx, record); err != nil {
		return SessionResponse{}, err
	}
	return sessionResponse(tokens), nil
}

func (service *SessionService) rotateSession(ctx context.Context, user authdomain.User, oldJTI string, now uint64) (SessionResponse, error) {
	tokens, err := service.issueTokens(user)
	if err != nil {
		return SessionResponse{}, err
	}
	replacement, err := refreshTokenRecord(user.ID, tokens, now)
	if err != nil {
		return SessionResponse{}, err
	}
	rotated, err := service.refreshes.Rotate(ctx, oldJTI, replacement)
	if err != nil {
		return SessionResponse{}, err
	}
	if !rotated {
		return SessionResponse{}, fault.Unauthorized()
	}
	return sessionResponse(tokens), nil
}

func (service *SessionService) issueTokens(user authdomain.User) (TokenPair, error) {
	role, err := roleString(user.Role)
	if err != nil {
		return TokenPair{}, err
	}
	return service.tokens.Issue(user.ID, role, user.TokenVersion)
}

func refreshTokenRecord(userID string, tokens TokenPair, now uint64) (authdomain.RefreshToken, error) {
	if tokens.RefreshExpiresIn > math.MaxUint64-now {
		return authdomain.RefreshToken{}, fault.Internal(fmt.Errorf("refresh token expiry overflows unix timestamp"))
	}
	refreshHash := sha256.Sum256([]byte(tokens.RefreshToken))
	return authdomain.RefreshToken{
		JTI:       tokens.RefreshJTI,
		UserID:    userID,
		TokenHash: hex.EncodeToString(refreshHash[:]),
		ExpiresAt: now + tokens.RefreshExpiresIn,
		CreatedAt: now,
	}, nil
}

func sessionResponse(tokens TokenPair) SessionResponse {
	return SessionResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    tokens.ExpiresIn,
	}
}

func roleString(role identity.RequesterRole) (string, error) {
	switch role {
	case identity.RoleAdmin:
		return "admin", nil
	case identity.RoleUser:
		return "user", nil
	default:
		return "", fault.Internal(fmt.Errorf("unsupported user role %d", role))
	}
}

func unixTimeNow() (uint64, error) {
	seconds := time.Now().Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("system time is before unix epoch")
	}
	return uint64(seconds), nil
}

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

const dummyPassword = "retroxide-dummy-password"

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
	role, err := roleString(user.Role)
	if err != nil {
		return SessionResponse{}, err
	}
	return service.issueSession(ctx, user.ID, role)
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
		if err := service.refreshes.RevokeAllForUser(ctx, record.UserID); err != nil {
			return SessionResponse{}, err
		}
		return SessionResponse{}, fault.Unauthorized()
	}
	user, err := service.users.FindByID(ctx, record.UserID)
	if err != nil {
		return SessionResponse{}, err
	}
	if user == nil {
		return SessionResponse{}, fault.Unauthorized()
	}
	role, err := roleString(user.Role)
	if err != nil {
		return SessionResponse{}, err
	}
	return service.rotateSessionAt(ctx, user.ID, role, *claims.JTI, now)
}

func (service *SessionService) Logout(ctx context.Context, requester identity.Requester) error {
	return service.refreshes.RevokeAllForUser(ctx, requester.UserID)
}

func (service *SessionService) dummyPasswordHash() (string, error) {
	service.dummyOnce.Do(func() {
		service.dummyHash, service.dummyHashErr = service.hasher.Hash(dummyPassword)
	})
	return service.dummyHash, service.dummyHashErr
}

func (service *SessionService) issueSession(ctx context.Context, userID, role string) (SessionResponse, error) {
	now, err := unixTimeNow()
	if err != nil {
		return SessionResponse{}, fault.Internal(err)
	}
	return service.issueSessionAt(ctx, userID, role, now)
}

func (service *SessionService) issueSessionAt(ctx context.Context, userID, role string, now uint64) (SessionResponse, error) {
	tokens, err := service.tokens.Issue(userID, role)
	if err != nil {
		return SessionResponse{}, err
	}
	replacement, err := refreshTokenRecord(userID, tokens, now)
	if err != nil {
		return SessionResponse{}, err
	}
	if err := service.refreshes.Store(ctx, replacement); err != nil {
		return SessionResponse{}, err
	}
	return sessionResponse(tokens), nil
}

func (service *SessionService) rotateSessionAt(ctx context.Context, userID, role, oldJTI string, now uint64) (SessionResponse, error) {
	tokens, err := service.tokens.Issue(userID, role)
	if err != nil {
		return SessionResponse{}, err
	}
	replacement, err := refreshTokenRecord(userID, tokens, now)
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

func refreshTokenRecord(userID string, tokens TokenPair, now uint64) (RefreshToken, error) {
	if tokens.RefreshExpiresIn > math.MaxUint64-now {
		return RefreshToken{}, fault.Internal(fmt.Errorf("refresh token expiry overflows unix timestamp"))
	}
	refreshHash := sha256.Sum256([]byte(tokens.RefreshToken))
	return RefreshToken{
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

type ClientService struct {
	clients ClientRepository
}

func NewClientService(clients ClientRepository) *ClientService {
	return &ClientService{clients: clients}
}

func (service *ClientService) Create(ctx context.Context, requester identity.Requester, name string) (CreateClientResponse, error) {
	if !requester.IsAdmin() {
		return CreateClientResponse{}, fault.Forbidden()
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return CreateClientResponse{}, fault.Internal(fmt.Errorf("generating client token: %w", err))
	}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return CreateClientResponse{}, fault.Internal(fmt.Errorf("generating client id: %w", err))
	}
	token := "rtx_" + hex.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	client := Client{
		ID:        hex.EncodeToString(clientIDBytes),
		UserID:    requester.UserID,
		Name:      name,
		TokenHash: hex.EncodeToString(tokenHash[:]),
	}
	if err := service.clients.SaveClient(ctx, client); err != nil {
		return CreateClientResponse{}, err
	}
	return CreateClientResponse{ID: client.ID, Name: client.Name, Token: token}, nil
}

func (service *ClientService) Revoke(ctx context.Context, requester identity.Requester, clientID string) error {
	if !requester.IsAdmin() {
		return fault.Forbidden()
	}
	return service.clients.DeleteClient(ctx, clientID)
}

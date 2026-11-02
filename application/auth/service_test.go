package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

type authTestUserRepository struct {
	usersByEmail map[string]authdomain.User
	usersByID    map[string]authdomain.User
}

func (repository authTestUserRepository) FindByEmail(_ context.Context, email string) (*authdomain.User, error) {
	user, ok := repository.usersByEmail[email]
	if !ok {
		return nil, nil
	}
	return &user, nil
}

func (repository authTestUserRepository) FindByID(_ context.Context, userID string) (*authdomain.User, error) {
	user, ok := repository.usersByID[userID]
	if !ok {
		return nil, nil
	}
	return &user, nil
}

func (authTestUserRepository) SaveUser(context.Context, authdomain.User) error { return nil }

func (repository authTestUserRepository) IncrementTokenVersion(_ context.Context, userID string) error {
	user, ok := repository.usersByID[userID]
	if !ok {
		return fault.NotFound()
	}
	user.TokenVersion++
	repository.usersByID[userID] = user
	return nil
}

type authTestHasher struct {
	hashCalls   int
	verifyCalls int
}

func (hasher *authTestHasher) Hash(password string) (string, error) {
	hasher.hashCalls++
	return "hashed:" + password, nil
}

func (hasher *authTestHasher) Verify(password, passwordHash string) (bool, error) {
	hasher.verifyCalls++
	return passwordHash == "hashed:"+password, nil
}

type authTestTokenService struct {
	mu     sync.Mutex
	claims map[string]AccessClaims
	issued []string
}

func (service *authTestTokenService) Issue(userID, role string, tokenVersion uint64) (TokenPair, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.issued = append(service.issued, fmt.Sprintf("%s:%s:v%d", userID, role, tokenVersion))
	return TokenPair{
		AccessToken:      "access-token",
		RefreshToken:     "refresh-token",
		ExpiresIn:        900,
		RefreshJTI:       "jti-new",
		RefreshExpiresIn: 604800,
	}, nil
}

func (*authTestTokenService) VerifyAccess(string) (AccessClaims, error) {
	return AccessClaims{}, fault.Unauthorized()
}

func (service *authTestTokenService) VerifyRefresh(token string) (AccessClaims, error) {
	claims, ok := service.claims[token]
	if !ok {
		return AccessClaims{}, fault.Unauthorized()
	}
	return claims, nil
}

type authTestRefreshRepository struct {
	mu         sync.Mutex
	records    map[string]authdomain.RefreshToken
	stored     []authdomain.RefreshToken
	revokedAll []string
}

type concurrentAuthTestRefreshRepository struct {
	*authTestRefreshRepository
	findCount atomic.Int32
	bothFound chan struct{}
	once      sync.Once
}

func (repository *concurrentAuthTestRefreshRepository) FindByJTI(ctx context.Context, jti string) (*authdomain.RefreshToken, error) {
	record, err := repository.authTestRefreshRepository.FindByJTI(ctx, jti)
	if repository.findCount.Add(1) == 2 {
		repository.once.Do(func() { close(repository.bothFound) })
	}
	<-repository.bothFound
	return record, err
}

func (repository *authTestRefreshRepository) Store(_ context.Context, token authdomain.RefreshToken) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.records[token.JTI] = token
	repository.stored = append(repository.stored, token)
	return nil
}

func (repository *authTestRefreshRepository) FindByJTI(_ context.Context, jti string) (*authdomain.RefreshToken, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	token, ok := repository.records[jti]
	if !ok {
		return nil, nil
	}
	return &token, nil
}

func (repository *authTestRefreshRepository) Revoke(_ context.Context, jti string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	token := repository.records[jti]
	token.Revoked = true
	repository.records[jti] = token
	return nil
}

func (repository *authTestRefreshRepository) Rotate(_ context.Context, jti string, replacement authdomain.RefreshToken) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	token, ok := repository.records[jti]
	if !ok || token.Revoked {
		for recordJTI, record := range repository.records {
			if record.UserID == replacement.UserID {
				record.Revoked = true
				repository.records[recordJTI] = record
			}
		}
		return false, nil
	}
	token.Revoked = true
	repository.records[jti] = token
	repository.records[replacement.JTI] = replacement
	repository.stored = append(repository.stored, replacement)
	return true, nil
}

func (repository *authTestRefreshRepository) RevokeAllForUser(_ context.Context, userID string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.revokedAll = append(repository.revokedAll, userID)
	for jti, token := range repository.records {
		if token.UserID == userID {
			token.Revoked = true
			repository.records[jti] = token
		}
	}
	return nil
}

type authTestClientRepository struct {
	client  authdomain.Client
	clients []authdomain.Client
	count   int
}

func (repository *authTestClientRepository) FindByTokenHash(context.Context, string) (*authdomain.Client, error) {
	return nil, nil
}
func (repository *authTestClientRepository) FindByUserID(_ context.Context, userID string) ([]authdomain.Client, error) {
	clients := make([]authdomain.Client, 0, len(repository.clients))
	for _, client := range repository.clients {
		if client.UserID == userID {
			clients = append(clients, client)
		}
	}
	return clients, nil
}
func (repository *authTestClientRepository) SaveClient(_ context.Context, client authdomain.Client) error {
	repository.client = client
	repository.count++
	return nil
}
func (*authTestClientRepository) DeleteClient(context.Context, string) error { return nil }

func TestSessionServiceUnknownEmailVerifiesDummyHash(t *testing.T) {
	hasher := &authTestHasher{}
	service := NewSessionService(authTestUserRepository{}, hasher, &authTestTokenService{}, &authTestRefreshRepository{records: make(map[string]authdomain.RefreshToken)})

	_, err := service.Login(context.Background(), LoginRequest{Email: "missing@example.com", Password: "secret"})
	if !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("Login() error = %v, want unauthorized", err)
	}
	if hasher.hashCalls != 1 || hasher.verifyCalls != 1 {
		t.Fatalf("hash calls=%d verify calls=%d, want one each", hasher.hashCalls, hasher.verifyCalls)
	}
}

func TestSessionServiceLoginStoresRefreshTokenHash(t *testing.T) {
	user := authdomain.User{ID: "user-1", Email: "mario@example.com", PasswordHash: "hashed:secret", Role: identity.RoleAdmin}
	users := authTestUserRepository{
		usersByEmail: map[string]authdomain.User{user.Email: user},
		usersByID:    map[string]authdomain.User{user.ID: user},
	}
	refreshes := &authTestRefreshRepository{records: make(map[string]authdomain.RefreshToken)}
	tokens := &authTestTokenService{}
	service := NewSessionService(users, &authTestHasher{}, tokens, refreshes)

	response, err := service.Login(context.Background(), LoginRequest{Email: user.Email, Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if response.TokenType != "Bearer" || response.ExpiresIn != 900 || len(refreshes.stored) != 1 {
		t.Fatalf("response = %+v stored tokens=%d", response, len(refreshes.stored))
	}
	expectedHash := sha256.Sum256([]byte(response.RefreshToken))
	if refreshes.stored[0].TokenHash != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("stored token hash = %q, want SHA-256 of refresh token", refreshes.stored[0].TokenHash)
	}
	if len(tokens.issued) != 1 || tokens.issued[0] != "user-1:admin:v0" {
		t.Fatalf("issued identities = %v", tokens.issued)
	}
}

func TestSessionServiceRefreshReplayRevokesFamily(t *testing.T) {
	user := authdomain.User{ID: "user-1", Email: "mario@example.com", Role: identity.RoleUser}
	users := authTestUserRepository{usersByID: map[string]authdomain.User{user.ID: user}}
	refreshes := &authTestRefreshRepository{
		records: map[string]authdomain.RefreshToken{
			"jti-old":   {JTI: "jti-old", UserID: user.ID, ExpiresAt: uint64(time.Now().Add(time.Hour).Unix()), Revoked: true},
			"jti-newer": {JTI: "jti-newer", UserID: user.ID, ExpiresAt: uint64(time.Now().Add(time.Hour).Unix())},
		},
	}
	tokens := &authTestTokenService{claims: map[string]AccessClaims{
		"replayed": {UserID: user.ID, JTI: authStringPointer("jti-old")},
	}}
	service := NewSessionService(users, &authTestHasher{}, tokens, refreshes)

	_, err := service.Refresh(context.Background(), "replayed")
	if !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("Refresh() error = %v, want unauthorized", err)
	}
	if len(refreshes.revokedAll) != 1 || refreshes.revokedAll[0] != user.ID || !refreshes.records["jti-newer"].Revoked {
		t.Fatalf("family revocation = %v records=%+v", refreshes.revokedAll, refreshes.records)
	}
	if version := users.usersByID[user.ID].TokenVersion; version != 1 {
		t.Fatalf("token version after replay = %d, want 1 so access tokens are invalidated", version)
	}
}

func TestSessionServiceLogoutInvalidatesAccessAndRefreshTokens(t *testing.T) {
	user := authdomain.User{ID: "user-1", Email: "mario@example.com", Role: identity.RoleUser, TokenVersion: 4}
	users := authTestUserRepository{usersByID: map[string]authdomain.User{user.ID: user}}
	refreshes := &authTestRefreshRepository{records: map[string]authdomain.RefreshToken{
		"jti-live": {JTI: "jti-live", UserID: user.ID, ExpiresAt: uint64(time.Now().Add(time.Hour).Unix())},
	}}
	service := NewSessionService(users, &authTestHasher{}, &authTestTokenService{}, refreshes)

	if err := service.Logout(context.Background(), identity.Requester{UserID: user.ID, Role: identity.RoleUser}); err != nil {
		t.Fatal(err)
	}
	if version := users.usersByID[user.ID].TokenVersion; version != 5 {
		t.Fatalf("token version after logout = %d, want 5", version)
	}
	if !refreshes.records["jti-live"].Revoked {
		t.Fatal("refresh token survived logout")
	}
}

func TestSessionServiceRefreshRejectsTokenFromEarlierVersion(t *testing.T) {
	user := authdomain.User{ID: "user-1", Email: "mario@example.com", Role: identity.RoleUser, TokenVersion: 2}
	users := authTestUserRepository{usersByID: map[string]authdomain.User{user.ID: user}}
	refreshes := &authTestRefreshRepository{records: map[string]authdomain.RefreshToken{
		"jti-old": {JTI: "jti-old", UserID: user.ID, ExpiresAt: uint64(time.Now().Add(time.Hour).Unix())},
	}}
	tokens := &authTestTokenService{claims: map[string]AccessClaims{
		"stale": {UserID: user.ID, TokenVersion: 1, JTI: authStringPointer("jti-old")},
	}}
	service := NewSessionService(users, &authTestHasher{}, tokens, refreshes)

	if _, err := service.Refresh(context.Background(), "stale"); !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("Refresh(stale version) error = %v, want unauthorized", err)
	}
	if len(tokens.issued) != 0 {
		t.Fatalf("issued tokens for stale refresh: %v", tokens.issued)
	}
}

func TestSessionServiceConcurrentRefreshHasSingleConsumer(t *testing.T) {
	user := authdomain.User{ID: "user-1", Email: "mario@example.com", Role: identity.RoleUser}
	users := authTestUserRepository{usersByID: map[string]authdomain.User{user.ID: user}}
	refreshes := &concurrentAuthTestRefreshRepository{
		authTestRefreshRepository: &authTestRefreshRepository{records: map[string]authdomain.RefreshToken{
			"jti-old": {JTI: "jti-old", UserID: user.ID, ExpiresAt: uint64(time.Now().Add(time.Hour).Unix())},
		}},
		bothFound: make(chan struct{}),
	}
	tokens := &authTestTokenService{claims: map[string]AccessClaims{
		"refresh": {UserID: user.ID, JTI: authStringPointer("jti-old")},
	}}
	service := NewSessionService(users, &authTestHasher{}, tokens, refreshes)

	var workers sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := service.Refresh(context.Background(), "refresh")
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	var successes int
	var unauthorized int
	for err := range results {
		if err == nil {
			successes++
		} else if fault.IsCode(err, fault.CodeUnauthorized) {
			unauthorized++
		} else {
			t.Fatalf("Refresh() error = %v, want success or unauthorized", err)
		}
	}
	if successes != 1 || unauthorized != 1 {
		t.Fatalf("refresh outcomes: successes=%d unauthorized=%d, want 1 each", successes, unauthorized)
	}
}

func TestClientServiceStoresOnlyHashAndReturnsPlainTokenOnce(t *testing.T) {
	repository := &authTestClientRepository{}
	service := NewClientService(repository)
	response, err := service.Create(context.Background(), identity.Requester{UserID: "admin-1", Role: identity.RoleAdmin}, "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if repository.count != 1 || response.Token == "" || repository.client.TokenHash == response.Token {
		t.Fatalf("client = %+v response = %+v", repository.client, response)
	}
	if got := sha256.Sum256([]byte(response.Token)); hex.EncodeToString(got[:]) != repository.client.TokenHash {
		t.Fatalf("stored token hash = %q", repository.client.TokenHash)
	}
	if repository.client.UserID != "admin-1" || repository.client.Name != "frontend" {
		t.Fatalf("stored client = %+v", repository.client)
	}
}

func TestClientServiceListsOnlyForAdminWithoutTokenSecrets(t *testing.T) {
	repository := &authTestClientRepository{clients: []authdomain.Client{
		{ID: "client-1", UserID: "admin-1", Name: "desktop", TokenHash: "secret-hash"},
		{ID: "client-2", UserID: "other-user", Name: "other", TokenHash: "other-hash"},
	}}
	service := NewClientService(repository)

	clients, err := service.List(context.Background(), identity.Requester{UserID: "admin-1", Role: identity.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(clients.Item) != 1 || clients.Item[0].ID != "client-1" || clients.Item[0].Name != "desktop" {
		t.Fatalf("clients = %+v, want only client-1 metadata", clients)
	}
	encoded, err := json.Marshal(clients)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "token") || strings.Contains(string(encoded), "secret-hash") {
		t.Fatalf("client list leaked token material: %s", encoded)
	}

	_, err = service.List(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser})
	if !fault.IsCode(err, fault.CodeForbidden) {
		t.Fatalf("user list error = %v, want forbidden", err)
	}
}

func authStringPointer(value string) *string { return &value }

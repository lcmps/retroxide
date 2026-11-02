package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

type principalTestTokens struct {
	claims AccessClaims
	err    error
}

func (tokens principalTestTokens) Issue(string, string, uint64) (TokenPair, error) {
	return TokenPair{}, nil
}
func (tokens principalTestTokens) VerifyAccess(string) (AccessClaims, error) {
	return tokens.claims, tokens.err
}
func (tokens principalTestTokens) VerifyRefresh(string) (AccessClaims, error) {
	return AccessClaims{}, fault.Unauthorized()
}

type principalTestUsers map[string]authdomain.User

func (users principalTestUsers) FindByID(_ context.Context, userID string) (*authdomain.User, error) {
	user, ok := users[userID]
	if !ok {
		return nil, nil
	}
	return &user, nil
}

type principalTestClients struct {
	client  *authdomain.Client
	gotHash string
	err     error
}

func (clients *principalTestClients) FindByTokenHash(_ context.Context, hash string) (*authdomain.Client, error) {
	clients.gotHash = hash
	return clients.client, clients.err
}
func (*principalTestClients) FindByUserID(context.Context, string) ([]authdomain.Client, error) {
	return nil, nil
}
func (*principalTestClients) SaveClient(context.Context, authdomain.Client) error { return nil }
func (*principalTestClients) DeleteClient(context.Context, string) error          { return nil }

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
		valid  bool
	}{
		{name: "standard", header: "Bearer access", want: "access", valid: true},
		{name: "case insensitive and whitespace", header: "  bEaReR   token  ", want: "token", valid: true},
		{name: "wrong scheme", header: "Basic token"},
		{name: "empty bearer", header: "Bearer "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ExtractBearerToken(test.header)
			if got != test.want || ok != test.valid {
				t.Fatalf("ExtractBearerToken() = %q, %t; want %q, %t", got, ok, test.want, test.valid)
			}
		})
	}
}

func TestAuthenticatorPrefersVerifiedJWT(t *testing.T) {
	clients := &principalTestClients{client: &authdomain.Client{ID: "client-1", UserID: "owner-1"}}
	users := principalTestUsers{"admin-1": {ID: "admin-1", Role: identity.RoleAdmin}}
	authenticator := NewAuthenticator(principalTestTokens{claims: AccessClaims{UserID: "admin-1", Role: "admin"}}, users, clients)

	requester, err := authenticator.Authenticate(context.Background(), "signed")
	if err != nil {
		t.Fatal(err)
	}
	if requester.UserID != "admin-1" || requester.Role != identity.RoleAdmin || clients.gotHash != "" {
		t.Fatalf("requester=%+v client lookup hash=%q", requester, clients.gotHash)
	}
}

func TestAuthenticatorTakesRoleFromCurrentUserRecord(t *testing.T) {
	users := principalTestUsers{"user-1": {ID: "user-1", Role: identity.RoleUser}}
	authenticator := NewAuthenticator(principalTestTokens{claims: AccessClaims{UserID: "user-1", Role: "admin"}}, users, &principalTestClients{})

	requester, err := authenticator.Authenticate(context.Background(), "signed")
	if err != nil {
		t.Fatal(err)
	}
	if requester.Role != identity.RoleUser {
		t.Fatalf("role = %v, want the demoted database role", requester.Role)
	}
}

func TestAuthenticatorRejectsRevokedOrOrphanedAccessTokens(t *testing.T) {
	users := principalTestUsers{"user-1": {ID: "user-1", Role: identity.RoleUser, TokenVersion: 3}}
	clients := &principalTestClients{client: &authdomain.Client{ID: "client-1", UserID: "user-1"}}
	for name, claims := range map[string]AccessClaims{
		"issued before logout": {UserID: "user-1", Role: "user", TokenVersion: 2},
		"deleted user":         {UserID: "gone", Role: "user"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewAuthenticator(principalTestTokens{claims: claims}, users, clients).Authenticate(context.Background(), "signed")
			if !fault.IsCode(err, fault.CodeUnauthorized) {
				t.Fatalf("Authenticate() error = %v, want unauthorized", err)
			}
			if clients.gotHash != "" {
				t.Fatal("a verified but revoked JWT fell back to client-token lookup")
			}
		})
	}
}

func TestAuthenticatorFallsBackToHashedClientToken(t *testing.T) {
	plainToken := "rtx-secret"
	digest := sha256.Sum256([]byte(plainToken))
	expectedHash := hex.EncodeToString(digest[:])
	clients := &principalTestClients{client: &authdomain.Client{ID: "client-1", UserID: "owner-1"}}
	authenticator := NewAuthenticator(principalTestTokens{err: fault.Unauthorized()}, principalTestUsers{}, clients)

	requester, err := authenticator.Authenticate(context.Background(), plainToken)
	if err != nil {
		t.Fatal(err)
	}
	if requester.UserID != "owner-1" || requester.Role != identity.RoleClient || clients.gotHash != expectedHash {
		t.Fatalf("requester=%+v lookup hash=%q, want %q", requester, clients.gotHash, expectedHash)
	}
}

func TestAuthenticatorRejectsUnknownToken(t *testing.T) {
	clients := &principalTestClients{err: errors.New("database offline")}
	authenticator := NewAuthenticator(principalTestTokens{err: fault.Unauthorized()}, principalTestUsers{}, clients)
	if _, err := authenticator.Authenticate(context.Background(), "bad"); !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("Authenticate() error = %v, want unauthorized", err)
	}
}

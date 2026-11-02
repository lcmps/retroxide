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

func (tokens principalTestTokens) Issue(string, string) (TokenPair, error) {
	return TokenPair{}, nil
}
func (tokens principalTestTokens) VerifyAccess(string) (AccessClaims, error) {
	return tokens.claims, tokens.err
}
func (tokens principalTestTokens) VerifyRefresh(string) (AccessClaims, error) {
	return AccessClaims{}, fault.Unauthorized()
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

func TestResolvePrincipalPrefersVerifiedJWT(t *testing.T) {
	clients := &principalTestClients{client: &authdomain.Client{ID: "client-1", UserID: "owner-1"}}
	requester, err := ResolvePrincipal(context.Background(), "signed", principalTestTokens{
		claims: AccessClaims{UserID: "admin-1", Role: "admin"},
	}, clients)
	if err != nil {
		t.Fatal(err)
	}
	if requester.UserID != "admin-1" || requester.Role != identity.RoleAdmin || clients.gotHash != "" {
		t.Fatalf("requester=%+v client lookup hash=%q", requester, clients.gotHash)
	}
}

func TestResolvePrincipalFallsBackToHashedClientToken(t *testing.T) {
	plainToken := "rtx-secret"
	digest := sha256.Sum256([]byte(plainToken))
	expectedHash := hex.EncodeToString(digest[:])
	clients := &principalTestClients{client: &authdomain.Client{ID: "client-1", UserID: "owner-1"}}
	requester, err := ResolvePrincipal(context.Background(), plainToken, principalTestTokens{err: fault.Unauthorized()}, clients)
	if err != nil {
		t.Fatal(err)
	}
	if requester.UserID != "owner-1" || requester.Role != identity.RoleClient || clients.gotHash != expectedHash {
		t.Fatalf("requester=%+v lookup hash=%q, want %q", requester, clients.gotHash, expectedHash)
	}
}

func TestResolvePrincipalRejectsUnknownToken(t *testing.T) {
	clients := &principalTestClients{err: errors.New("database offline")}
	_, err := ResolvePrincipal(context.Background(), "bad", principalTestTokens{err: fault.Unauthorized()}, clients)
	if !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("ResolvePrincipal() error = %v, want unauthorized", err)
	}
}

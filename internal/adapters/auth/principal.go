package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	authfeature "github.com/lcmps/retroxide/internal/features/auth"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type clientTokenReader interface {
	FindByTokenHash(ctx context.Context, tokenHash string) (*authfeature.Client, error)
}

func ExtractBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if len(header) <= len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(header[len("Bearer "):])
	if token == "" {
		return "", false
	}
	return token, true
}

func ResolvePrincipal(ctx context.Context, token string, tokens authfeature.TokenService, clients clientTokenReader) (identity.Requester, error) {
	claims, err := tokens.VerifyAccess(token)
	if err == nil {
		role := identity.RoleUser
		if claims.Role == "admin" {
			role = identity.RoleAdmin
		}
		return identity.Requester{UserID: claims.UserID, Role: role}, nil
	}

	digest := sha256.Sum256([]byte(token))
	client, err := clients.FindByTokenHash(ctx, hex.EncodeToString(digest[:]))
	if err != nil || client == nil {
		return identity.Requester{}, fault.Unauthorized()
	}
	return identity.Requester{UserID: client.UserID, Role: identity.RoleClient}, nil
}

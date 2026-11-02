package auth

import "github.com/lcmps/retroxide/domain/identity"

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         identity.RequesterRole
	// TokenVersion invalidates every outstanding token when incremented; tokens carry the version they were issued under.
	TokenVersion uint64
}

type Client struct {
	ID        string
	UserID    string
	Name      string
	TokenHash string
}

type RefreshToken struct {
	JTI       string
	UserID    string
	TokenHash string
	ExpiresAt uint64
	Revoked   bool
	CreatedAt uint64
}

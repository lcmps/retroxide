package auth

import "github.com/lcmps/retroxide/domain/identity"

type User struct {
	ID    string
	Email string
	// Username is an optional, lowercase sign-in name used instead of the email.
	Username     *string
	PasswordHash string
	Role         identity.RequesterRole
	// TokenVersion invalidates every outstanding token when incremented; tokens carry the version they were issued under.
	TokenVersion uint64
	// Locale is the interface language the user chose, such as "pt-BR"; nil follows the browser.
	Locale *string
	// Font is the interface font the user chose (see the appearance package); nil uses the
	// application default.
	Font *string
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

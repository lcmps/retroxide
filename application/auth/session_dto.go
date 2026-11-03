package auth

import "strings"

// LoginRequest signs in with an email or a username in Login; Email is still accepted for clients
// that predate usernames.
type LoginRequest struct {
	Login    string `json:"login"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Identifier is the email or username to sign in with.
func (request LoginRequest) Identifier() string {
	if login := strings.TrimSpace(request.Login); login != "" {
		return login
	}
	return strings.TrimSpace(request.Email)
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type SessionResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    uint64 `json:"expires_in"`
}

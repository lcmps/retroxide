package auth

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
)

const MaxEmailLength = 255

func ValidateEmail(email string) error {
	if email == "" {
		return fault.Validation("email cannot be empty")
	}
	if len(email) > MaxEmailLength {
		return fault.Validation(fmt.Sprintf("email exceeds %d characters", MaxEmailLength))
	}
	if !strings.Contains(email, "@") {
		return fault.Validation("email must contain '@' symbol")
	}
	return nil
}

// localeTag accepts language tags such as "en" or "pt-BR".
var localeTag = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// ValidateLocale checks a language tag's shape; whether it is supported is up to the caller.
func ValidateLocale(locale string) error {
	if !localeTag.MatchString(locale) {
		return fault.Validation("locale must be a language tag such as en or pt-BR")
	}
	return nil
}

const (
	MinUsernameLength = 3
	MaxUsernameLength = 32
)

// usernamePattern allows lowercase letters, digits, and . _ - after a leading letter or digit.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// NormalizeUsername lowercases and trims a username; it is stored and compared in that form.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ValidateUsername checks a normalized username. It can never contain '@', so a sign-in name is
// unambiguously either an email or a username.
func ValidateUsername(username string) error {
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return fault.Validation(fmt.Sprintf("username must be %d to %d characters", MinUsernameLength, MaxUsernameLength))
	}
	if !usernamePattern.MatchString(username) {
		return fault.Validation("username may only contain letters, digits, dots, dashes, and underscores, starting with a letter or digit")
	}
	return nil
}

package auth

import (
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/internal/shared/fault"
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

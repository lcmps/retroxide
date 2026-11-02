package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func New(prefix string) (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(randomBytes[:]), nil
}

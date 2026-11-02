package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = uint32(19 * 1024)
	argonIterations  = uint32(2)
	argonParallelism = uint8(1)
	argonKeyLength   = uint32(32)
	argonSaltLength  = 16
)

type Argon2PasswordHasher struct{}

func NewArgon2PasswordHasher() Argon2PasswordHasher {
	return Argon2PasswordHasher{}
}

func (Argon2PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating password salt: %w", err)
	}
	derived := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

func (Argon2PasswordHasher) Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid argon2id password hash")
	}
	parameters, err := parseParameters(parts[3])
	if err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, errors.New("invalid argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != int(argonKeyLength) {
		return false, errors.New("invalid argon2id digest")
	}
	actual := argon2.IDKey([]byte(password), salt, parameters.iterations, parameters.memory, parameters.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type argonParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseParameters(encoded string) (argonParameters, error) {
	values := make(map[string]uint64, 3)
	for _, parameter := range strings.Split(encoded, ",") {
		key, value, ok := strings.Cut(parameter, "=")
		if !ok {
			return argonParameters{}, errors.New("invalid argon2id parameters")
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argonParameters{}, errors.New("invalid argon2id parameter value")
		}
		values[key] = parsed
	}
	memory := values["m"]
	iterations := values["t"]
	parallelism := values["p"]
	if memory < 8*parallelism || memory > 1024*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return argonParameters{}, errors.New("argon2id parameters are outside supported bounds")
	}
	return argonParameters{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
	}, nil
}

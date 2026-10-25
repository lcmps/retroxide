package auth

import (
	"strings"
	"testing"
)

func TestArgon2PasswordHasher(t *testing.T) {
	hasher := NewArgon2PasswordHasher()
	hash, err := hasher.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash PHC prefix = %q", hash)
	}
	valid, err := hasher.Verify("correct horse", hash)
	if err != nil || !valid {
		t.Fatalf("Verify(correct password) = %t, %v", valid, err)
	}
	valid, err = hasher.Verify("wrong password", hash)
	if err != nil || valid {
		t.Fatalf("Verify(wrong password) = %t, %v, want false without error", valid, err)
	}
}

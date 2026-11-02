package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

func TestDownloadLinkRoundTripsAndRejectsTamperingAndExpiry(t *testing.T) {
	signer := NewDownloadLinkSigner(strings.Repeat("s", 32), time.Minute)
	now := time.Unix(1_700_000_000, 0)
	signer.now = func() time.Time { return now }
	requester := identity.Requester{UserID: "user-1", Role: identity.RoleUser}

	token, expiresAt, err := signer.Sign(requester, "rom-1")
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiresAt = %v", expiresAt)
	}
	gotRequester, romID, err := signer.Verify(token)
	if err != nil || gotRequester != requester || romID != "rom-1" {
		t.Fatalf("Verify() = %+v %q %v", gotRequester, romID, err)
	}

	payload, signature, _ := strings.Cut(token, ".")
	otherSigner := NewDownloadLinkSigner(strings.Repeat("x", 32), time.Minute)
	otherSigner.now = signer.now
	forged, _, _ := otherSigner.Sign(identity.Requester{UserID: "user-1", Role: identity.RoleAdmin}, "rom-1")
	for name, candidate := range map[string]string{
		"tampered payload":   strings.ToUpper(payload[:1]) + payload[1:] + "." + signature,
		"wrong secret":       forged,
		"missing signature":  payload,
		"garbage":            "not-a-token",
		"reused JWT-ish key": payload + "." + signature + "x",
	} {
		if _, _, err := signer.Verify(candidate); !fault.IsCode(err, fault.CodeUnauthorized) {
			t.Errorf("%s: Verify() error = %v, want unauthorized", name, err)
		}
	}

	now = now.Add(time.Minute + time.Second)
	if _, _, err := signer.Verify(token); !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("expired token error = %v, want unauthorized", err)
	}
}

package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

func TestImageLinkRoundTripsAndIsStableWithinAWindow(t *testing.T) {
	signer := NewImageLinkSigner(strings.Repeat("s", 32), 24*time.Hour)
	now := time.Unix(1_700_000_000, 0)
	signer.now = func() time.Time { return now }
	requester := identity.Requester{UserID: "user-1", Role: identity.RoleUser}

	first := signer.Sign(requester, "rom-1")
	gotRequester, romID, err := signer.Verify(first)
	if err != nil || gotRequester != requester || romID != "rom-1" {
		t.Fatalf("Verify() = %+v %q %v", gotRequester, romID, err)
	}
	// The same link within the window, so the browser cache keeps answering for it.
	now = now.Add(time.Hour)
	if again := signer.Sign(requester, "rom-1"); again != first {
		t.Fatal("a token signed later in the same window differs, which defeats the browser cache")
	}
	if other := signer.Sign(identity.Requester{UserID: "user-2", Role: identity.RoleUser}, "rom-1"); other == first {
		t.Fatal("tokens for different users are identical")
	}
	// A token outlives its window by one more, then expires.
	now = now.Add(24 * time.Hour)
	if _, _, err := signer.Verify(first); err != nil {
		t.Fatalf("token in the following window = %v, want valid", err)
	}
	now = now.Add(24 * time.Hour)
	if _, _, err := signer.Verify(first); !fault.IsCode(err, fault.CodeUnauthorized) {
		t.Fatalf("token two windows on = %v, want unauthorized", err)
	}
}

func TestImageLinkRejectsTampering(t *testing.T) {
	signer := NewImageLinkSigner(strings.Repeat("s", 32), 24*time.Hour)
	token := signer.Sign(identity.Requester{UserID: "user-1", Role: identity.RoleUser}, "rom-1")
	payload, signature, _ := strings.Cut(token, ".")
	forged := NewImageLinkSigner(strings.Repeat("x", 32), 24*time.Hour).Sign(identity.Requester{UserID: "user-1", Role: identity.RoleAdmin}, "rom-1")
	downloadToken, _, _ := NewDownloadLinkSigner(strings.Repeat("s", 32), time.Minute).Sign(identity.Requester{UserID: "user-1", Role: identity.RoleUser}, "rom-1")
	for name, candidate := range map[string]string{
		"tampered payload":  strings.ToUpper(payload[:1]) + payload[1:] + "." + signature,
		"wrong secret":      forged,
		"missing signature": payload,
		"garbage":           "not-a-token",
		"download token":    downloadToken,
	} {
		if _, _, err := signer.Verify(candidate); !fault.IsCode(err, fault.CodeUnauthorized) {
			t.Errorf("%s: Verify() error = %v, want unauthorized", name, err)
		}
	}
}

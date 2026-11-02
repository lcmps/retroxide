package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

// downloadLinkKeyLabel separates the download-link key from the JWT key derived from the same secret.
const downloadLinkKeyLabel = "retroxide/download-link/v1"

// DownloadLinkSigner issues short-lived, signed capabilities for downloading one ROM, so browsers
// can stream a download natively without attaching a bearer token.
type DownloadLinkSigner struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

type downloadLinkClaims struct {
	ROMID   string                 `json:"rom"`
	UserID  string                 `json:"sub"`
	Role    identity.RequesterRole `json:"role"`
	Expires int64                  `json:"exp"`
}

func NewDownloadLinkSigner(secret string, ttl time.Duration) *DownloadLinkSigner {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(downloadLinkKeyLabel))
	return &DownloadLinkSigner{key: mac.Sum(nil), ttl: ttl, now: time.Now}
}

// Sign returns a token letting requester download romID until the returned expiry.
func (signer *DownloadLinkSigner) Sign(requester identity.Requester, romID string) (string, time.Time, error) {
	expiresAt := signer.now().Add(signer.ttl)
	payload, err := json.Marshal(downloadLinkClaims{ROMID: romID, UserID: requester.UserID, Role: requester.Role, Expires: expiresAt.Unix()})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encoding download link: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signer.signature(encoded)), expiresAt, nil
}

// Verify returns the requester and ROM a token was issued for, or unauthorized when the token is
// malformed, tampered with, or expired.
func (signer *DownloadLinkSigner) Verify(token string) (identity.Requester, string, error) {
	encoded, signature, found := strings.Cut(token, ".")
	if !found {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, signer.signature(encoded)) {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	var claims downloadLinkClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ROMID == "" || claims.UserID == "" {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	if signer.now().Unix() > claims.Expires {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	return identity.Requester{UserID: claims.UserID, Role: claims.Role}, claims.ROMID, nil
}

func (signer *DownloadLinkSigner) signature(encodedPayload string) []byte {
	mac := hmac.New(sha256.New, signer.key)
	mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}

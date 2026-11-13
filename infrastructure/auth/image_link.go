package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

// imageLinkKeyLabel separates the image-link key from the other keys derived from the secret.
const imageLinkKeyLabel = "retroxide/image-link/v1"

// ImageLinkSigner signs the image URLs the API hands out, so a browser can load covers with
// plain <img> tags instead of fetching them with a bearer token. A token binds a requester to one
// ROM; access is checked again when the image is served.
//
// Tokens expire on a schedule rather than a fixed time after signing: every token signed within
// one window of the clock (a day, say) is identical and lasts until the end of the window after
// it. Image URLs therefore stay the same for a whole window, which lets the browser's cache answer
// repeat requests, and a page open across a window boundary keeps working for one more window.
type ImageLinkSigner struct {
	key    []byte
	window time.Duration
	now    func() time.Time
}

type imageLinkClaims struct {
	ROMID   string                 `json:"rom"`
	UserID  string                 `json:"sub"`
	Role    identity.RequesterRole `json:"role"`
	Expires int64                  `json:"exp"`
}

func NewImageLinkSigner(secret string, window time.Duration) *ImageLinkSigner {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(imageLinkKeyLabel))
	return &ImageLinkSigner{key: mac.Sum(nil), window: window, now: time.Now}
}

// Sign returns the token letting requester load romID's images during the current window.
func (signer *ImageLinkSigner) Sign(requester identity.Requester, romID string) string {
	expiresAt := signer.now().Truncate(signer.window).Add(2 * signer.window)
	payload, err := json.Marshal(imageLinkClaims{ROMID: romID, UserID: requester.UserID, Role: requester.Role, Expires: expiresAt.Unix()})
	if err != nil {
		return "" // the claims are plain strings and integers; this cannot happen
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signer.signature(encoded))
}

// Verify returns the requester and ROM a token was issued for, or unauthorized when the token is
// malformed, tampered with, or expired.
func (signer *ImageLinkSigner) Verify(token string) (identity.Requester, string, error) {
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
	var claims imageLinkClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ROMID == "" || claims.UserID == "" {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	if signer.now().Unix() >= claims.Expires {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	return identity.Requester{UserID: claims.UserID, Role: claims.Role}, claims.ROMID, nil
}

// SignImage is Sign under the name the ROM service expects of its ImageSigner.
func (signer *ImageLinkSigner) SignImage(requester identity.Requester, romID string) string {
	return signer.Sign(requester, romID)
}

func (signer *ImageLinkSigner) signature(encodedPayload string) []byte {
	mac := hmac.New(sha256.New, signer.key)
	mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}

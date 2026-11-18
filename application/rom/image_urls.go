package rom

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// imageVersionLength is how many hex digits of an image's SHA-256 its URL carries.
const imageVersionLength = 16

// ImageVersionParam is the query parameter carrying an image's content version. A URL that
// includes it names immutable content, so clients may cache the response indefinitely.
const ImageVersionParam = "v"

// ImageTokenParam is the query parameter carrying the signed token that lets a browser load an
// image without a bearer token (see ImageSigner).
const ImageTokenParam = "token"

// ImageSigner signs image URLs for a requester, so they can be loaded by plain <img> tags.
type ImageSigner interface {
	SignImage(requester identity.Requester, romID string) string
}

// ImageSign returns the token for one ROM's images; nil leaves URLs unsigned.
type ImageSign func(romID string) string

// imageSigner returns the ImageSign for requester, or nil when the service has no signer.
func (service *Service) imageSigner(requester identity.Requester) ImageSign {
	if service.images == nil || requester.UserID == "" {
		return nil
	}
	return func(romID string) string { return service.images.SignImage(requester, romID) }
}

// imageURL returns the API URL of a stored image, versioned by its content so the URL changes
// whenever the image does, and carrying token when one is given. Images are stored at their
// SHA-256 content address.
func imageURL(romID, resource string, storagePath *string, token string) *string {
	if storagePath == nil {
		return nil
	}
	var query []string
	if address := filepath.Base(*storagePath); len(address) >= imageVersionLength && isLowerHex(address) {
		query = append(query, ImageVersionParam+"="+address[:imageVersionLength])
	}
	if token != "" {
		query = append(query, ImageTokenParam+"="+url.QueryEscape(token))
	}
	result := "/api/v1/rom/" + romID + "/" + resource
	if len(query) > 0 {
		result += "?" + strings.Join(query, "&")
	}
	return &result
}

// signFor returns the token sign gives for romID, or "" without a signer.
func signFor(sign ImageSign, romID string) string {
	if sign == nil {
		return ""
	}
	return sign(romID)
}

// coverThumbURL returns the URL of a ROM's cover thumbnail, or nil without a cover. Until the
// thumbnail exists it is versioned by the cover it will be made from.
func coverThumbURL(rom romdomain.ROM, token string) *string {
	if rom.Identity.CoverArtPath == nil {
		return nil
	}
	if rom.Identity.CoverThumbPath != nil {
		return imageURL(rom.ID, "cover/thumb", rom.Identity.CoverThumbPath, token)
	}
	return imageURL(rom.ID, "cover/thumb", rom.Identity.CoverArtPath, token)
}

// ImageVersionMatches reports whether version is the content version of an image with the
// given SHA-256 hex digest.
func ImageVersionMatches(version, sha256Hex string) bool {
	return len(version) == imageVersionLength && strings.HasPrefix(sha256Hex, version)
}

func isLowerHex(value string) bool {
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

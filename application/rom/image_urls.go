package rom

import (
	"path/filepath"
	"strings"
)

// imageVersionLength is how many hex digits of an image's SHA-256 its URL carries.
const imageVersionLength = 16

// ImageVersionParam is the query parameter carrying an image's content version. A URL that
// includes it names immutable content, so clients may cache the response indefinitely.
const ImageVersionParam = "v"

// imageURL returns the API URL of a stored image, versioned by its content so the URL changes
// whenever the image does. Images are stored at their SHA-256 content address.
func imageURL(romID, resource string, storagePath *string) *string {
	if storagePath == nil {
		return nil
	}
	url := "/api/v1/rom/" + romID + "/" + resource
	if address := filepath.Base(*storagePath); len(address) >= imageVersionLength && isLowerHex(address) {
		url += "?" + ImageVersionParam + "=" + address[:imageVersionLength]
	}
	return &url
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

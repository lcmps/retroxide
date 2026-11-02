package rom

import (
	"strings"
	"testing"
)

func TestImageURLIsVersionedByContentAddress(t *testing.T) {
	address := strings.Repeat("ab", 32)
	path := "/storage/" + address
	url := imageURL("rom-1", "cover", &path)
	if url == nil || *url != "/api/v1/rom/rom-1/cover?v="+address[:imageVersionLength] {
		t.Fatalf("imageURL = %v", url)
	}
	if !ImageVersionMatches(address[:imageVersionLength], address) || ImageVersionMatches("abab", address) || ImageVersionMatches(strings.Repeat("0", imageVersionLength), address) {
		t.Fatal("ImageVersionMatches accepted a wrong or short version")
	}
	legacy := "/storage/cover.png"
	if url := imageURL("rom-1", "cover", &legacy); url == nil || *url != "/api/v1/rom/rom-1/cover" {
		t.Fatalf("non content-addressed path = %v, want an unversioned URL", url)
	}
	if imageURL("rom-1", "cover", nil) != nil {
		t.Fatal("missing image produced a URL")
	}
}

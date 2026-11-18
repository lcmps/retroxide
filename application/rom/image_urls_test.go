package rom

import (
	"strings"
	"testing"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

func TestImageURLIsVersionedByContentAddress(t *testing.T) {
	address := strings.Repeat("ab", 32)
	path := "/storage/" + address
	url := imageURL("rom-1", "cover", &path, "")
	if url == nil || *url != "/api/v1/rom/rom-1/cover?v="+address[:imageVersionLength] {
		t.Fatalf("imageURL = %v", url)
	}
	if !ImageVersionMatches(address[:imageVersionLength], address) || ImageVersionMatches("abab", address) || ImageVersionMatches(strings.Repeat("0", imageVersionLength), address) {
		t.Fatal("ImageVersionMatches accepted a wrong or short version")
	}
	legacy := "/storage/cover.png"
	if url := imageURL("rom-1", "cover", &legacy, ""); url == nil || *url != "/api/v1/rom/rom-1/cover" {
		t.Fatalf("non content-addressed path = %v, want an unversioned URL", url)
	}
	if imageURL("rom-1", "cover", nil, "") != nil {
		t.Fatal("missing image produced a URL")
	}
}

func TestImageURLCarriesTheSignedToken(t *testing.T) {
	address := strings.Repeat("ab", 32)
	path := "/storage/" + address
	if url := imageURL("rom-1", "cover", &path, "tok.sig"); url == nil || *url != "/api/v1/rom/rom-1/cover?v="+address[:imageVersionLength]+"&token=tok.sig" {
		t.Fatalf("versioned URL with token = %v", url)
	}
	legacy := "/storage/cover.png"
	if url := imageURL("rom-1", "cover", &legacy, "tok.sig"); url == nil || *url != "/api/v1/rom/rom-1/cover?token=tok.sig" {
		t.Fatalf("unversioned URL with token = %v", url)
	}
	if url := imageURL("rom-1", "cover", &path, ""); url == nil || strings.Contains(*url, "token") {
		t.Fatalf("URL without a signer = %v, want no token", url)
	}
}

func TestSummarizeSignsEveryImageURLForTheROM(t *testing.T) {
	address := strings.Repeat("cd", 32)
	cover := "/storage/" + address
	row := romdomain.ROMWithFile{ROM: romdomain.ROM{ID: "rom-9", Identity: romdomain.ROMIdentity{Title: "T", Platform: "snes", CoverArtPath: &cover}}}
	signed := Summarize(row, func(romID string) string { return "signed-" + romID })
	if signed.CoverArtURL == nil || !strings.HasSuffix(*signed.CoverArtURL, "&token=signed-rom-9") {
		t.Fatalf("cover_art_url = %v", signed.CoverArtURL)
	}
	if signed.CoverThumbURL == nil || !strings.HasSuffix(*signed.CoverThumbURL, "&token=signed-rom-9") {
		t.Fatalf("cover_thumb_url = %v", signed.CoverThumbURL)
	}
	plain := Summarize(row, nil)
	if plain.CoverArtURL == nil || strings.Contains(*plain.CoverArtURL, "token") {
		t.Fatalf("unsigned cover_art_url = %v", plain.CoverArtURL)
	}
}

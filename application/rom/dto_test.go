package rom

import (
	"encoding/json"
	"testing"
)

func TestROMDetailSerializesMissingMetadataAsNull(t *testing.T) {
	response := ROMDetailDTO{
		ROMID:    "rom-1",
		Title:    "Example",
		Platform: "snes",
		Hash: ROMHashDTO{
			SHA256: "sha256",
			SHA1:   "sha1",
			MD5:    "md5",
			CRC32:  "crc32",
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rom_id":"rom-1","title":"Example","platform":"snes","region":null,"version":null,"publisher":null,"release_year":null,"description":null,"cover_art_url":null,"title_screen_url":null,"screenshot_url":null,"hash":{"sha256":"sha256","sha1":"sha1","md5":"md5","crc32":"crc32"}}`
	if got := string(encoded); got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

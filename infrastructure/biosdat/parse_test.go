package biosdat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// The bundled list must stay exactly as libretro publishes it (its CC-BY-SA license allows
// redistribution with attribution; NOTICE carries that).
func TestEmbeddedListIsUnmodified(t *testing.T) {
	want, err := os.ReadFile("testdata/System.dat.sha256")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(embedded)
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Fatalf("embedded System.dat sha256 = %s, want %s", got, want)
	}
}

func TestParseEmbedded(t *testing.T) {
	entries, err := Parse(bytes.NewReader(embedded))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]bool{}
	for _, entry := range entries {
		byPath[entry.Path] = true
		if _, ok := romdomain.PlatformByKey(entry.Platform); !ok {
			t.Errorf("%s has platform %q, not in the catalog", entry.Path, entry.Platform)
		}
		if len(entry.SHA1) != 40 || entry.SHA1 != strings.ToLower(entry.SHA1) {
			t.Errorf("%s has SHA-1 %q, want 40 lowercase hex digits", entry.Path, entry.SHA1)
		}
		if strings.HasSuffix(entry.Path, ".yaml") || strings.HasSuffix(entry.Path, ".zim") {
			t.Errorf("support file %s was listed as a BIOS", entry.Path)
		}
		if entry.Path == "pcsx2/bios/ps2-0160e-20010704.bin" {
			if entry.Platform != "ps2" || entry.Size != 4194304 || entry.Labels.Region == nil || *entry.Labels.Region != "Europe" {
				t.Errorf("PS2 v1.60 Europe parsed as %+v", entry)
			}
		}
	}
	for _, path := range []string{"pcsx2/bios/ps2-0160e-20010704.bin", "scph5501.bin", "bios_CD_U.bin", "dc/dc_boot.bin", "gba_bios.bin"} {
		if !byPath[path] {
			t.Errorf("%s missing from the parsed list", path)
		}
	}
}

// A section renamed upstream would silently drop its platform's BIOS files; this catches it.
func TestEverySectionMappingExists(t *testing.T) {
	for section := range sectionPlatforms {
		if !bytes.Contains(embedded, []byte(`comment "`+section+`"`)) {
			t.Errorf("section %q is mapped but not in System.dat", section)
		}
	}
}

func TestParseRejectsListsWithoutMappedEntries(t *testing.T) {
	if _, err := Parse(strings.NewReader("<html>rate limited</html>")); err == nil {
		t.Fatal("Parse accepted a list with no BIOS entries")
	}
}

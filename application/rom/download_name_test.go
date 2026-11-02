package rom

import (
	"strings"
	"testing"
	"unicode/utf8"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

func TestDownloadFilename(t *testing.T) {
	uploaded := func(name string) *string { return &name }
	tests := []struct {
		name     string
		title    string
		platform string
		uploaded *string
		want     string
	}{
		{name: "title with uploaded extension", title: "The Legend of Zelda", platform: "nes", uploaded: uploaded("zelda (U).nes"), want: "The Legend of Zelda.nes"},
		{name: "extension is lowercased", title: "Chrono Trigger", platform: "snes", uploaded: uploaded("CT.SMC"), want: "Chrono Trigger.smc"},
		{name: "only the final extension is kept", title: "Final Fantasy VII (Disc 1)", platform: "psx", uploaded: uploaded("ff7.d1.chd"), want: "Final Fantasy VII (Disc 1).chd"},
		{name: "reserved characters are replaced", title: `Metroid: Zero/Mission? <"Fusion">*`, platform: "gba", uploaded: uploaded("m.gba"), want: "Metroid Zero Mission Fusion.gba"},
		{name: "path in uploaded name is ignored", title: "Tetris", platform: "gb", uploaded: uploaded(`C:\roms\..\tetris.gb`), want: "Tetris.gb"},
		{name: "unicode title is preserved", title: "ポケットモンスター 赤", platform: "gb", uploaded: uploaded("pm.gb"), want: "ポケットモンスター 赤.gb"},
		{name: "platform extension without upload", title: "Mario Kart 64", platform: "N64", want: "Mario Kart 64.z64"},
		{name: "generic extension for unknown platform", title: "Homebrew", platform: "unknown", want: "Homebrew.rom"},
		{name: "implausible uploaded extension falls back", title: "Demo", platform: "gba", uploaded: uploaded("demo.tar gz"), want: "Demo.gba"},
		{name: "uploaded name when title is unusable", title: " ... ", platform: "nes", uploaded: uploaded("Kirby's Adventure.nes"), want: "Kirby's Adventure.nes"},
		{name: "rom id as last resort", title: "", platform: "unknown", want: "rom-1.rom"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rom := romdomain.ROM{ID: "rom-1", Identity: romdomain.ROMIdentity{Title: test.title, Platform: test.platform}}
			if got := downloadFilename(rom, test.uploaded); got != test.want {
				t.Fatalf("downloadFilename() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSanitizeFilenameCapsLengthOnARuneBoundary(t *testing.T) {
	got := sanitizeFilename(strings.Repeat("é", maxDownloadNameBytes))
	if len(got) > maxDownloadNameBytes || !utf8.ValidString(got) {
		t.Fatalf("sanitizeFilename() = %d bytes, valid UTF-8 %v", len(got), utf8.ValidString(got))
	}
}

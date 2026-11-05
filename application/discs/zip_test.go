package discs

import (
	"archive/zip"
	"testing"
)

func TestZipMethodStoresCompressedDiscFormats(t *testing.T) {
	for name, want := range map[string]uint16{
		"Game.chd": zip.Store, "Game.CHD": zip.Store, "a/Game.cso": zip.Store, "Game.rvz": zip.Store, "Game.PBP": zip.Store,
		"Game.iso": zip.Deflate, "Game (Track 1).bin": zip.Deflate, "Game.cue": zip.Deflate, "Game.m3u": zip.Deflate, "chd": zip.Deflate,
	} {
		if got := zipMethod(name); got != want {
			t.Errorf("zipMethod(%q) = %d, want %d", name, got, want)
		}
	}
}

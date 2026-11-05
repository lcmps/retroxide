package discs

import (
	"archive/zip"
	"bytes"
	"io"
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

func TestZipWriterRefusesUnsafeEntryNames(t *testing.T) {
	write := func(w io.Writer) error { _, err := w.Write([]byte("x")); return err }
	for _, name := range []string{"..", ".", "a/../b", "/a", `a\b`, "a//b", "a/", "", "../a", "a/."} {
		var out bytes.Buffer
		archive := newZipWriter(&out)
		if err := archive.add(name, 1, write); err == nil {
			t.Errorf("add(%q) succeeded, want an error", name)
		}
		if err := archive.close(); err != nil {
			t.Fatal(err)
		}
		if reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil || len(reader.File) != 0 {
			t.Errorf("add(%q) wrote an entry: %v, %v", name, reader, err)
		}
	}
	for _, name := range []string{"a..b.bin", "A...B (Disc 1)/x.bin", "...", ".hidden", "a/b/c.bin"} {
		var out bytes.Buffer
		archive := newZipWriter(&out)
		if err := archive.add(name, 1, write); err != nil {
			t.Errorf("add(%q) = %v", name, err)
		}
		_ = archive.close()
	}
}

package discimage

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	romhash "github.com/lcmps/retroxide/infrastructure/hash"
)

const fixtures = "../chd/testdata"

func expectedSums(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sums := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		sum, name, _ := strings.Cut(scanner.Text(), "  ")
		sums[name] = sum
	}
	return sums
}

func TestInspectHashesEveryTrackAsItsFile(t *testing.T) {
	images := NewImages(romhash.New())
	content, err := os.ReadFile(filepath.Join(fixtures, "chd", "cd-cdlz.chd"))
	if err != nil {
		t.Fatal(err)
	}
	tracks, ok, err := images.Inspect(context.Background(), bytes.NewReader(content), int64(len(content)), "Game")
	if err != nil || !ok {
		t.Fatalf("Inspect() = %v, %v", ok, err)
	}
	sums := expectedSums(t, filepath.Join(fixtures, "expected", "cd", "SHA1SUMS"))
	modes := []string{"MODE1/2352", "AUDIO", "MODE2/2352", "AUDIO"}
	for index, track := range tracks {
		if track.Hash.SHA1 != sums[track.Filename] || track.Mode != modes[index] || track.Hash.SHA256 == "" || track.Hash.CRC32 == "" {
			t.Errorf("track %d = %+v; want SHA-1 %s and mode %s", track.Number, track, sums[track.Filename], modes[index])
		}
	}
	if !tracks[1].PregapStored || tracks[1].Pregap != 30 {
		t.Errorf("track 2 pregap = %d stored=%v, want 30 stored", tracks[1].Pregap, tracks[1].PregapStored)
	}

	// Anything else is simply not a disc.
	raw, _ := os.ReadFile(filepath.Join(fixtures, "chd", "raw-zlib.chd"))
	for name, data := range map[string][]byte{"plain file": []byte("not a chd at all"), "hard disk chd": raw} {
		if _, ok, err := images.Inspect(context.Background(), bytes.NewReader(data), int64(len(data)), "x"); ok || err != nil {
			t.Errorf("%s: ok=%v err=%v, want not a disc", name, ok, err)
		}
	}
}

func TestExtractRebuildsTheSheetAndTracks(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(fixtures, "chd", "gd.chd"))
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	add := func(name string, size int64, write func(io.Writer) error) error {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		return write(entry)
	}
	if err := NewImages(romhash.New()).Extract(context.Background(), bytes.NewReader(content), int64(len(content)), "Disc", add); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	sums := expectedSums(t, filepath.Join(fixtures, "expected", "gd", "SHA1SUMS"))
	wantSheet, _ := os.ReadFile(filepath.Join(fixtures, "expected", "gd", "Disc.gdi"))
	for _, file := range reader.File {
		entry, _ := file.Open()
		data, _ := io.ReadAll(entry)
		if file.Name == "Disc.gdi" {
			if string(data) != string(wantSheet) {
				t.Errorf("sheet = %q, want %q", data, wantSheet)
			}
			continue
		}
		sum := sha1.Sum(data)
		if hex.EncodeToString(sum[:]) != sums[file.Name] {
			t.Errorf("%s does not match chdman's", file.Name)
		}
	}
	if len(reader.File) != 4 {
		t.Fatalf("archive holds %d files, want the sheet and 3 tracks", len(reader.File))
	}
}

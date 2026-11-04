package chd

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixtures in testdata are made by testdata/generate.sh with MAME's chdman, so these tests
// check the reader against the reference implementation's own files and output.

func openFixture(t *testing.T, name string) *Reader {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "chd", name))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	return reader
}

func readAll(t *testing.T, reader *Reader) []byte {
	t.Helper()
	header := reader.Header()
	var data bytes.Buffer
	hunk := make([]byte, header.HunkBytes)
	for index := uint32(0); index < header.HunkCount; index++ {
		if err := reader.ReadHunk(index, hunk); err != nil {
			t.Fatalf("hunk %d: %v", index, err)
		}
		data.Write(hunk)
	}
	return data.Bytes()[:header.LogicalBytes]
}

func TestReaderDecodesEveryCodec(t *testing.T) {
	want, err := os.ReadFile("testdata/expected/disk.raw")
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []string{"zlib", "lzma", "huff", "flac", "zstd", "none"} {
		reader := openFixture(t, "raw-"+codec+".chd")
		got := readAll(t, reader)
		if !bytes.Equal(got, want) {
			t.Errorf("%s: decoded data differs", codec)
		}
		// chdman leaves the hashes of uncompressed images zero.
		if raw := reader.Header().RawSHA1; raw != ([20]byte{}) && sha1.Sum(got) != raw {
			t.Errorf("%s: data does not match the CHD's own SHA-1", codec)
		}
		if reader.IsCD() {
			t.Errorf("%s: a raw image is reported as a CD", codec)
		}
	}
}

// expectedTracks reads a SHA1SUMS file.
func expectedTracks(t *testing.T, path string) map[string]string {
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

func TestReaderExtractsTracksAsChdmanDoes(t *testing.T) {
	for _, test := range []struct {
		fixture, expected, base string
		gdrom                   bool
	}{
		{"cd-cdlz.chd", "cd", "Game", false},
		{"cd-cdzl.chd", "cd", "Game", false},
		{"cd-cdfl.chd", "cd", "Game", false},
		{"cd-cdzs.chd", "cd", "Game", false},
		{"gd.chd", "gd", "Disc", true},
	} {
		reader := openFixture(t, test.fixture)
		if !reader.IsCD() {
			t.Fatalf("%s: not reported as a CD", test.fixture)
		}
		disc, err := reader.Disc()
		if err != nil {
			t.Fatalf("%s: %v", test.fixture, err)
		}
		if disc.GDROM != test.gdrom {
			t.Errorf("%s: GD-ROM = %v", test.fixture, disc.GDROM)
		}
		wantSheet, err := os.ReadFile(filepath.Join("testdata", "expected", test.expected, disc.SheetName(test.base)))
		if err != nil {
			t.Fatal(err)
		}
		if got := disc.Sheet(test.base); got != string(wantSheet) {
			t.Errorf("%s sheet:\n%s\nwant:\n%s", test.fixture, got, wantSheet)
		}
		sums := expectedTracks(t, filepath.Join("testdata", "expected", test.expected, "SHA1SUMS"))
		if len(sums) != len(disc.Tracks) {
			t.Fatalf("%s: %d tracks, want %d", test.fixture, len(disc.Tracks), len(sums))
		}
		for _, track := range disc.Tracks {
			hash := sha1.New()
			written, err := reader.ExtractTrack(track, hash)
			if err != nil {
				t.Fatalf("%s track %d: %v", test.fixture, track.Number, err)
			}
			name := disc.TrackFileName(test.base, track)
			if got := hex.EncodeToString(hash.Sum(nil)); got != sums[name] {
				t.Errorf("%s: %s hashes %s, want %s", test.fixture, name, got, sums[name])
			}
			if written != track.FileBytes() {
				t.Errorf("%s: %s is %d bytes, FileBytes says %d", test.fixture, name, written, track.FileBytes())
			}
		}
	}
}

func TestReaderRegeneratesStrippedECC(t *testing.T) {
	// chdman drops sync and ECC from sectors whose ECC it can regenerate and flags them in each
	// hunk's bitmap; the MODE1 fixture track has valid ECC, so the first hunk must be flagged.
	reader := openFixture(t, "cd-cdlz.chd")
	entry := reader.entries[0]
	compressed := make([]byte, entry.length)
	if _, err := reader.source.ReadAt(compressed, int64(entry.offset)); err != nil {
		t.Fatal(err)
	}
	if compressed[0] == 0 {
		t.Fatal("no sector had its ECC stripped, so regeneration is untested")
	}
	sector := make([]byte, sectorBytes)
	copy(sector, syncHeader[:])
	sector[modeOffset] = 1
	for position := 16; position < 2064; position++ {
		sector[position] = byte(position)
	}
	generateECC(sector)
	want := append([]byte(nil), sector...)
	clear(sector[eccPOffset:])
	generateECC(sector)
	if !bytes.Equal(sector, want) {
		t.Fatal("ECC generation is not deterministic")
	}
}

func TestReaderRejectsWhatItCannotRead(t *testing.T) {
	content, err := os.ReadFile("testdata/chd/cd-cdlz.chd")
	if err != nil {
		t.Fatal(err)
	}
	open := func(mutate func([]byte)) error {
		changed := append([]byte(nil), content...)
		mutate(changed)
		reader, err := Open(bytes.NewReader(changed), int64(len(changed)))
		if err != nil {
			return err
		}
		hunk := make([]byte, reader.Header().HunkBytes)
		for index := uint32(0); index < reader.Header().HunkCount; index++ {
			if err := reader.ReadHunk(index, hunk); err != nil {
				return err
			}
		}
		return nil
	}
	if err := open(func(data []byte) { copy(data, "NotACHD!") }); !errors.Is(err, ErrNotCHD) {
		t.Errorf("wrong signature: %v", err)
	}
	if err := open(func(data []byte) { data[15] = 4 }); !errors.Is(err, ErrUnsupported) {
		t.Errorf("version 4: %v", err)
	}
	if err := open(func(data []byte) { data[110] = 1 }); !errors.Is(err, ErrUnsupported) {
		t.Errorf("parent CHD: %v", err)
	}
	// A flipped byte in the first hunk's data fails its CRC (or its decompression).
	first := openFixture(t, "cd-cdlz.chd").entries[0]
	if err := open(func(data []byte) { data[first.offset+first.offset%7+20] ^= 0xff }); !errors.Is(err, ErrCorrupt) {
		t.Errorf("corrupted hunk: %v", err)
	}
	if _, err := Open(bytes.NewReader([]byte("short")), 5); !errors.Is(err, ErrNotCHD) {
		t.Errorf("short file: %v", err)
	}
}

// failingWriter accepts limit bytes, then fails.
type failingWriter struct{ limit int }

func (writer *failingWriter) Write(data []byte) (int, error) {
	if len(data) > writer.limit {
		return writer.limit, errors.New("disk full")
	}
	writer.limit -= len(data)
	return len(data), nil
}

func TestExtractionStopsCleanlyAndRunsConcurrently(t *testing.T) {
	reader := openFixture(t, "cd-cdlz.chd")
	disc, err := reader.Disc()
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	for range 20 {
		if _, err := reader.ExtractTrack(disc.Tracks[0], &failingWriter{limit: 1000}); err == nil {
			t.Fatal("extraction into a failing writer succeeded")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines grew from %d to %d after aborted extractions", before, after)
	}

	// The same reader serves several extractions at once.
	sums := expectedTracks(t, "testdata/expected/cd/SHA1SUMS")
	var wg sync.WaitGroup
	for _, track := range disc.Tracks {
		wg.Go(func() {
			hash := sha1.New()
			if _, err := reader.ExtractTrack(track, hash); err != nil {
				t.Error(err)
				return
			}
			if got := hex.EncodeToString(hash.Sum(nil)); got != sums[disc.TrackFileName("Game", track)] {
				t.Errorf("track %d hashes %s concurrently", track.Number, got)
			}
		})
	}
	wg.Wait()
}

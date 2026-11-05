package discimage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	chdman "github.com/lcmps/chdman-go"

	"github.com/lcmps/retroxide/infrastructure/chd"
)

// discOnDisk rebuilds a fixture CD as the sheet and track files a person would upload.
func discOnDisk(t *testing.T) (sheetPath string, fixture chd.Header) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(fixtures, "chd", "cd-cdlz.chd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := chd.Open(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	disc, err := reader.Disc()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for _, track := range disc.Tracks {
		var data bytes.Buffer
		if _, err := reader.ExtractTrack(track, &data); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, disc.TrackFileName("Game", track)), data.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sheetPath = filepath.Join(directory, "Game.cue")
	if err := os.WriteFile(sheetPath, []byte(disc.Sheet("Game")), 0o600); err != nil {
		t.Fatal(err)
	}
	return sheetPath, reader.Header()
}

func TestCompressMatchesChdman(t *testing.T) {
	sheet, fixture := discOnDisk(t)
	output := filepath.Join(t.TempDir(), "disc.chd")
	if err := NewCompressor().Compress(context.Background(), sheet, output, nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := chd.Open(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("the built-in reader cannot open the CHD: %v", err)
	}
	if got := reader.Header(); got.RawSHA1 != fixture.RawSHA1 || got.SHA1 != fixture.SHA1 {
		t.Fatalf("hashes = %x %x, want chdman's %x %x", got.RawSHA1, got.SHA1, fixture.RawSHA1, fixture.SHA1)
	}
}

func TestCompressCancelledLeavesNoOutput(t *testing.T) {
	sheet, _ := discOnDisk(t)
	output := filepath.Join(t.TempDir(), "disc.chd")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewCompressor().Compress(ctx, sheet, output, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compress() = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output exists after cancelling: %v", err)
	}
}

// A panic in the library used to be a crash in another process; it must not take the server down.
func TestCompressPanicBecomesError(t *testing.T) {
	compressor := Compressor{createCD: func(context.Context, string, string, chdman.CreateOptions) (chdman.CreateResult, error) {
		panic("boom")
	}}
	err := compressor.Compress(context.Background(), "disc.cue", "disc.chd", nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Compress() = %v, want an error naming the panic", err)
	}
}

func TestCompressFailureIsReturned(t *testing.T) {
	compressor := Compressor{createCD: func(context.Context, string, string, chdman.CreateOptions) (chdman.CreateResult, error) {
		return chdman.CreateResult{}, chdman.ErrInvalidCue
	}}
	if err := compressor.Compress(context.Background(), "disc.cue", "disc.chd", nil); !errors.Is(err, chdman.ErrInvalidCue) {
		t.Fatalf("Compress() = %v, want the library's error", err)
	}
}

// Warnings report input the disc loses; the read-back check decides whether the CHD is kept.
func TestCompressWarningsDoNotFail(t *testing.T) {
	compressor := Compressor{createCD: func(context.Context, string, string, chdman.CreateOptions) (chdman.CreateResult, error) {
		return chdman.CreateResult{Warnings: []chdman.Warning{{Kind: chdman.WarnFileUnused, Message: "unused"}}}, nil
	}}
	if err := compressor.Compress(context.Background(), "disc.cue", "disc.chd", nil); err != nil {
		t.Fatalf("Compress() = %v", err)
	}
}

func TestCompressionLeavesACoreFree(t *testing.T) {
	var workers int
	compressor := Compressor{createCD: func(_ context.Context, _, _ string, options chdman.CreateOptions) (chdman.CreateResult, error) {
		workers = options.Workers
		return chdman.CreateResult{}, nil
	}}
	if err := compressor.Compress(context.Background(), "disc.cue", "disc.chd", nil); err != nil {
		t.Fatal(err)
	}
	if want := compressionWorkers(runtime.GOMAXPROCS(0)); workers != want {
		t.Fatalf("workers = %d, want %d", workers, want)
	}
}

func TestCompressionWorkers(t *testing.T) {
	for procs, want := range map[int]int{8: 7, 2: 1, 1: 1, 0: 1} {
		if got := compressionWorkers(procs); got != want {
			t.Errorf("compressionWorkers(%d) = %d, want %d", procs, got, want)
		}
	}
}

func TestCompressReportsProgressUpToTheWholeDisc(t *testing.T) {
	sheet, _ := discOnDisk(t)
	var last, total uint64
	report := func(done, of uint64) { last, total = done, of }
	if err := NewCompressor().Compress(context.Background(), sheet, filepath.Join(t.TempDir(), "disc.chd"), report); err != nil {
		t.Fatal(err)
	}
	if total == 0 || last != total {
		t.Fatalf("last report = %d of %d, want the whole disc", last, total)
	}
}

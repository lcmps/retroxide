package discs

import (
	"context"
	"testing"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// countingStorage serves one sheet and counts how often it is read.
type countingStorage struct {
	Storage
	sheet string
	reads int
}

func (storage *countingStorage) Read(context.Context, string) ([]byte, error) {
	storage.reads++
	return []byte(storage.sheet), nil
}

type noopCompressor struct{}

func (noopCompressor) Compress(context.Context, string, string, func(done, total uint64)) error {
	return nil
}

func TestCanCompressFileReadsTheSheetOnce(t *testing.T) {
	storage := &countingStorage{sheet: "FILE \"Game.bin\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n"}
	service := NewService(nil, storage, nil, nil, noopCompressor{})
	sheetPath := "ab/sheet"
	tracks := romdomain.ROMFile{ID: "rf_1", Format: romdomain.FormatTracks, StoragePath: sheetPath, SheetPath: &sheetPath}

	for range 3 {
		if !service.CanCompressFile(context.Background(), tracks) {
			t.Fatal("a plain CD kept as tracks should be compressible")
		}
	}
	if storage.reads != 1 {
		t.Fatalf("sheet reads = %d, want 1: the answer never changes for a stored file", storage.reads)
	}
	// A file compressed since answers from its format, without touching the cache or the disk.
	compressed := tracks
	compressed.Format = romdomain.FormatCHD
	if service.CanCompressFile(context.Background(), compressed) || storage.reads != 1 {
		t.Fatalf("compressed file = compressible with %d reads", storage.reads)
	}
}

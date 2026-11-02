package storage

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestStorageWriteReadAndDelete(t *testing.T) {
	root := t.TempDir()
	filesystem := New(root)
	address := strings.Repeat("a", 64)
	path, err := filesystem.Write(context.Background(), address, []byte("rom bytes"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := filesystem.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "rom bytes" {
		t.Fatalf("Read() = %q, want rom bytes", content)
	}
	if err := filesystem.Delete(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat() error = %v, want not-exist", err)
	}
}

func TestStorageRejectsInvalidContentAddress(t *testing.T) {
	filesystem := New(t.TempDir())
	if _, err := filesystem.Write(context.Background(), "../outside", []byte("x")); err == nil {
		t.Fatal("Write() accepted a non-SHA256 content address")
	}
}

func TestStorageConcurrentWritesRemainComplete(t *testing.T) {
	filesystem := New(t.TempDir())
	address := strings.Repeat("b", 64)
	content := []byte(strings.Repeat("payload", 1024))
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := filesystem.Write(context.Background(), address, content)
			if err != nil {
				errors <- err
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	stored, err := filesystem.Read(context.Background(), filesystem.pathFor(address))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(content) {
		t.Fatalf("stored content length = %d, want %d", len(stored), len(content))
	}
}

func TestStorageStageCommitsAtomicallyAndDiscardLeavesNothing(t *testing.T) {
	root := t.TempDir()
	filesystem := New(root)
	ctx := context.Background()

	staged, err := filesystem.Stage(ctx, strings.NewReader("streamed rom"))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Size() != int64(len("streamed rom")) {
		t.Fatalf("staged size = %d", staged.Size())
	}
	path, err := staged.Commit(ctx, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	staged.Discard() // no-op after commit
	if content, err := os.ReadFile(path); err != nil || string(content) != "streamed rom" {
		t.Fatalf("committed content = %q, %v", content, err)
	}

	abandoned, err := filesystem.Stage(ctx, strings.NewReader("duplicate"))
	if err != nil {
		t.Fatal(err)
	}
	abandoned.Discard()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != strings.Repeat("b", 64) {
		t.Fatalf("storage root entries = %v, want only the committed file", entries)
	}
}

func TestStorageStageStopsWhenContextIsCancelled(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(root).Stage(ctx, strings.NewReader("never written")); err == nil {
		t.Fatal("Stage succeeded with a cancelled context")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("cancelled stage left files behind: %v", entries)
	}
}

func TestStorageOpenReturnsSeekableContentWithMetadata(t *testing.T) {
	filesystem := New(t.TempDir())
	path, err := filesystem.Write(context.Background(), strings.Repeat("c", 64), []byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := filesystem.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	if content.Size != 6 || content.ModifiedAt.IsZero() {
		t.Fatalf("metadata size=%d modified=%v", content.Size, content.ModifiedAt)
	}
	if _, err := content.Seek(4, 0); err != nil {
		t.Fatal(err)
	}
	rest := make([]byte, 2)
	if _, err := content.Read(rest); err != nil || string(rest) != "ef" {
		t.Fatalf("read after seek = %q, %v", rest, err)
	}
}

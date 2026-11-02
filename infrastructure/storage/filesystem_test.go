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

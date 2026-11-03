package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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
	if path != "aa/"+address {
		t.Fatalf("Write() key = %q, want aa/%s", path, address)
	}
	if err := filesystem.Delete(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "aa", address)); !os.IsNotExist(err) {
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
	stored, err := filesystem.Read(context.Background(), keyFor(address))
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

	staged, err := filesystem.Stage(ctx, strings.NewReader("streamed rom"), 0)
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
	if content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err != nil || string(content) != "streamed rom" {
		t.Fatalf("committed content = %q, %v", content, err)
	}

	abandoned, err := filesystem.Stage(ctx, strings.NewReader("duplicate"), 0)
	if err != nil {
		t.Fatal(err)
	}
	abandoned.Discard()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "bb" {
		t.Fatalf("storage root entries = %v, want only the bb folder", entries)
	}
}

func TestStorageStageStopsWhenContextIsCancelled(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(root).Stage(ctx, strings.NewReader("never written"), 0); err == nil {
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

func TestStorageResolvesLegacyAbsolutePathsInsideTheRootOnly(t *testing.T) {
	root := t.TempDir()
	address := strings.Repeat("d", 64)
	legacy := filepath.Join(root, address)
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	filesystem := New(root)
	if content, err := filesystem.Read(context.Background(), legacy); err != nil || string(content) != "legacy" {
		t.Fatalf("Read(legacy) = %q, %v", content, err)
	}
	for _, path := range []string{filepath.Join(filepath.Dir(root), address), "../" + address, "dd/../" + address, "ab/" + address} {
		if _, err := filesystem.Read(context.Background(), path); err == nil {
			t.Errorf("Read(%q) succeeded, want rejection", path)
		}
	}
}

func TestStorageRelocatesLegacyFilesIdempotently(t *testing.T) {
	root := t.TempDir()
	filesystem := New(root)
	flat, moved := strings.Repeat("e", 64), strings.Repeat("f", 64)
	for _, address := range []string{flat, moved} {
		if err := os.WriteFile(filepath.Join(root, address), []byte(address[:1]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	relocate := func(path string) (string, error) {
		keys, errs := filesystem.Relocate([]string{path})
		return keys[path], errors.Join(errs...)
	}
	// A file from the flat layout, recorded at its absolute path.
	key, err := relocate(filepath.Join(root, flat))
	if err != nil || key != "ee/"+flat {
		t.Fatalf("Relocate(flat) = %q, %v", key, err)
	}
	if content, err := filesystem.Read(context.Background(), key); err != nil || string(content) != "e" {
		t.Fatalf("relocated content = %q, %v", content, err)
	}
	// Running again (say after a crash before the key was saved) finds it in place.
	if again, err := relocate(filepath.Join(root, flat)); err != nil || again != key {
		t.Fatalf("second Relocate = %q, %v", again, err)
	}
	// A path recorded before the storage folder moved is found by its name.
	if key, err := relocate("/old/storage/" + moved); err != nil || key != "ff/"+moved {
		t.Fatalf("Relocate(moved root) = %q, %v", key, err)
	}
	if key, err := relocate("ee/" + flat); err != nil || key != "ee/"+flat {
		t.Fatalf("Relocate(key) = %q, %v", key, err)
	}
	if _, err := relocate(filepath.Join(root, strings.Repeat("0", 64))); !errors.Is(err, ErrMissingContent) {
		t.Fatalf("Relocate(missing) error = %v, want ErrMissingContent", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Fatalf("root entries = %v, want only the ee and ff folders", entries)
	}
}

func TestStorageSweepsOnlyStaleStagedFiles(t *testing.T) {
	root := t.TempDir()
	filesystem := New(root)
	stale, fresh := filepath.Join(root, ".staged.1.tmp"), filepath.Join(root, ".staged.2.tmp")
	for _, path := range []string{stale, fresh, filepath.Join(root, "keep.tmp")} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if removed, err := filesystem.SweepStaged(time.Hour, time.Hour); err != nil || removed != 1 {
		t.Fatalf("SweepStaged() = %d, %v; want 1", removed, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale staged file kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("staged file still being written was removed")
	}
	if removed, err := New(filepath.Join(root, "absent")).SweepStaged(time.Hour, time.Hour); err != nil || removed != 0 {
		t.Fatalf("SweepStaged(absent root) = %d, %v", removed, err)
	}
}

func TestStorageReportsWhetherContentFits(t *testing.T) {
	filesystem := New(t.TempDir())
	if !filesystem.HasRoomFor(1) {
		t.Fatal("one byte does not fit")
	}
	if filesystem.HasRoomFor(1 << 62) {
		t.Fatal("an exabyte fits")
	}
}

func TestStorageStagesLargeContentInBlocks(t *testing.T) {
	filesystem := New(t.TempDir())
	content := strings.Repeat("0123456789abcdef", 3*writeBufferSize/16+5)
	staged, err := filesystem.Stage(context.Background(), strings.NewReader(content), 0)
	if err != nil {
		t.Fatal(err)
	}
	key, err := staged.Commit(context.Background(), strings.Repeat("9", 64))
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := filesystem.Read(context.Background(), key); err != nil || string(stored) != content {
		t.Fatalf("stored %d bytes, %v; want %d", len(stored), err, len(content))
	}
}

// brokenReader returns its content, then err, as a request body does when the client hangs up.
type brokenReader struct {
	content string
	err     error
}

func (reader *brokenReader) Read(buffer []byte) (int, error) {
	if reader.content == "" {
		return 0, reader.err
	}
	read := copy(buffer, reader.content)
	reader.content = reader.content[read:]
	return read, nil
}

func TestStorageStageFailsWhenTheContentIsCutOff(t *testing.T) {
	root := t.TempDir()
	for _, cause := range []error{io.ErrUnexpectedEOF, errors.New("connection reset")} {
		if _, err := New(root).Stage(context.Background(), &brokenReader{content: "partial rom", err: cause}, 0); !errors.Is(err, cause) {
			t.Fatalf("Stage() error = %v, want %v", err, cause)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("cut-off uploads left files behind: %v", entries)
	}
}

func TestStorageReservesTheHintedSizeAndTrimsToTheContent(t *testing.T) {
	filesystem := New(t.TempDir())
	content := strings.Repeat("rom", 1000)
	// The hint overestimates, as a request length counting multipart framing does.
	staged, err := filesystem.Stage(context.Background(), strings.NewReader(content), int64(len(content))+4096)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Size() != int64(len(content)) {
		t.Fatalf("staged size = %d, want %d", staged.Size(), len(content))
	}
	key, err := staged.Commit(context.Background(), strings.Repeat("7", 64))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := filesystem.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	if stored.Size != int64(len(content)) {
		t.Fatalf("stored file is %d bytes, want %d: the reservation was not trimmed", stored.Size, len(content))
	}
	// On Linux an impossible reservation fails at once, before anything is written.
	if runtime.GOOS != "linux" {
		return
	}
	if _, err := filesystem.Stage(context.Background(), strings.NewReader("x"), 1<<60); err == nil {
		t.Fatal("reserving an exabyte succeeded")
	}
}

func TestStorageOpensLargeFilesSoTheyCanStillBeSentWithSendfile(t *testing.T) {
	filesystem := New(t.TempDir())
	key, err := filesystem.Write(context.Background(), strings.Repeat("6", 64), make([]byte, uncachedSize))
	if err != nil {
		t.Fatal(err)
	}
	content, err := filesystem.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	// Large files close through uncachedFile, which must keep the raw descriptor reachable.
	if _, ok := content.ReadSeekCloser.(uncachedFile); !ok {
		t.Fatalf("large file opened as %T, want uncachedFile", content.ReadSeekCloser)
	}
	if _, ok := content.ReadSeekCloser.(interface {
		SyscallConn() (syscall.RawConn, error)
	}); !ok {
		t.Fatal("large file hides SyscallConn, so sendfile cannot use it")
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoragePartialUploadsResumeAfterAFailedPart(t *testing.T) {
	ctx := context.Background()
	filesystem := New(t.TempDir())
	if err := filesystem.ReservePartial(ctx, "ups_1", 12); err != nil {
		t.Fatal(err)
	}
	if err := filesystem.ReservePartial(ctx, "ups_1", 12); err == nil {
		t.Fatal("a second reservation for the same session succeeded")
	}
	if written, err := filesystem.AppendPartial(ctx, "ups_1", 0, strings.NewReader("hello ")); err != nil || written != 6 {
		t.Fatalf("first part = %d, %v", written, err)
	}
	// A part cut off midway leaves bytes past the recorded size; the retry replaces them.
	if _, err := filesystem.AppendPartial(ctx, "ups_1", 6, &brokenReader{content: "wo", err: io.ErrUnexpectedEOF}); err == nil {
		t.Fatal("a cut-off part succeeded")
	}
	if written, err := filesystem.AppendPartial(ctx, "ups_1", 6, strings.NewReader("world!")); err != nil || written != 6 {
		t.Fatalf("retried part = %d, %v", written, err)
	}
	key, err := filesystem.CommitPartial(ctx, "ups_1", strings.Repeat("4", 64))
	if err != nil {
		t.Fatal(err)
	}
	if content, err := filesystem.Read(ctx, key); err != nil || string(content) != "hello world!" {
		t.Fatalf("committed content = %q, %v", content, err)
	}
	if err := filesystem.DiscardPartial("ups_1"); err != nil {
		t.Fatalf("discarding a committed session: %v", err)
	}
	for _, id := range []string{"", "../escape", "a/b", strings.Repeat("x", 65)} {
		if err := filesystem.ReservePartial(ctx, id, 1); err == nil {
			t.Errorf("ReservePartial(%q) accepted an unsafe id", id)
		}
	}
}

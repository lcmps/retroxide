package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
)

type Filesystem struct {
	root string
}

func New(root string) *Filesystem {
	return &Filesystem{root: filepath.Clean(root)}
}

// Write stores content at its content address.
func (filesystem *Filesystem) Write(ctx context.Context, contentAddress string, content []byte) (string, error) {
	staged, err := filesystem.Stage(ctx, bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	defer staged.Discard()
	return staged.Commit(ctx, contentAddress)
}

// Stage streams content into a temporary file inside the storage root, so committing it is an
// atomic rename and readers never observe partially written content.
func (filesystem *Filesystem) Stage(ctx context.Context, content io.Reader) (uploads.StagedContent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filesystem.root, 0o755); err != nil {
		return nil, fmt.Errorf("creating storage root: %w", err)
	}
	file, err := os.CreateTemp(filesystem.root, ".staged.*.tmp")
	if err != nil {
		return nil, fmt.Errorf("creating staged rom file: %w", err)
	}
	staged := &stagedFile{filesystem: filesystem, path: file.Name()}
	size, copyErr := io.Copy(file, contextReader{ctx: ctx, reader: content})
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		staged.Discard()
		return nil, fmt.Errorf("writing staged rom file: %w", err)
	}
	staged.size = size
	return staged, nil
}

type stagedFile struct {
	filesystem *Filesystem
	path       string
	size       int64
}

func (staged *stagedFile) Size() int64 {
	return staged.size
}

func (staged *stagedFile) Commit(ctx context.Context, contentAddress string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isContentAddress(contentAddress) {
		return "", errors.New("storage: invalid content address")
	}
	if staged.path == "" {
		return "", errors.New("storage: staged content was already committed or discarded")
	}
	storagePath := staged.filesystem.pathFor(contentAddress)
	if err := os.Rename(staged.path, storagePath); err != nil {
		return "", fmt.Errorf("committing rom file: %w", err)
	}
	staged.path = ""
	return storagePath, nil
}

func (staged *stagedFile) Discard() {
	if staged.path == "" {
		return
	}
	_ = os.Remove(staged.path)
	staged.path = ""
}

// contextReader stops a copy once ctx is cancelled, e.g. when an uploading client disconnects.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func (filesystem *Filesystem) Read(ctx context.Context, storagePath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filesystem.resolve(storagePath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading rom content: %w", err)
	}
	return content, nil
}

// Open returns a seekable handle on stored content, suitable for streaming with range support.
func (filesystem *Filesystem) Open(ctx context.Context, storagePath string) (rom.StoredContent, error) {
	if err := ctx.Err(); err != nil {
		return rom.StoredContent{}, err
	}
	path, err := filesystem.resolve(storagePath)
	if err != nil {
		return rom.StoredContent{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return rom.StoredContent{}, fmt.Errorf("opening rom content: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return rom.StoredContent{}, fmt.Errorf("reading rom metadata: %w", err)
	}
	return rom.StoredContent{ReadSeekCloser: file, Size: info.Size(), ModifiedAt: info.ModTime()}, nil
}

func (filesystem *Filesystem) Delete(ctx context.Context, storagePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := filesystem.resolve(storagePath)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting rom content: %w", err)
	}
	return nil
}

func (filesystem *Filesystem) pathFor(contentAddress string) string {
	return filepath.Join(filesystem.root, contentAddress)
}

func (filesystem *Filesystem) resolve(storagePath string) (string, error) {
	root, err := filepath.Abs(filesystem.root)
	if err != nil {
		return "", fmt.Errorf("resolving storage root: %w", err)
	}
	path, err := filepath.Abs(storagePath)
	if err != nil {
		return "", fmt.Errorf("resolving storage path: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("storage: path is outside storage root")
	}
	return path, nil
}

func isContentAddress(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

var (
	_ rom.Storage = (*Filesystem)(nil)
	_ interface {
		Stage(context.Context, io.Reader) (uploads.StagedContent, error)
	} = (*Filesystem)(nil)
)

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Filesystem struct {
	root string
}

func New(root string) *Filesystem {
	return &Filesystem{root: filepath.Clean(root)}
}

func (filesystem *Filesystem) Write(ctx context.Context, contentAddress string, content []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isContentAddress(contentAddress) {
		return "", errors.New("storage: invalid content address")
	}
	if err := os.MkdirAll(filesystem.root, 0o755); err != nil {
		return "", fmt.Errorf("creating storage root: %w", err)
	}
	temporaryFile, err := os.CreateTemp(filesystem.root, "."+contentAddress+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("creating temporary rom file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	if _, err := temporaryFile.Write(content); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("writing temporary rom file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("closing temporary rom file: %w", err)
	}
	storagePath := filesystem.pathFor(contentAddress)
	if err := os.Rename(temporaryPath, storagePath); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("committing rom file: %w", err)
	}
	return storagePath, nil
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

func (filesystem *Filesystem) Open(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filesystem.resolve(storagePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening rom content: %w", err)
	}
	return file, nil
}

func (filesystem *Filesystem) LastModified(ctx context.Context, storagePath string) (*uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filesystem.resolve(storagePath)
	if err != nil {
		return nil, err
	}
	metadata, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading rom metadata: %w", err)
	}
	modified := metadata.ModTime().Unix()
	if modified < 0 {
		zero := uint64(0)
		return &zero, nil
	}
	modifiedSeconds := uint64(modified)
	return &modifiedSeconds, nil
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

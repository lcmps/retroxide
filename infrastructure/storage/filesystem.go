// Package storage keeps ROM files and images on the local filesystem, each at its SHA-256
// content address.
//
// Content is stored under a key of the form "ab/abcdef…" (the first two hex digits, then the
// full 64-digit address): spreading files over 256 folders keeps directory lookups fast on
// filesystems that search large folders linearly, such as exFAT and FAT32 USB drives. The
// database stores keys rather than absolute paths, so the storage folder can be moved. Paths
// recorded by earlier versions (absolute, in one flat folder) still resolve, and Relocate
// converts them.
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
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
)

const (
	stagedPrefix  = ".staged."
	stagedSuffix  = ".tmp"
	partialPrefix = ".upload."
	partialSuffix = ".part"
	workPrefix    = ".work-"
	// writeBufferSize is how much content is gathered before each write to disk. Uploads arrive
	// in pieces of a few KiB; writing them one by one costs a system call each.
	writeBufferSize = 1 << 20
	// freeSpaceReserve is left free when accepting uploads, so the database and logs never run
	// out of room because of a ROM.
	freeSpaceReserve = 256 << 20
	// uncachedSize is the size from which files are dropped from the page cache once written
	// or served: ROMs that large are read rarely, and caching them evicts what is read often.
	uncachedSize = 32 << 20
)

// ErrMissingContent reports a legacy path whose file is not in the storage folder.
var ErrMissingContent = errors.New("storage: content is missing")

type Filesystem struct {
	root string
}

func New(root string) *Filesystem {
	return &Filesystem{root: filepath.Clean(root)}
}

func (filesystem *Filesystem) Write(ctx context.Context, contentAddress string, content []byte) (string, error) {
	staged, err := filesystem.Stage(ctx, bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", err
	}
	defer staged.Discard()
	return staged.Commit(ctx, contentAddress)
}

// Stage writes content to a temporary file in the storage folder, so committing it is a rename
// on the same filesystem. The file is flushed to disk before Stage returns: a power cut can then
// never leave a truncated file under a content address the database trusts.
//
// sizeHint, when positive, is the expected size: that much space is reserved before writing,
// so the file is not fragmented and a full disk is reported at once. The file is trimmed to its
// real size afterwards, so a hint that overestimates (such as a request's length, which counts
// multipart framing) costs nothing.
func (filesystem *Filesystem) Stage(ctx context.Context, content io.Reader, sizeHint int64) (uploads.StagedContent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filesystem.root, 0o755); err != nil {
		return nil, fmt.Errorf("creating storage root: %w", err)
	}
	file, err := os.CreateTemp(filesystem.root, stagedPrefix+"*"+stagedSuffix)
	if err != nil {
		return nil, storageError("creating staged rom file", err)
	}
	staged := &stagedFile{filesystem: filesystem, path: file.Name()}
	var size int64
	copyErr := func() error {
		if sizeHint > 0 {
			if err := reserve(file, sizeHint, false); err != nil {
				return err
			}
		}
		var err error
		if size, err = copyInBlocks(ctx, file, content); err != nil {
			return err
		}
		if sizeHint > size {
			if err := file.Truncate(size); err != nil {
				return err
			}
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if size >= uncachedSize {
			dropFromCache(file)
		}
		return nil
	}()
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		staged.Discard()
		return nil, storageError("writing staged rom file", err)
	}
	staged.size = size
	return staged, nil
}

// copyInBlocks copies content to file in writeBufferSize writes, checking ctx between them.
// Only io.EOF ends the copy successfully: any other error, including io.ErrUnexpectedEOF from a
// request body whose client disconnected, fails it, so a cut-off upload is never stored.
func copyInBlocks(ctx context.Context, file *os.File, content io.Reader) (int64, error) {
	buffer := make([]byte, writeBufferSize)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return size, err
		}
		filled, readErr := fill(content, buffer)
		if filled > 0 {
			if _, err := file.Write(buffer[:filled]); err != nil {
				return size, err
			}
			size += int64(filled)
		}
		if errors.Is(readErr, io.EOF) {
			return size, nil
		}
		if readErr != nil {
			return size, readErr
		}
	}
}

// fill reads into buffer until it is full or content returns an error, which it passes on
// unchanged (unlike io.ReadFull, which turns an early io.EOF into io.ErrUnexpectedEOF).
func fill(content io.Reader, buffer []byte) (int, error) {
	filled := 0
	for filled < len(buffer) {
		read, err := content.Read(buffer[filled:])
		filled += read
		if err != nil {
			return filled, err
		}
	}
	return filled, nil
}

// storageError reports a full disk as such; other failures keep their cause.
func storageError(operation string, err error) error {
	if isDiskFull(err) {
		return fault.InsufficientStorage()
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// FreeSpace returns the bytes free on the storage folder's disk, or false when unknown.
func (filesystem *Filesystem) FreeSpace() (int64, bool) {
	return freeSpace(filesystem.root)
}

// HasRoomFor reports whether size more bytes fit while keeping freeSpaceReserve free. It
// answers true when free space cannot be measured.
func (filesystem *Filesystem) HasRoomFor(size int64) bool {
	available, ok := freeSpace(filesystem.root)
	return !ok || size <= available-freeSpaceReserve
}

type stagedFile struct {
	filesystem *Filesystem
	path       string
	size       int64
}

func (staged *stagedFile) Size() int64 {
	return staged.size
}

// Commit moves the staged file to its content address and returns its storage key.
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
	key := keyFor(contentAddress)
	target := staged.filesystem.pathForKey(key)
	if err := staged.filesystem.moveInto(staged.path, target); err != nil {
		return "", fmt.Errorf("committing rom file: %w", err)
	}
	staged.path = ""
	return key, nil
}

func (staged *stagedFile) Discard() {
	if staged.path == "" {
		return
	}
	_ = os.Remove(staged.path)
	staged.path = ""
}

// moveInto renames source to target, creating target's folder, and flushes the folder so the
// new name survives a power cut.
func (filesystem *Filesystem) moveInto(source, target string) error {
	folder := filepath.Dir(target)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return syncFolder(folder)
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

// Open returns the stored file itself, so responses can send it with sendfile.
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
	var content io.ReadSeekCloser = file
	if info.Size() >= uncachedSize {
		content = uncachedFile{file}
	}
	return rom.StoredContent{ReadSeekCloser: content, Size: info.Size(), ModifiedAt: info.ModTime()}, nil
}

// uncachedFile drops a large file from the page cache when it is closed, after a download. It
// embeds the file, so it still offers SyscallConn and responses still send it with sendfile.
type uncachedFile struct {
	*os.File
}

func (file uncachedFile) Close() error {
	dropFromCache(file.File)
	return file.File.Close()
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

// SweepStaged removes staged files left by uploads that never finished, such as after a crash
// or power cut, once they have not been written to for olderThan, and partial uploads not
// written to for partialOlderThan (their sessions normally remove them sooner). It returns how
// many it removed.
func (filesystem *Filesystem) SweepStaged(olderThan, partialOlderThan time.Duration) (int, error) {
	entries, err := os.ReadDir(filesystem.root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("listing storage root: %w", err)
	}
	removed := 0
	stagedCutoff, partialCutoff := time.Now().Add(-olderThan), time.Now().Add(-partialOlderThan)
	for _, entry := range entries {
		name := entry.Name()
		var cutoff time.Time
		switch {
		case entry.IsDir() && strings.HasPrefix(name, workPrefix):
			// A work folder left by a crash; RemoveAll below handles folders.
			cutoff = partialCutoff
		case entry.IsDir():
			continue
		case strings.HasPrefix(name, stagedPrefix) && strings.HasSuffix(name, stagedSuffix):
			cutoff = stagedCutoff
		case strings.HasPrefix(name, partialPrefix) && strings.HasSuffix(name, partialSuffix):
			cutoff = partialCutoff
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.RemoveAll(filepath.Join(filesystem.root, name)) == nil {
			removed++
		}
	}
	return removed, nil
}

// Relocate moves files recorded by an earlier version into the sharded layout and returns each
// path's storage key. A file is found at its recorded path or, when the storage folder has moved
// since, by its name in the current folder. The folders are flushed once after every move, so
// callers record the keys only after the moves are durable. Paths that are already keys map to
// themselves; paths whose file is missing are reported in errs and left out of keys.
//
// Relocating is idempotent: after a crash between moving files and recording their keys, the
// next run finds them already in place.
func (filesystem *Filesystem) Relocate(paths []string) (keys map[string]string, errs []error) {
	keys = make(map[string]string, len(paths))
	touched := map[string]bool{}
	for _, path := range paths {
		key, moved, err := filesystem.relocate(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		keys[path] = key
		if moved {
			touched[filepath.Dir(filesystem.pathForKey(key))] = true
		}
	}
	if len(touched) > 0 {
		touched[filesystem.root] = true // the old names left it
	}
	for folder := range touched {
		if err := syncFolder(folder); err != nil {
			return map[string]string{}, append(errs, fmt.Errorf("flushing %s: %w", folder, err))
		}
	}
	return keys, errs
}

func (filesystem *Filesystem) relocate(storagePath string) (key string, moved bool, err error) {
	if isKey(storagePath) {
		return storagePath, false, nil
	}
	address := filepath.Base(storagePath)
	if !filepath.IsAbs(storagePath) || !isContentAddress(address) {
		return "", false, fmt.Errorf("storage: %q is not a content-addressed path", storagePath)
	}
	key = keyFor(address)
	target := filesystem.pathForKey(key)
	candidates := []string{filepath.Join(filesystem.root, address)}
	if inside, err := filesystem.resolve(storagePath); err == nil && inside != candidates[0] {
		candidates = append(candidates, inside)
	}
	if _, err := os.Stat(target); err == nil {
		// Already moved; drop copies left at old locations (same address, same content).
		for _, candidate := range candidates {
			_ = os.Remove(candidate)
		}
		return key, false, nil
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", false, err
		}
		if err := os.Rename(candidate, target); err != nil {
			return "", false, fmt.Errorf("relocating %s: %w", candidate, err)
		}
		return key, true, nil
	}
	return "", false, fmt.Errorf("%w: %s", ErrMissingContent, storagePath)
}

// ReservePartial creates the file for an upload sent in parts and reserves size bytes for it.
func (filesystem *Filesystem) ReservePartial(ctx context.Context, sessionID string, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := filesystem.partialPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filesystem.root, 0o755); err != nil {
		return fmt.Errorf("creating storage root: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storageError("creating partial upload", err)
	}
	reserveErr := reserve(file, size, true)
	if err := errors.Join(reserveErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return storageError("reserving partial upload", err)
	}
	return nil
}

// AppendPartial writes content to a partial upload at offset, its recorded size, and returns
// how many bytes it wrote. Bytes past offset, left by an earlier part that failed midway, are
// dropped first. The part is flushed to disk before AppendPartial returns, so recording the new
// size afterwards can never get ahead of the data.
func (filesystem *Filesystem) AppendPartial(ctx context.Context, sessionID string, offset int64, content io.Reader) (int64, error) {
	path, err := filesystem.partialPath(sessionID)
	if err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("opening partial upload: %w", err)
	}
	written, writeErr := func() (int64, error) {
		info, err := file.Stat()
		if err != nil {
			return 0, err
		}
		if info.Size() != offset {
			if err := file.Truncate(offset); err != nil {
				return 0, err
			}
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return 0, err
		}
		written, err := copyInBlocks(ctx, file, content)
		if err != nil {
			return written, err
		}
		if err := file.Sync(); err != nil {
			return written, err
		}
		if offset+written >= uncachedSize {
			dropFromCache(file)
		}
		return written, nil
	}()
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return written, storageError("writing partial upload", err)
	}
	return written, nil
}

// CommitPartial moves a finished partial upload to its content address and returns its key.
func (filesystem *Filesystem) CommitPartial(ctx context.Context, sessionID, contentAddress string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isContentAddress(contentAddress) {
		return "", errors.New("storage: invalid content address")
	}
	path, err := filesystem.partialPath(sessionID)
	if err != nil {
		return "", err
	}
	key := keyFor(contentAddress)
	if err := filesystem.moveInto(path, filesystem.pathForKey(key)); err != nil {
		return "", fmt.Errorf("committing partial upload: %w", err)
	}
	return key, nil
}

// DiscardPartial removes a partial upload; removing one that is gone is not an error.
func (filesystem *Filesystem) DiscardPartial(sessionID string) error {
	path, err := filesystem.partialPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discarding partial upload: %w", err)
	}
	return nil
}

func (filesystem *Filesystem) partialPath(sessionID string) (string, error) {
	if sessionID == "" || len(sessionID) > 64 || strings.IndexFunc(sessionID, func(char rune) bool {
		return !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-')
	}) >= 0 {
		return "", errors.New("storage: invalid upload session id")
	}
	return filepath.Join(filesystem.root, partialPrefix+sessionID+partialSuffix), nil
}

// LocalPath returns where a stored file lives on disk, for tools that need a path.
func (filesystem *Filesystem) LocalPath(storagePath string) (string, error) {
	return filesystem.resolve(storagePath)
}

// WorkDir creates an empty folder for temporary work beside the stored files (on the same
// filesystem, so results can be adopted by renaming). The caller removes it.
func (filesystem *Filesystem) WorkDir() (string, error) {
	if err := os.MkdirAll(filesystem.root, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(filesystem.root, workPrefix+"*")
}

// Adopt moves a finished file from a work folder to its content address and returns its key.
func (filesystem *Filesystem) Adopt(ctx context.Context, path, contentAddress string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isContentAddress(contentAddress) {
		return "", errors.New("storage: invalid content address")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	syncErr := file.Sync()
	if err := errors.Join(syncErr, file.Close()); err != nil {
		return "", err
	}
	key := keyFor(contentAddress)
	if err := filesystem.moveInto(path, filesystem.pathForKey(key)); err != nil {
		return "", fmt.Errorf("adopting %s: %w", path, err)
	}
	return key, nil
}

// StoredKey is a stored file and when it was last written.
type StoredKey struct {
	Key      string
	Modified time.Time
}

// WalkKeys calls visit with every file stored under a storage key, a subfolder at a time.
// Other files (staged uploads, partial uploads, anything placed there by hand) are skipped.
func (filesystem *Filesystem) WalkKeys(ctx context.Context, visit func(StoredKey) error) error {
	folders, err := os.ReadDir(filesystem.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing storage root: %w", err)
	}
	for _, folder := range folders {
		if !folder.IsDir() || len(folder.Name()) != 2 || !isContentAddress(folder.Name()+strings.Repeat("0", 62)) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(filesystem.root, folder.Name()))
		if err != nil {
			return fmt.Errorf("listing storage folder %s: %w", folder.Name(), err)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := folder.Name() + "/" + entry.Name()
			if entry.IsDir() || !isKey(key) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue // removed meanwhile
			}
			if err := visit(StoredKey{Key: key, Modified: info.ModTime()}); err != nil {
				return err
			}
		}
	}
	return nil
}

func keyFor(contentAddress string) string {
	return contentAddress[:2] + "/" + contentAddress
}

func (filesystem *Filesystem) pathForKey(key string) string {
	return filepath.Join(filesystem.root, filepath.FromSlash(key))
}

// resolve maps a storage key, or a legacy absolute path inside the storage folder, to a path.
func (filesystem *Filesystem) resolve(storagePath string) (string, error) {
	if isKey(storagePath) {
		return filesystem.pathForKey(storagePath), nil
	}
	if !filepath.IsAbs(storagePath) {
		return "", errors.New("storage: invalid storage key")
	}
	root, err := filepath.Abs(filesystem.root)
	if err != nil {
		return "", fmt.Errorf("resolving storage root: %w", err)
	}
	path := filepath.Clean(storagePath)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("storage: path is outside storage root")
	}
	return path, nil
}

// isKey reports whether value is a storage key: "ab/" followed by a content address starting ab.
func isKey(value string) bool {
	return len(value) == 67 && value[2] == '/' && isContentAddress(value[3:]) && value[:2] == value[3:5]
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
		Stage(context.Context, io.Reader, int64) (uploads.StagedContent, error)
	} = (*Filesystem)(nil)
)

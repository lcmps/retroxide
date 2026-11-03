package scraper

import (
	"bufio"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// datDiskCache keeps downloaded libretro databases as files, with each one's ETag beside it so
// a stale copy can be revalidated cheaply. A database the server does not have (some systems
// lack a genre database) is remembered with a marker, so it is not asked for on every start.
type datDiskCache struct {
	dir    string
	maxAge time.Duration
}

// cachedDAT describes the cached copy of one database.
type cachedDAT struct {
	path   string
	exists bool
	// fresh copies are used without asking the server; a missing database counts as an empty one.
	fresh   bool
	missing bool
	etag    string
}

func (cache *datDiskCache) paths(database string) (data, etag, missing string) {
	// One flat folder: the escaped name keeps "no-intro/Nintendo - Game Boy" a single file.
	base := filepath.Join(cache.dir, url.PathEscape(database))
	return base + ".dat", base + ".etag", base + ".missing"
}

func (cache *datDiskCache) lookup(database string) cachedDAT {
	dataPath, etagPath, missingPath := cache.paths(database)
	cached := cachedDAT{path: dataPath}
	if info, err := os.Stat(missingPath); err == nil && time.Since(info.ModTime()) < cache.maxAge {
		cached.fresh, cached.missing = true, true
		return cached
	}
	info, err := os.Stat(dataPath)
	if err != nil {
		return cached
	}
	cached.exists = true
	cached.fresh = time.Since(info.ModTime()) < cache.maxAge
	if etag, err := os.ReadFile(etagPath); err == nil {
		cached.etag = string(etag)
	}
	return cached
}

// read parses the cached copy, streaming it from disk.
func (cached cachedDAT) read() (*datIndex, error) {
	if cached.missing {
		return emptyDATIndex(), nil
	}
	file, err := os.Open(cached.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseDAT(bufio.NewReaderSize(file, 256<<10))
}

// store saves a downloaded database atomically, so a crash never leaves half a file that later
// reads as a smaller database. Failing to cache is not an error: the download still serves.
func (cache *datDiskCache) store(database string, body []byte, etag string) {
	dataPath, etagPath, missingPath := cache.paths(database)
	if os.MkdirAll(cache.dir, 0o755) != nil {
		return
	}
	if writeAtomically(dataPath, body) != nil {
		return
	}
	_ = os.Remove(missingPath)
	if etag == "" {
		_ = os.Remove(etagPath)
		return
	}
	_ = writeAtomically(etagPath, []byte(etag))
}

// storeMissing remembers that the server has no such database.
func (cache *datDiskCache) storeMissing(database string) {
	_, _, missingPath := cache.paths(database)
	if os.MkdirAll(cache.dir, 0o755) == nil {
		_ = writeAtomically(missingPath, nil)
	}
}

// touch renews a copy the server confirmed is current.
func (cache *datDiskCache) touch(database string) {
	dataPath, _, _ := cache.paths(database)
	now := time.Now()
	_ = os.Chtimes(dataPath, now, now)
}

func writeAtomically(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		return err
	}
	return nil
}

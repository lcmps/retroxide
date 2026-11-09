package biosdat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	biosdomain "github.com/lcmps/retroxide/domain/bios"
)

// DefaultSource is where libretro publishes System.dat.
const DefaultSource = "https://raw.githubusercontent.com/libretro/libretro-database/master/dat/System.dat"

const (
	cacheFile = "System.dat"
	etagFile  = "System.dat.etag"
	// cacheMaxAge is how long a downloaded copy is used before asking upstream for a newer one.
	cacheMaxAge = 24 * time.Hour
	// maxListBytes bounds a download; the list is well under 1 MiB.
	maxListBytes = 8 << 20
)

// Catalog recognizes BIOS files by checksum. It starts from the bundled list, prefers a newer
// copy downloaded earlier, and refreshes from upstream; a copy that fails to parse never
// replaces a working one.
type Catalog struct {
	cacheDir string
	source   string
	client   *http.Client

	mu         sync.RWMutex
	bySHA1     map[string]biosdomain.Known
	extensions []string
}

// NewCatalog loads the bundled list, then the copy cached in cacheDir when there is a valid one.
func NewCatalog(cacheDir, source string, client *http.Client) (*Catalog, error) {
	catalog := &Catalog{cacheDir: cacheDir, source: source, client: client}
	entries, err := Parse(bytes.NewReader(embedded))
	if err != nil {
		return nil, fmt.Errorf("reading the bundled BIOS list: %w", err)
	}
	if cached, err := os.ReadFile(filepath.Join(cacheDir, cacheFile)); err == nil {
		if fromCache, err := Parse(bytes.NewReader(cached)); err == nil {
			entries = fromCache
		}
	}
	catalog.use(entries)
	return catalog, nil
}

// Lookup finds the known BIOS file with this SHA-1 (any case) and size.
func (catalog *Catalog) Lookup(sha1 string, size uint64) (biosdomain.Known, bool) {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	known, ok := catalog.bySHA1[strings.ToLower(sha1)]
	if !ok || known.Size != size {
		return biosdomain.Known{}, false
	}
	return known, true
}

// Extensions lists the file types of known BIOS files: lowercase, without the dot.
func (catalog *Catalog) Extensions() []string {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	return slices.Clone(catalog.extensions)
}

// Refresh downloads a newer list when the cached copy is older than a day, and reports whether
// the list changed. A failed or invalid download leaves everything as it was.
func (catalog *Catalog) Refresh(ctx context.Context) (bool, error) {
	cachePath := filepath.Join(catalog.cacheDir, cacheFile)
	if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < cacheMaxAge {
		return false, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, catalog.source, nil)
	if err != nil {
		return false, err
	}
	if etag, err := os.ReadFile(filepath.Join(catalog.cacheDir, etagFile)); err == nil {
		request.Header.Set("If-None-Match", strings.TrimSpace(string(etag)))
	}
	response, err := catalog.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("downloading the BIOS list: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotModified:
		now := time.Now()
		return false, os.Chtimes(cachePath, now, now)
	case http.StatusOK:
	default:
		return false, fmt.Errorf("downloading the BIOS list: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxListBytes+1))
	if err != nil {
		return false, fmt.Errorf("downloading the BIOS list: %w", err)
	}
	if len(body) > maxListBytes {
		return false, errors.New("the downloaded BIOS list is too large")
	}
	entries, err := Parse(bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("the downloaded BIOS list is invalid: %w", err)
	}
	if err := os.MkdirAll(catalog.cacheDir, 0o755); err != nil {
		return false, err
	}
	if err := writeAtomically(cachePath, body); err != nil {
		return false, err
	}
	if etag := response.Header.Get("ETag"); etag != "" {
		_ = writeAtomically(filepath.Join(catalog.cacheDir, etagFile), []byte(etag))
	}
	catalog.use(entries)
	return true, nil
}

func (catalog *Catalog) use(entries []biosdomain.Known) {
	bySHA1 := make(map[string]biosdomain.Known, len(entries))
	seen := map[string]bool{}
	var extensions []string
	for _, entry := range entries {
		if _, exists := bySHA1[entry.SHA1]; !exists {
			bySHA1[entry.SHA1] = entry
		}
		if extension := strings.ToLower(strings.TrimPrefix(path.Ext(entry.Path), ".")); extension != "" && !seen[extension] {
			seen[extension] = true
			extensions = append(extensions, extension)
		}
	}
	slices.Sort(extensions)
	catalog.mu.Lock()
	catalog.bySHA1, catalog.extensions = bySHA1, extensions
	catalog.mu.Unlock()
}

func writeAtomically(target string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	_, writeErr := temporary.Write(content)
	if err := errors.Join(writeErr, temporary.Close()); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return os.Rename(temporary.Name(), target)
}

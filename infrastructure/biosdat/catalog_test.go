package biosdat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// knownPS2 returns the embedded list's entry for PS2 v1.60 Europe.
func knownPS2(t *testing.T) (sha1 string, size uint64) {
	t.Helper()
	entries, err := Parse(bytes.NewReader(embedded))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Path == "pcsx2/bios/ps2-0160e-20010704.bin" {
			return entry.SHA1, entry.Size
		}
	}
	t.Fatal("PS2 v1.60 Europe not in the embedded list")
	return "", 0
}

// withExtraPS2Entry is the embedded list with one more PS2 file, whose SHA-1 is sha1.
func withExtraPS2Entry(sha1 string) []byte {
	line := `	rom ( name "pcsx2/bios/ps2-9999e-20990101.bin" size 4194304 crc 00000000 md5 00000000000000000000000000000000 sha1 ` + sha1 + " )\n"
	marker := []byte("comment \"Sony - PlayStation 2\"\n")
	index := bytes.Index(embedded, marker) + len(marker)
	return slices.Concat(embedded[:index], []byte(line), embedded[index:])
}

func newTestCatalog(t *testing.T, cacheDir string, server *httptest.Server) *Catalog {
	t.Helper()
	source := "http://127.0.0.1:1/unreachable"
	client := http.DefaultClient
	if server != nil {
		source, client = server.URL, server.Client()
	}
	catalog, err := NewCatalog(cacheDir, source, client)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestLookupMatchesBySHA1AndSize(t *testing.T) {
	sha1, size := knownPS2(t)
	catalog := newTestCatalog(t, t.TempDir(), nil)
	known, ok := catalog.Lookup(strings.ToUpper(sha1), size)
	if !ok || known.Platform != "ps2" || known.Path != "pcsx2/bios/ps2-0160e-20010704.bin" {
		t.Fatalf("Lookup = %+v, %v", known, ok)
	}
	if _, ok := catalog.Lookup(sha1, size+1); ok {
		t.Fatal("Lookup matched a file of another size")
	}
}

func TestExtensionsComeFromTheList(t *testing.T) {
	extensions := newTestCatalog(t, t.TempDir(), nil).Extensions()
	if !slices.Contains(extensions, "bin") || !slices.Contains(extensions, "pce") {
		t.Fatalf("Extensions = %v, want bin and pce", extensions)
	}
	if slices.Contains(extensions, "yaml") || slices.Contains(extensions, ".bin") {
		t.Fatalf("Extensions = %v, want no support types and no dots", extensions)
	}
}

func TestRefreshReplacesTheListWithANewerValidOne(t *testing.T) {
	newSHA1 := strings.Repeat("ab", 20)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("ETag", `"v2"`)
		_, _ = writer.Write(withExtraPS2Entry(newSHA1))
	}))
	defer server.Close()
	cacheDir := t.TempDir()
	catalog := newTestCatalog(t, cacheDir, server)
	if _, ok := catalog.Lookup(newSHA1, 4194304); ok {
		t.Fatal("new entry known before refreshing")
	}
	changed, err := catalog.Refresh(context.Background())
	if err != nil || !changed {
		t.Fatalf("Refresh = %v, %v", changed, err)
	}
	if _, ok := catalog.Lookup(newSHA1, 4194304); !ok {
		t.Fatal("refreshed entry not found")
	}
	// A restart without network reads the cached copy.
	if _, ok := newTestCatalog(t, cacheDir, nil).Lookup(newSHA1, 4194304); !ok {
		t.Fatal("refreshed entry lost after restart")
	}
}

func TestRefreshKeepsTheListWhenTheDownloadIsBroken(t *testing.T) {
	sha1, size := knownPS2(t)
	for name, respond := range map[string]func(http.ResponseWriter){
		"html":      func(writer http.ResponseWriter) { _, _ = writer.Write([]byte("<html>rate limited</html>")) },
		"truncated": func(writer http.ResponseWriter) { _, _ = writer.Write(embedded[:len(embedded)/2]) },
		"error":     func(writer http.ResponseWriter) { writer.WriteHeader(http.StatusInternalServerError) },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { respond(writer) }))
			defer server.Close()
			cacheDir := t.TempDir()
			catalog := newTestCatalog(t, cacheDir, server)
			if changed, _ := catalog.Refresh(context.Background()); changed {
				t.Fatal("a broken download replaced the list")
			}
			if _, ok := catalog.Lookup(sha1, size); !ok {
				t.Fatal("known entry lost after a broken download")
			}
			if _, err := os.Stat(filepath.Join(cacheDir, cacheFile)); !os.IsNotExist(err) {
				t.Fatalf("a broken download was cached: %v", err)
			}
		})
	}
}

func TestRefreshSendsTheETagAndHonours304(t *testing.T) {
	var conditional []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conditional = append(conditional, request.Header.Get("If-None-Match"))
		if request.Header.Get("If-None-Match") == `"v1"` {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", `"v1"`)
		_, _ = writer.Write(embedded)
	}))
	defer server.Close()
	cacheDir := t.TempDir()
	catalog := newTestCatalog(t, cacheDir, server)
	if _, err := catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A fresh copy is not downloaded again.
	if _, err := catalog.Refresh(context.Background()); err != nil || len(conditional) != 1 {
		t.Fatalf("second refresh made %d requests, err %v", len(conditional), err)
	}
	// Once stale, the copy is revalidated with its ETag.
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(cacheDir, cacheFile), old, old); err != nil {
		t.Fatal(err)
	}
	changed, err := catalog.Refresh(context.Background())
	if err != nil || changed || len(conditional) != 2 || conditional[1] != `"v1"` {
		t.Fatalf("revalidation: changed %v, err %v, If-None-Match %q", changed, err, conditional)
	}
}

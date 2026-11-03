package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// datServer serves the SNES database with an ETag, answering 304 when the client has it.
type datServer struct {
	server       *httptest.Server
	downloads    atomic.Int32
	revalidated  atomic.Int32
	genreLookups atomic.Int32
	down         atomic.Bool
}

func newDATServer(t *testing.T) *datServer {
	t.Helper()
	upstream := &datServer{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if upstream.down.Load() {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		switch {
		case strings.HasPrefix(request.URL.Path, "/db/genre/"):
			upstream.genreLookups.Add(1)
			writer.WriteHeader(http.StatusNotFound)
		case request.Header.Get("If-None-Match") == `"v1"`:
			upstream.revalidated.Add(1)
			writer.WriteHeader(http.StatusNotModified)
		default:
			upstream.downloads.Add(1)
			writer.Header().Set("ETag", `"v1"`)
			_, _ = writer.Write([]byte(snesDAT))
		}
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (upstream *datServer) client(dir string) *Client {
	return NewClient(upstream.server.Client(), Endpoints{LibretroDatabase: upstream.server.URL + "/db"}).CacheDatabasesIn(dir, time.Hour)
}

func TestDatabasesAreCachedOnDiskAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	upstream := newDATServer(t)
	snes := libretroSystems["snes"]
	lookup := func(client *Client) {
		t.Helper()
		index, err := client.datIndex(context.Background(), snes.database, false)
		if err != nil || len(index.bySHA1) != 2 {
			t.Fatalf("database = %v, %v; want the SNES entries", index, err)
		}
	}
	lookup(upstream.client(dir))
	// A restart reads the fresh copy from disk without asking the server.
	lookup(upstream.client(dir))
	if upstream.downloads.Load() != 1 || upstream.revalidated.Load() != 0 {
		t.Fatalf("downloads = %d, revalidations = %d; want 1 and 0", upstream.downloads.Load(), upstream.revalidated.Load())
	}

	// Once stale, the copy is revalidated: the server answers 304 and nothing is downloaded.
	stale := time.Now().Add(-2 * time.Hour)
	for _, path := range mustGlob(t, filepath.Join(dir, "*.dat")) {
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	lookup(upstream.client(dir))
	if upstream.downloads.Load() != 1 || upstream.revalidated.Load() != 1 {
		t.Fatalf("after going stale: downloads = %d, revalidations = %d; want 1 and 1", upstream.downloads.Load(), upstream.revalidated.Load())
	}
	// Revalidating renewed it: the next restart uses it without asking.
	lookup(upstream.client(dir))
	if upstream.revalidated.Load() != 1 {
		t.Fatal("a renewed copy was revalidated again")
	}

	// A stale copy still serves while the server is unreachable.
	for _, path := range mustGlob(t, filepath.Join(dir, "*.dat")) {
		_ = os.Chtimes(path, stale, stale)
	}
	upstream.down.Store(true)
	lookup(upstream.client(dir))
}

func TestMissingDatabasesAreRememberedOnDisk(t *testing.T) {
	dir := t.TempDir()
	upstream := newDATServer(t)
	psxGenre := libretroSystems["psx"].genreDatabase()
	for range 2 {
		index, err := upstream.client(dir).datIndex(context.Background(), psxGenre, true)
		if err != nil || len(index.byCRC) != 0 {
			t.Fatalf("missing database = %v, %v; want empty", index, err)
		}
	}
	if upstream.genreLookups.Load() != 1 {
		t.Fatalf("missing database asked for %d times across restarts, want 1", upstream.genreLookups.Load())
	}
}

func TestIdleDatabasesLeaveMemory(t *testing.T) {
	upstream := newDATServer(t)
	client := upstream.client(t.TempDir())
	client.idleAfter = 20 * time.Millisecond
	snes := libretroSystems["snes"].database
	if _, err := client.datIndex(context.Background(), snes, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		client.mu.Lock()
		_, cached := client.datCache[snes]
		client.mu.Unlock()
		if !cached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("an idle database stayed in memory")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Needed again, it comes back from disk, not the network.
	if _, err := client.datIndex(context.Background(), snes, false); err != nil || upstream.downloads.Load() != 1 {
		t.Fatalf("reloading = %v after %d downloads, want 1", err, upstream.downloads.Load())
	}
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no files match %s (%v)", pattern, err)
	}
	return paths
}

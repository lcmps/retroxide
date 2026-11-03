package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
)

const snesDAT = `clrmamepro (
	name "Nintendo - Super Nintendo Entertainment System"
)

game (
	name "Super Mario World (USA)"
	region "USA"
	rom ( name "Super Mario World (USA).sfc" size 524288 crc B19ED489 md5 CDD3C8C37322978CA8669B34BC89C804 sha1 6B47BB75D16514B6A476AA0C73A683A2A4C18765 )
)

game (
	name "Kirby's Dream Course: Special / Rev 1 (Europe)"
	region "Europe"
	rom ( name "Kirby.sfc" size 1 crc 11111111 sha1 1111111111111111111111111111111111111111 )
)
`

const snesGenreDAT = `clrmamepro (
	name "Nintendo - Super Nintendo Entertainment System"
)

game (
	comment "Super Mario World (USA)"
	genre "Platform"
	rom ( crc B19ED489 )
)
`

var (
	png  = "\x89PNG\r\n\x1a\n" + strings.Repeat("p", 32)
	jpeg = "\xff\xd8\xff\xe0" + strings.Repeat("j", 32)
)

type fakeUpstream struct {
	server        *httptest.Server
	hasheousDown  bool
	datRequests   atomic.Int32
	genreRequests atomic.Int32
	requestedURLs []string
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	upstream := &fakeUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.EscapedPath()
		upstream.requestedURLs = append(upstream.requestedURLs, path)
		switch {
		case strings.HasPrefix(path, "/hasheous/") && upstream.hasheousDown:
			writer.WriteHeader(http.StatusServiceUnavailable)
		case path == "/hasheous/api/v1/Lookup/ByHash/sha1/6b47bb75d16514b6a476aa0c73a683a2a4c18765":
			_, _ = writer.Write([]byte(`{"name":"Super Mario World","platform":{"name":"Nintendo Super Nintendo Entertainment System"},"publisher":{"name":"Nintendo"},"signature":{"game":{"year":"1991"}},
				"attributes":[{"attributeName":"AIDescription","value":"Dinosaur Land."},{"attributeName":"Logo","value":"3F4AAFC1FCC6DF51F8E9E81CA08AE2583AA6688F"},{"attributeName":"VIMMManualId","value":"4770"}]}`))
		case path == "/hasheous/api/v1/images/3F4AAFC1FCC6DF51F8E9E81CA08AE2583AA6688F":
			_, _ = writer.Write([]byte(jpeg))
		case path == "/db/no-intro/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System.dat":
			upstream.datRequests.Add(1)
			_, _ = writer.Write([]byte(snesDAT))
		case path == "/db/genre/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System.dat":
			upstream.genreRequests.Add(1)
			_, _ = writer.Write([]byte(snesGenreDAT))
		case path == "/db/genre/Sony%20-%20PlayStation.dat":
			upstream.genreRequests.Add(1)
			writer.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(path, "/thumbs/Nintendo_-_Super_Nintendo_Entertainment_System/master/"):
			_, _ = writer.Write([]byte(png))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (upstream *fakeUpstream) client() *Client {
	return NewClient(upstream.server.Client(), Endpoints{
		Hasheous:           upstream.server.URL + "/hasheous",
		LibretroDatabase:   upstream.server.URL + "/db",
		LibretroThumbnails: upstream.server.URL + "/thumbs",
	})
}

func TestScrapeCombinesHasheousAndLibretro(t *testing.T) {
	upstream := newFakeUpstream(t)
	result, err := upstream.client().Scrape(context.Background(), rom.ScrapeQuery{
		Platform: "snes", SHA1: "6b47bb75d16514b6a476aa0c73a683a2a4c18765", CRC32: "b19ed489",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != SourceHasheous || result.Title != "Super Mario World" || result.Publisher != "Nintendo" ||
		result.ReleaseYear != "1991" || result.Description != "Dinosaur Land." || result.Region != "USA" || result.Genre != "Platform" {
		t.Fatalf("result = %+v", result)
	}
	if string(result.Cover) != jpeg {
		t.Fatal("Hasheous cover should take precedence over libretro box art")
	}
	if string(result.TitleScreen) != png || string(result.Screenshot) != png {
		t.Fatal("libretro title screen and screenshot missing")
	}
	for _, requested := range upstream.requestedURLs {
		if strings.Contains(requested, "Named_Boxarts") {
			t.Fatal("box art fetched although Hasheous supplied a cover")
		}
	}
}

func TestScrapeFallsBackToLibretroAndCachesDatabase(t *testing.T) {
	upstream := newFakeUpstream(t)
	client := upstream.client()
	query := rom.ScrapeQuery{Platform: "snes", SHA1: "1111111111111111111111111111111111111111", CRC32: "11111111"}
	for range 2 {
		result, err := client.Scrape(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if result.Source != SourceLibretro || result.Title != "Kirby's Dream Course: Special / Rev 1" || result.Region != "Europe" || len(result.Cover) == 0 {
			t.Fatalf("result = %+v", result)
		}
	}
	if upstream.datRequests.Load() != 1 {
		t.Fatalf("libretro database fetched %d times, want 1", upstream.datRequests.Load())
	}
	wantImage := "/thumbs/Nintendo_-_Super_Nintendo_Entertainment_System/master/Named_Boxarts/Kirby%27s%20Dream%20Course_%20Special%20_%20Rev%201%20%28Europe%29.png"
	found := false
	for _, requested := range upstream.requestedURLs {
		found = found || requested == wantImage
	}
	if !found {
		t.Fatalf("expected sanitized thumbnail request %s in %v", wantImage, upstream.requestedURLs)
	}
}

func TestScrapeReadsGenreByCRCAndCachesMissingGenreDatabases(t *testing.T) {
	upstream := newFakeUpstream(t)
	upstream.hasheousDown = true
	client := upstream.client()
	// The SNES genre database is keyed by CRC alone, so it adds a genre to a libretro match.
	result, err := client.Scrape(context.Background(), rom.ScrapeQuery{Platform: "snes", SHA1: "6b47bb75d16514b6a476aa0c73a683a2a4c18765", CRC32: "b19ed489"})
	if err != nil || result == nil || result.Genre != "Platform" {
		t.Fatalf("Scrape() = %+v, %v; want genre Platform", result, err)
	}
	// Kirby is not in the genre database: no genre, and the match still succeeds.
	result, err = client.Scrape(context.Background(), rom.ScrapeQuery{Platform: "snes", SHA1: "1111111111111111111111111111111111111111", CRC32: "11111111"})
	if err != nil || result == nil || result.Genre != "" {
		t.Fatalf("Scrape() = %+v, %v; want a match without genre", result, err)
	}
	if upstream.genreRequests.Load() != 1 {
		t.Fatalf("genre database fetched %d times, want 1", upstream.genreRequests.Load())
	}

	// PlayStation has no genre database upstream; the miss is cached, not retried per scrape.
	for range 2 {
		index, err := client.datIndex(context.Background(), libretroSystems["psx"].genreDatabase(), true)
		if err != nil || len(index.byCRC) != 0 {
			t.Fatalf("missing genre database = %+v, %v; want an empty index", index, err)
		}
	}
	if upstream.genreRequests.Load() != 2 {
		t.Fatalf("genre databases fetched %d times, want 2", upstream.genreRequests.Load())
	}
}

func TestScrapeReturnsNilWithoutMatchAndErrorWhenUnavailable(t *testing.T) {
	upstream := newFakeUpstream(t)
	result, err := upstream.client().Scrape(context.Background(), rom.ScrapeQuery{Platform: "snes", SHA1: strings.Repeat("0", 40), CRC32: "00000000"})
	if err != nil || result != nil {
		t.Fatalf("Scrape() = %+v, %v; want nil, nil", result, err)
	}
	result, err = upstream.client().Scrape(context.Background(), rom.ScrapeQuery{Platform: "unknown", SHA1: strings.Repeat("0", 40)})
	if err != nil || result != nil {
		t.Fatalf("unsupported platform = %+v, %v; want nil, nil", result, err)
	}

	upstream.hasheousDown = true
	result, err = upstream.client().Scrape(context.Background(), rom.ScrapeQuery{Platform: "unknown", SHA1: strings.Repeat("0", 40)})
	if !fault.IsCode(err, fault.CodeUpstreamUnavailable) || result != nil {
		t.Fatalf("outage = %+v, %v; want upstream_unavailable", result, err)
	}
	result, err = upstream.client().Scrape(context.Background(), rom.ScrapeQuery{Platform: "snes", SHA1: "1111111111111111111111111111111111111111"})
	if err != nil || result == nil || result.Source != SourceLibretro {
		t.Fatalf("libretro should still match during a Hasheous outage: %+v, %v", result, err)
	}
}

func TestFetchImageRejectsNonImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<html>not an image</html>"))
	}))
	defer server.Close()
	client := NewClient(server.Client(), Endpoints{})
	if _, err := client.fetchImage(context.Background(), server.URL); !errors.Is(err, errUpstreamNoHit) {
		t.Fatalf("fetchImage() error = %v, want rejection", err)
	}
}

func TestDatIndexSharesOneDownloadAcrossConcurrentLookups(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if strings.Contains(request.URL.Path, "Super%20Nintendo") || strings.Contains(request.URL.Path, "Super Nintendo") {
			<-release // hold the SNES download open until every caller is waiting on it
		}
		_, _ = writer.Write([]byte(snesDAT))
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.Client(), Endpoints{LibretroDatabase: server.URL})

	const callers = 8
	var waiting sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			index, err := client.datIndex(context.Background(), libretroSystems["snes"].database, false)
			if err == nil && index == nil {
				err = errors.New("nil index")
			}
			errs <- err
		}()
	}

	// Another system is not blocked behind the in-flight SNES download.
	if _, err := client.datIndex(context.Background(), libretroSystems["gba"].database, false); err != nil {
		t.Fatalf("independent system lookup failed: %v", err)
	}
	close(release)
	waiting.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want one per system", got)
	}
}

func TestScrapeUsesTheHashMatchsPlatformWhenTheExtensionDidNotTell(t *testing.T) {
	upstream := newFakeUpstream(t)
	result, err := upstream.client().Scrape(context.Background(), rom.ScrapeQuery{
		Platform: "unknown", SHA1: "6b47bb75d16514b6a476aa0c73a683a2a4c18765", CRC32: "b19ed489",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Platform != "snes" {
		t.Fatalf("Platform = %q, want the catalog key for the Hasheous platform name", result.Platform)
	}
	// The libretro lookup ran for the reported platform, supplying region and artwork.
	if result.Region != "USA" || len(result.TitleScreen) == 0 || upstream.datRequests.Load() != 1 {
		t.Fatalf("region=%q title screen=%d bytes dat requests=%d", result.Region, len(result.TitleScreen), upstream.datRequests.Load())
	}
}

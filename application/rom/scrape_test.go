package rom

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type fakeScraper struct {
	result *ScrapeResult
	err    error
	query  ScrapeQuery
	// during runs while the lookup is in flight.
	during func()
}

func (scraper *fakeScraper) Scrape(_ context.Context, query ScrapeQuery) (*ScrapeResult, error) {
	scraper.query = query
	if scraper.during != nil {
		scraper.during()
	}
	return scraper.result, scraper.err
}

var pngBytes = []byte("\x89PNG\r\n\x1a\nimage")

func scrapeFixture(t *testing.T, identityValue romdomain.ROMIdentity) (*romTestRepository, *romTestStorage) {
	t.Helper()
	hash, err := romdomain.NewROMHash(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"deadbeef",
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := &romTestRepository{
		roms:     map[string]romdomain.ROM{"rom-1": {ID: "rom-1", ROMFileID: "file-1", UploaderID: "user-1", Identity: identityValue}},
		romFiles: map[string]romdomain.ROMFile{"file-1": {ID: "file-1", Hash: hash}},
	}
	return repository, &romTestStorage{files: make(map[string][]byte)}
}

var owner = identity.Requester{UserID: "user-1", Role: identity.RoleUser}

func TestScrapeFillsFilenameInferredROM(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{
		Title: "super mario world", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename, Confidence: 70,
	})
	scraper := &fakeScraper{result: &ScrapeResult{
		Source: "hasheous", Title: "Super Mario World", Region: "USA", Publisher: "Nintendo", ReleaseYear: "1991", Genre: "Platform",
		Description: "Dinosaur Land.", Cover: pngBytes, TitleScreen: pngBytes, Screenshot: pngBytes,
	}}
	service := NewService(repository, storage, romTestAudit{}, scraper)

	detail, err := service.Scrape(context.Background(), owner, "rom-1")
	if err != nil {
		t.Fatal(err)
	}
	if scraper.query != (ScrapeQuery{Platform: "snes", SHA1: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CRC32: "deadbeef", MD5: "cccccccccccccccccccccccccccccccc"}) {
		t.Fatalf("scrape query = %+v", scraper.query)
	}
	if detail.Title != "Super Mario World" || *detail.Publisher != "Nintendo" || *detail.ReleaseYear != "1991" || *detail.Region != "USA" || *detail.Description != "Dinosaur Land." ||
		detail.Genre == nil || *detail.Genre != "Platform" {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.CoverArtURL == nil || !strings.HasPrefix(*detail.TitleScreenURL, "/api/v1/rom/rom-1/media/title_screen?v=") || !strings.HasPrefix(*detail.ScreenshotURL, "/api/v1/rom/rom-1/media/screenshot?v=") {
		t.Fatalf("media urls = %v %v %v", detail.CoverArtURL, detail.TitleScreenURL, detail.ScreenshotURL)
	}
	saved := repository.roms["rom-1"].Identity
	if saved.IdentifiedBy != "hasheous" || saved.Confidence != scrapedConfidence {
		t.Fatalf("identity source = %q/%d", saved.IdentifiedBy, saved.Confidence)
	}
	image, err := service.OpenMedia(context.Background(), owner, "rom-1", MediaScreenshot)
	if err != nil {
		t.Fatal(err)
	}
	screenshot, _ := io.ReadAll(image)
	if string(screenshot) != string(pngBytes) || image.Version == "" {
		t.Fatalf("OpenMedia() = %q, version %q", screenshot, image.Version)
	}
}

func TestScrapeKeepsManualTitleAndExistingData(t *testing.T) {
	cover, publisher := "/storage/manual-cover", "Manual Publisher"
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{
		Title: "My Custom Title", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByManual, Confidence: 100,
		CoverArtPath: &cover, Publisher: &publisher,
	})
	service := NewService(repository, storage, romTestAudit{}, &fakeScraper{result: &ScrapeResult{
		Source: "hasheous", Title: "Super Mario World", Publisher: "Nintendo", ReleaseYear: "1991", Genre: strings.Repeat("g", MaxGenreLength+1), Cover: pngBytes,
	}})

	detail, err := service.Scrape(context.Background(), owner, "rom-1")
	if err != nil {
		t.Fatal(err)
	}
	saved := repository.roms["rom-1"].Identity
	if detail.Title != "My Custom Title" || saved.IdentifiedBy != romdomain.IdentifiedByManual {
		t.Fatalf("manual title overwritten: %q (%s)", detail.Title, saved.IdentifiedBy)
	}
	if *saved.CoverArtPath != cover || *saved.Publisher != publisher {
		t.Fatalf("existing data overwritten: cover=%s publisher=%s", *saved.CoverArtPath, *saved.Publisher)
	}
	if saved.ReleaseYear == nil || *saved.ReleaseYear != "1991" {
		t.Fatalf("missing year not filled: %v", saved.ReleaseYear)
	}
	if saved.Genre != nil {
		t.Fatalf("overlong scraped genre stored: %q", *saved.Genre)
	}
	if len(storage.files) != 0 {
		t.Fatalf("cover stored despite existing cover: %v", storage.files)
	}
}

func TestScrapeReportsNoMatchAndScraperErrors(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "x", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename})
	service := NewService(repository, storage, romTestAudit{}, &fakeScraper{})
	if _, err := service.Scrape(context.Background(), owner, "rom-1"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("no match error = %v", err)
	}
	upstream := fault.UpstreamUnavailable("down", nil)
	service = NewService(repository, storage, romTestAudit{}, &fakeScraper{err: upstream})
	if _, err := service.Scrape(context.Background(), owner, "rom-1"); !fault.IsCode(err, fault.CodeUpstreamUnavailable) {
		t.Fatalf("upstream error = %v", err)
	}
	if _, err := service.Scrape(context.Background(), identity.Requester{UserID: "other", Role: identity.RoleUser}, "rom-1"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("foreign ROM scrape error = %v", err)
	}
	if repository.roms["rom-1"].Identity.Title != "x" {
		t.Fatal("ROM changed after failed scrape")
	}
}

func TestUpdateMarksChangedTitleAsManual(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "Old", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename})
	service := NewService(repository, storage, romTestAudit{}, nil)
	if _, err := service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{Title: "Old", Platform: "snes"}); err != nil {
		t.Fatal(err)
	}
	if repository.roms["rom-1"].Identity.IdentifiedBy != romdomain.IdentifiedByFilename {
		t.Fatal("unchanged title marked manual")
	}
	if _, err := service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{Title: "New", Platform: "snes"}); err != nil {
		t.Fatal(err)
	}
	if repository.roms["rom-1"].Identity.IdentifiedBy != romdomain.IdentifiedByManual {
		t.Fatal("edited title not marked manual")
	}
}

func TestScrapeAdoptsTheReportedPlatformOnlyWhileInferred(t *testing.T) {
	tests := []struct {
		name         string
		identity     romdomain.ROMIdentity
		reported     string
		wantPlatform string
	}{
		{name: "unknown platform is filled", identity: romdomain.ROMIdentity{Title: "Alex Kidd", Platform: "unknown", IdentifiedBy: romdomain.IdentifiedByFilename}, reported: "sms", wantPlatform: "sms"},
		{name: "extension guess is corrected", identity: romdomain.ROMIdentity{Title: "Lumines", Platform: "psx", IdentifiedBy: romdomain.IdentifiedByFilename}, reported: "psp", wantPlatform: "psp"},
		{name: "manual platform is kept", identity: romdomain.ROMIdentity{Title: "Mine", Platform: "gb", IdentifiedBy: romdomain.IdentifiedByManual}, reported: "gbc", wantPlatform: "gb"},
		{name: "unknown is filled even when manual", identity: romdomain.ROMIdentity{Title: "Mine", Platform: "unknown", IdentifiedBy: romdomain.IdentifiedByManual}, reported: "Sega Pico", wantPlatform: "Sega Pico"},
		{name: "no report keeps the platform", identity: romdomain.ROMIdentity{Title: "Alex Kidd", Platform: "unknown", IdentifiedBy: romdomain.IdentifiedByFilename}, wantPlatform: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, storage := scrapeFixture(t, test.identity)
			scraper := &fakeScraper{result: &ScrapeResult{Source: "hasheous", Title: "Scraped", Platform: test.reported}}
			detail, err := NewService(repository, storage, romTestAudit{}, scraper).Scrape(context.Background(), owner, "rom-1")
			if err != nil {
				t.Fatal(err)
			}
			if detail.Platform != test.wantPlatform || detail.PlatformName != romdomain.PlatformName(test.wantPlatform) {
				t.Fatalf("platform = %q (%q), want %q", detail.Platform, detail.PlatformName, test.wantPlatform)
			}
		})
	}
}

func TestScrapeUploadScrapesTheUploadsROMAndToleratesMissesAndDeletions(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "smw", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename})
	repository.uploadROMs = map[string]string{"upl-1": "rom-1", "upl-deleted": "rom-gone"}
	scraper := &fakeScraper{result: &ScrapeResult{Source: "hasheous", Title: "Super Mario World"}}
	service := NewService(repository, storage, romTestAudit{}, scraper)
	ctx := context.Background()

	if err := service.ScrapeUpload(ctx, "upl-1"); err != nil {
		t.Fatal(err)
	}
	if got := repository.roms["rom-1"].Identity.Title; got != "Super Mario World" {
		t.Fatalf("title after ScrapeUpload = %q", got)
	}
	for _, uploadID := range []string{"upl-unidentified", "upl-deleted"} {
		if err := service.ScrapeUpload(ctx, uploadID); err != nil {
			t.Fatalf("ScrapeUpload(%q) = %v, want nil", uploadID, err)
		}
	}

	scraper.result = nil
	if err := service.ScrapeUpload(ctx, "upl-1"); err != nil {
		t.Fatalf("ScrapeUpload() with no match = %v, want nil", err)
	}
	scraper.err = fault.UpstreamUnavailable("down", nil)
	if err := service.ScrapeUpload(ctx, "upl-1"); !fault.IsCode(err, fault.CodeUpstreamUnavailable) {
		t.Fatalf("ScrapeUpload() with upstream down = %v, want an error so the job retries", err)
	}
}

func TestPlatformsListsVisiblePlatformsByNameWithUnknownLast(t *testing.T) {
	repository := &romTestRepository{roms: map[string]romdomain.ROM{
		"r1": {ID: "r1", UploaderID: "user-1", Identity: romdomain.ROMIdentity{Platform: "sms"}},
		"r2": {ID: "r2", UploaderID: "user-1", Identity: romdomain.ROMIdentity{Platform: "unknown"}},
		"r3": {ID: "r3", UploaderID: "user-1", Identity: romdomain.ROMIdentity{Platform: "gba"}},
		"r4": {ID: "r4", UploaderID: "user-1", Identity: romdomain.ROMIdentity{Platform: "gba"}},
		"r5": {ID: "r5", UploaderID: "other", Identity: romdomain.ROMIdentity{Platform: "vita"}},
	}}
	service := NewService(repository, &romTestStorage{files: map[string][]byte{}}, romTestAudit{}, nil)

	platforms, err := service.Platforms(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	want := []PlatformDTO{
		{Key: "gba", Name: "Game Boy Advance", ROMCount: 2},
		{Key: "sms", Name: "Master System", ROMCount: 1},
		{Key: "unknown", Name: "Unknown", ROMCount: 1},
	}
	if len(platforms) != len(want) {
		t.Fatalf("Platforms() = %+v, want %+v", platforms, want)
	}
	for index := range want {
		if platforms[index] != want[index] {
			t.Fatalf("Platforms() = %+v, want %+v", platforms, want)
		}
	}
	admin, err := service.Platforms(context.Background(), identity.Requester{UserID: "admin", Role: identity.RoleAdmin})
	if err != nil || len(admin) != 4 {
		t.Fatalf("admin Platforms() = %+v, %v; want all 4 platforms", admin, err)
	}
}

// fakeThumbnailer returns a fixed thumbnail, or err.
type fakeThumbnailer struct {
	calls int
	err   error
}

func (thumbnailer *fakeThumbnailer) Thumbnail([]byte) ([]byte, error) {
	thumbnailer.calls++
	if thumbnailer.err != nil {
		return nil, thumbnailer.err
	}
	return []byte("\xff\xd8\xffthumbnail"), nil
}

func TestScrapeStoresACoverThumbnail(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "x", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename})
	thumbnailer := &fakeThumbnailer{}
	service := NewService(repository, storage, romTestAudit{}, &fakeScraper{result: &ScrapeResult{Source: "hasheous", Title: "X", Cover: pngBytes}}).WithThumbnails(thumbnailer)
	if _, err := service.Scrape(context.Background(), owner, "rom-1"); err != nil {
		t.Fatal(err)
	}
	saved := repository.roms["rom-1"].Identity
	if saved.CoverThumbPath == nil || *saved.CoverThumbPath == *saved.CoverArtPath {
		t.Fatalf("thumbnail path = %v, want its own copy", saved.CoverThumbPath)
	}
	image, err := service.OpenCoverThumbnail(context.Background(), owner, "rom-1")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(image)
	if string(content) != "\xff\xd8\xffthumbnail" || thumbnailer.calls != 1 {
		t.Fatalf("thumbnail = %q after %d scalings", content, thumbnailer.calls)
	}
	summary := Summarize(romdomain.ROMWithFile{ROM: repository.roms["rom-1"]})
	if summary.CoverThumbURL == nil || !strings.HasPrefix(*summary.CoverThumbURL, "/api/v1/rom/rom-1/cover/thumb?v=") {
		t.Fatalf("cover_thumb_url = %v", summary.CoverThumbURL)
	}
}

func TestCoverThumbnailsAreMadeOnFirstRequestForOlderCovers(t *testing.T) {
	cover := "/storage/old-cover"
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "x", Platform: "snes", CoverArtPath: &cover})
	storage.files[cover] = pngBytes
	thumbnailer := &fakeThumbnailer{}
	service := NewService(repository, storage, romTestAudit{}, nil).WithThumbnails(thumbnailer)
	for range 2 {
		if _, err := service.OpenCoverThumbnail(context.Background(), owner, "rom-1"); err != nil {
			t.Fatal(err)
		}
	}
	if thumbnailer.calls != 1 || repository.roms["rom-1"].Identity.CoverThumbPath == nil {
		t.Fatalf("scaled %d times, thumbnail %v; want one recorded scaling", thumbnailer.calls, repository.roms["rom-1"].Identity.CoverThumbPath)
	}

	// A cover that is already small, or cannot be decoded, is its own thumbnail.
	repository, storage = scrapeFixture(t, romdomain.ROMIdentity{Title: "x", Platform: "snes", CoverArtPath: &cover})
	storage.files[cover] = pngBytes
	service = NewService(repository, storage, romTestAudit{}, nil).WithThumbnails(&fakeThumbnailer{err: ErrThumbnailNotSmaller})
	if _, err := service.OpenCoverThumbnail(context.Background(), owner, "rom-1"); err != nil {
		t.Fatal(err)
	}
	if path := repository.roms["rom-1"].Identity.CoverThumbPath; path == nil || *path != cover {
		t.Fatalf("thumbnail path = %v, want the cover itself", path)
	}

	// Without a cover there is no thumbnail.
	repository, storage = scrapeFixture(t, romdomain.ROMIdentity{Title: "x", Platform: "snes"})
	service = NewService(repository, storage, romTestAudit{}, nil).WithThumbnails(thumbnailer)
	if _, err := service.OpenCoverThumbnail(context.Background(), owner, "rom-1"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("thumbnail without cover = %v, want not found", err)
	}
}

func TestAScrapeFinishingAfterTheROMWasRemovedStoresNothing(t *testing.T) {
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{Title: "smw", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByFilename})
	repository.uploadROMs = map[string]string{"upl-1": "rom-1"}
	scraper := &fakeScraper{result: &ScrapeResult{Source: "hasheous", Title: "Super Mario World"}}
	scraper.during = func() { delete(repository.roms, "rom-1") } // a merge or deletion commits meanwhile
	service := NewService(repository, storage, romTestAudit{}, scraper)
	ctx := context.Background()

	if _, err := service.Scrape(ctx, owner, "rom-1"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Scrape() = %v, want not found", err)
	}
	if err := service.ScrapeUpload(ctx, "upl-1"); err != nil {
		t.Fatalf("ScrapeUpload() = %v, want nil", err)
	}
	if len(repository.roms) != 0 {
		t.Errorf("roms = %v, want none stored", repository.roms)
	}
}

package rom

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type fakeScraper struct {
	result *ScrapeResult
	err    error
	query  ScrapeQuery
}

func (scraper *fakeScraper) Scrape(_ context.Context, query ScrapeQuery) (*ScrapeResult, error) {
	scraper.query = query
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
		Source: "hasheous", Title: "Super Mario World", Region: "USA", Publisher: "Nintendo", ReleaseYear: "1991",
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
	if detail.Title != "Super Mario World" || *detail.Publisher != "Nintendo" || *detail.ReleaseYear != "1991" || *detail.Region != "USA" || *detail.Description != "Dinosaur Land." {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.CoverArtURL == nil || *detail.TitleScreenURL != "/api/v1/rom/rom-1/media/title_screen" || *detail.ScreenshotURL != "/api/v1/rom/rom-1/media/screenshot" {
		t.Fatalf("media urls = %v %v %v", detail.CoverArtURL, detail.TitleScreenURL, detail.ScreenshotURL)
	}
	saved := repository.roms["rom-1"].Identity
	if saved.IdentifiedBy != "hasheous" || saved.Confidence != scrapedConfidence {
		t.Fatalf("identity source = %q/%d", saved.IdentifiedBy, saved.Confidence)
	}
	screenshot, err := service.ReadMedia(context.Background(), owner, "rom-1", MediaScreenshot)
	if err != nil || string(screenshot) != string(pngBytes) {
		t.Fatalf("ReadMedia() = %q, %v", screenshot, err)
	}
}

func TestScrapeKeepsManualTitleAndExistingData(t *testing.T) {
	cover, publisher := "/storage/manual-cover", "Manual Publisher"
	repository, storage := scrapeFixture(t, romdomain.ROMIdentity{
		Title: "My Custom Title", Platform: "snes", IdentifiedBy: romdomain.IdentifiedByManual, Confidence: 100,
		CoverArtPath: &cover, Publisher: &publisher,
	})
	service := NewService(repository, storage, romTestAudit{}, &fakeScraper{result: &ScrapeResult{
		Source: "hasheous", Title: "Super Mario World", Publisher: "Nintendo", ReleaseYear: "1991", Cover: pngBytes,
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

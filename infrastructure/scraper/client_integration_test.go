//go:build integration

package scraper

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/rom"
)

func TestScrapeLiveServices(t *testing.T) {
	client := NewClient(&http.Client{Timeout: 30 * time.Second}, DefaultEndpoints())
	result, err := client.Scrape(context.Background(), rom.ScrapeQuery{
		Platform: "snes",
		SHA1:     "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
		CRC32:    "b19ed489",
		MD5:      "cdd3c8c37322978ca8669b34bc89c804",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Title != "Super Mario World" || result.Publisher == "" || result.ReleaseYear == "" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Cover) == 0 || len(result.TitleScreen) == 0 || len(result.Screenshot) == 0 {
		t.Fatalf("images: cover=%d title=%d screenshot=%d", len(result.Cover), len(result.TitleScreen), len(result.Screenshot))
	}
	t.Logf("source=%s title=%q publisher=%q year=%q region=%q description=%d chars cover=%dB title=%dB screenshot=%dB",
		result.Source, result.Title, result.Publisher, result.ReleaseYear, result.Region, len(result.Description),
		len(result.Cover), len(result.TitleScreen), len(result.Screenshot))
}

package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/rom"
)

func TestRegistryServesMetricsOnlyWithTheToken(t *testing.T) {
	registry := NewRegistry(func(context.Context) (Counts, error) {
		return Counts{ROMs: 3, ROMFiles: 2, ROMBytes: 125829120, Users: 1, UploadSessions: 1, UploadSessionBytes: 99, JobsQueued: map[string]int64{"scrape_rom": 2, "identify_rom": 1}}, nil
	}, func() (int64, bool) { return 123456, true })
	registry.UploadsAccepted.Inc()
	registry.UploadedBytes.Add(2048)
	registry.CountResponse(204)
	registry.CountResponse(503)
	registry.CountResponse(99) // ignored
	handler := registry.Handler("metrics-token-1234")

	for _, authorization := range []string{"", "Bearer wrong-token-000000", "metrics-token-1234"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.Header.Set("Authorization", authorization)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q = %d, want 401", authorization, response.Code)
		}
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer metrics-token-1234")
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("metrics = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	for _, line := range []string{
		"# TYPE retroxide_uploads_accepted_total counter",
		"retroxide_uploads_accepted_total 1",
		"retroxide_uploaded_bytes_total 2048",
		`retroxide_http_responses_total{class="2xx"} 1`,
		`retroxide_http_responses_total{class="5xx"} 1`,
		"retroxide_database_up 1",
		"retroxide_roms 3",
		"retroxide_rom_files_bytes 125829120",
		"retroxide_upload_sessions_reserved_bytes 99",
		`retroxide_jobs_queued{kind="identify_rom"} 1`,
		`retroxide_jobs_queued{kind="scrape_rom"} 2`,
		"retroxide_storage_free_bytes 123456",
		"retroxide_upload_sessions 1",
		"# TYPE go_goroutines gauge",
	} {
		if !strings.Contains(body, line+"\n") {
			t.Errorf("metrics lack %q:\n%s", line, body)
		}
	}
	// Each metric's samples follow its HELP and TYPE lines, as the format requires.
	if strings.Index(body, "# HELP retroxide_jobs_queued") > strings.Index(body, "retroxide_jobs_queued{") {
		t.Error("samples precede their metadata")
	}

	// A database that does not answer is reported as down, not as zeros.
	down := NewRegistry(func(context.Context) (Counts, error) { return Counts{}, errors.New("offline") }, nil)
	var text strings.Builder
	if err := down.Write(context.Background(), &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "retroxide_database_up 0\n") || strings.Contains(text.String(), "retroxide_roms ") {
		t.Fatalf("metrics with the database down:\n%s", text.String())
	}
	// Without a token nothing is served, whatever the request carries.
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer ")
	NewRegistry(nil, nil).Handler("").ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("tokenless metrics = %d, want 401", response.Code)
	}
}

type fakeScraper struct {
	result *rom.ScrapeResult
	err    error
}

func (scraper fakeScraper) Scrape(context.Context, rom.ScrapeQuery) (*rom.ScrapeResult, error) {
	return scraper.result, scraper.err
}

type nopAudit struct{ calls int }

func (audit *nopAudit) UploadAccepted(context.Context, string, string, string, uint64) { audit.calls++ }
func (audit *nopAudit) DuplicateUpload(context.Context, string, string)                { audit.calls++ }
func (audit *nopAudit) DownloadServed(context.Context, string, string, string, uint64) {
	audit.calls++
}

func TestInstrumentsCountOutcomesAndPassCallsOn(t *testing.T) {
	registry := NewRegistry(nil, nil)
	for _, scraper := range []fakeScraper{{result: &rom.ScrapeResult{}}, {}, {err: errors.New("down")}, {err: errors.New("down")}} {
		_, _ = NewCountingScraper(scraper, registry).Scrape(context.Background(), rom.ScrapeQuery{})
	}
	if registry.ScrapesMatched.Load() != 1 || registry.ScrapesUnmatched.Load() != 1 || registry.ScrapesFailed.Load() != 2 {
		t.Fatalf("scrapes = %d/%d/%d", registry.ScrapesMatched.Load(), registry.ScrapesUnmatched.Load(), registry.ScrapesFailed.Load())
	}
	next := &nopAudit{}
	audit := NewCountingAudit(next, registry)
	audit.UploadAccepted(context.Background(), "u", "f", "s", 10)
	audit.DuplicateUpload(context.Background(), "f", "s")
	audit.DownloadServed(context.Background(), "r", "f", "s", 7)
	if next.calls != 3 || registry.UploadedBytes.Load() != 10 || registry.UploadsDuplicate.Load() != 1 || registry.DownloadedBytes.Load() != 7 || registry.Downloads.Load() != 1 {
		t.Fatalf("audit calls %d, uploaded %d, duplicates %d, downloaded %d", next.calls, registry.UploadedBytes.Load(), registry.UploadsDuplicate.Load(), registry.DownloadedBytes.Load())
	}
}

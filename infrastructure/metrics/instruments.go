package metrics

import (
	"context"

	"github.com/lcmps/retroxide/application/rom"
)

// AuditRecorder is what the server reports uploads and downloads to.
type AuditRecorder interface {
	UploadAccepted(ctx context.Context, uploadID, romFileID, sha256 string, sizeBytes uint64)
	DuplicateUpload(ctx context.Context, romFileID, sha256 string)
	DownloadServed(ctx context.Context, romID, romFileID, sha256 string, sizeBytes uint64)
}

// CountingAudit counts uploads and downloads, then passes each on to the audit log.
type CountingAudit struct {
	next     AuditRecorder
	registry *Registry
}

func NewCountingAudit(next AuditRecorder, registry *Registry) *CountingAudit {
	return &CountingAudit{next: next, registry: registry}
}

func (audit *CountingAudit) UploadAccepted(ctx context.Context, uploadID, romFileID, sha256 string, sizeBytes uint64) {
	audit.registry.UploadsAccepted.Inc()
	audit.registry.UploadedBytes.Add(sizeBytes)
	audit.next.UploadAccepted(ctx, uploadID, romFileID, sha256, sizeBytes)
}

func (audit *CountingAudit) DuplicateUpload(ctx context.Context, romFileID, sha256 string) {
	audit.registry.UploadsDuplicate.Inc()
	audit.next.DuplicateUpload(ctx, romFileID, sha256)
}

func (audit *CountingAudit) DownloadServed(ctx context.Context, romID, romFileID, sha256 string, sizeBytes uint64) {
	audit.registry.Downloads.Inc()
	audit.registry.DownloadedBytes.Add(sizeBytes)
	audit.next.DownloadServed(ctx, romID, romFileID, sha256, sizeBytes)
}

// CountingScraper counts metadata lookups by outcome.
type CountingScraper struct {
	next     rom.Scraper
	registry *Registry
}

var _ rom.Scraper = (*CountingScraper)(nil)

func NewCountingScraper(next rom.Scraper, registry *Registry) *CountingScraper {
	return &CountingScraper{next: next, registry: registry}
}

func (scraper *CountingScraper) Scrape(ctx context.Context, query rom.ScrapeQuery) (*rom.ScrapeResult, error) {
	result, err := scraper.next.Scrape(ctx, query)
	switch {
	case err != nil:
		scraper.registry.ScrapesFailed.Inc()
	case result == nil:
		scraper.registry.ScrapesUnmatched.Inc()
	default:
		scraper.registry.ScrapesMatched.Inc()
	}
	return result, err
}

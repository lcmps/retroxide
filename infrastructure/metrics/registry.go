// Package metrics exposes the server's activity and state in the Prometheus text format, for
// people who monitor their server. It has no dependencies: a handful of counters and gauges do
// not need a client library.
package metrics

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Counter only goes up.
type Counter struct{ value atomic.Uint64 }

func (counter *Counter) Add(delta uint64) { counter.value.Add(delta) }
func (counter *Counter) Inc()             { counter.value.Add(1) }
func (counter *Counter) Load() uint64     { return counter.value.Load() }

// Counts are the gauges read from the database when metrics are requested.
type Counts struct {
	ROMs, ROMFiles, ROMBytes, Users    int64
	UploadSessions, UploadSessionBytes int64
	DeadLetters                        int64
	// JobsQueued counts queued jobs by kind.
	JobsQueued map[string]int64
}

// Registry holds the counters the server updates as it works, and reads the rest on request.
type Registry struct {
	started time.Time

	UploadsAccepted, UploadsDuplicate, UploadedBytes Counter
	Downloads, DownloadedBytes                       Counter
	ScrapesMatched, ScrapesUnmatched, ScrapesFailed  Counter
	// responses counts HTTP responses by status class: 1xx at 0 through 5xx at 4.
	responses [5]Counter

	counts    func(context.Context) (Counts, error)
	freeSpace func() (int64, bool)
}

// NewRegistry returns a registry reading database gauges with counts and free disk space with
// freeSpace (which reports false when it cannot be measured).
func NewRegistry(counts func(context.Context) (Counts, error), freeSpace func() (int64, bool)) *Registry {
	return &Registry{started: time.Now(), counts: counts, freeSpace: freeSpace}
}

// CountResponse records an HTTP response's status.
func (registry *Registry) CountResponse(status int) {
	if class := status/100 - 1; class >= 0 && class < len(registry.responses) {
		registry.responses[class].Inc()
	}
}

// Handler serves the metrics to requests bearing token. Without a token it serves nothing, so
// metrics are only exposed when the administrator chose to.
func (registry *Registry) Handler(token string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		bearer, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if token == "" || !found || subtle.ConstantTimeCompare([]byte(bearer), []byte(token)) != 1 {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		if err := registry.Write(request.Context(), writer); err != nil {
			// Headers are sent; the scraper sees a truncated response and marks the scrape failed.
			return
		}
	})
}

// Write writes every metric in the Prometheus text exposition format.
func (registry *Registry) Write(ctx context.Context, out io.Writer) error {
	writer := &metricWriter{out: out}
	counter := func(name, help string, value uint64) {
		writer.metric(name, "counter", help, sample{value: float64(value)})
	}
	gauge := func(name, help string, value int64) {
		writer.metric(name, "gauge", help, sample{value: float64(value)})
	}

	counter("retroxide_uploads_accepted_total", "Uploads accepted as new ROM files.", registry.UploadsAccepted.Load())
	counter("retroxide_uploads_duplicate_total", "Uploads refused because the library already had the ROM.", registry.UploadsDuplicate.Load())
	counter("retroxide_uploaded_bytes_total", "Bytes of ROM files accepted from uploads.", registry.UploadedBytes.Load())
	counter("retroxide_downloads_total", "ROM downloads served, in full or in part.", registry.Downloads.Load())
	counter("retroxide_downloaded_bytes_total", "Bytes of ROM files sent to downloads.", registry.DownloadedBytes.Load())
	writer.metric("retroxide_scrapes_total", "counter", "Metadata lookups by outcome.",
		sample{labels: `result="matched"`, value: float64(registry.ScrapesMatched.Load())},
		sample{labels: `result="unmatched"`, value: float64(registry.ScrapesUnmatched.Load())},
		sample{labels: `result="failed"`, value: float64(registry.ScrapesFailed.Load())},
	)
	responses := make([]sample, len(registry.responses))
	for class := range registry.responses {
		responses[class] = sample{labels: fmt.Sprintf(`class="%dxx"`, class+1), value: float64(registry.responses[class].Load())}
	}
	writer.metric("retroxide_http_responses_total", "counter", "HTTP responses by status class.", responses...)

	if registry.counts != nil {
		counts, err := registry.counts(ctx)
		if err != nil {
			gauge("retroxide_database_up", "Whether the database answered the metrics queries.", 0)
		} else {
			gauge("retroxide_database_up", "Whether the database answered the metrics queries.", 1)
			gauge("retroxide_roms", "ROMs in the library.", counts.ROMs)
			gauge("retroxide_rom_files", "Distinct stored ROM files.", counts.ROMFiles)
			gauge("retroxide_rom_files_bytes", "Bytes of stored ROM files.", counts.ROMBytes)
			gauge("retroxide_users", "User accounts.", counts.Users)
			gauge("retroxide_upload_sessions", "Uploads in progress that are sent in parts.", counts.UploadSessions)
			gauge("retroxide_upload_sessions_reserved_bytes", "Disk space reserved for uploads in progress.", counts.UploadSessionBytes)
			gauge("retroxide_jobs_dead_lettered", "Background jobs that failed for good.", counts.DeadLetters)
			kinds := make([]string, 0, len(counts.JobsQueued))
			for kind := range counts.JobsQueued {
				kinds = append(kinds, kind)
			}
			sort.Strings(kinds)
			queued := make([]sample, 0, len(kinds))
			for _, kind := range kinds {
				queued = append(queued, sample{labels: `kind="` + escapeLabel(kind) + `"`, value: float64(counts.JobsQueued[kind])})
			}
			writer.metric("retroxide_jobs_queued", "gauge", "Background jobs waiting or running, by kind.", queued...)
		}
	}
	if registry.freeSpace != nil {
		if free, ok := registry.freeSpace(); ok {
			gauge("retroxide_storage_free_bytes", "Free disk space where ROMs are stored.", free)
		}
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	gauge("go_goroutines", "Goroutines that currently exist.", int64(runtime.NumGoroutine()))
	gauge("go_memstats_heap_alloc_bytes", "Bytes of allocated heap objects.", int64(memory.HeapAlloc))
	gauge("go_memstats_sys_bytes", "Bytes of memory obtained from the operating system.", int64(memory.Sys))
	writer.metric("process_start_time_seconds", "gauge", "When the server started, in Unix seconds.", sample{value: float64(registry.started.Unix())})
	writer.metric("retroxide_build_info", "gauge", "The Go version the server was built with.", sample{labels: `go_version="` + runtime.Version() + `"`, value: 1})
	return writer.err
}

type sample struct {
	labels string
	value  float64
}

type metricWriter struct {
	out io.Writer
	err error
}

func (writer *metricWriter) metric(name, kind, help string, samples ...sample) {
	if writer.err != nil {
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	for _, sample := range samples {
		if sample.labels != "" {
			fmt.Fprintf(&text, "%s{%s} %s\n", name, sample.labels, formatValue(sample.value))
		} else {
			fmt.Fprintf(&text, "%s %s\n", name, formatValue(sample.value))
		}
	}
	_, writer.err = io.WriteString(writer.out, text.String())
}

// formatValue writes whole numbers in full (byte counts would otherwise read 1.2e+08).
func formatValue(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func escapeLabel(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

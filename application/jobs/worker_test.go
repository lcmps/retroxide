package jobs

import (
	"context"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// identifyPool runs workers that only identify uploads.
func identifyPool(workers int, identifier Identifier, notifier *Notifier) Pool {
	return Pool{Name: "identify", Workers: workers, Handlers: ROMPipeline(identifier, nil, nil), Notifier: notifier}
}

type workerTestQueue struct {
	job         *jobsdomain.Job
	claimCount  atomic.Int32
	completed   atomic.Int32
	rescheduled atomic.Int32
	lastAttempt atomic.Uint32
}

func (*workerTestQueue) Enqueue(context.Context, jobsdomain.Payload) error { return nil }

func (queue *workerTestQueue) Claim(ctx context.Context, _ time.Time, _ time.Duration, _ jobsdomain.KindFilter) (*jobsdomain.Job, error) {
	if queue.claimCount.Add(1) == 1 {
		return queue.job, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (queue *workerTestQueue) Complete(context.Context, string) error {
	queue.completed.Add(1)
	return nil
}

func (queue *workerTestQueue) Reschedule(_ context.Context, _ string, attempt uint8, _ time.Time) error {
	queue.lastAttempt.Store(uint32(attempt))
	queue.rescheduled.Add(1)
	return nil
}
func (*workerTestQueue) DeadLetter(context.Context, jobsdomain.Job, uint8, string) error { return nil }
func (*workerTestQueue) ListDeadLetter(context.Context, int, *string) (jobsdomain.DeadLetterPage, error) {
	return jobsdomain.DeadLetterPage{}, nil
}
func (*workerTestQueue) RetryDeadLetter(context.Context, string) (bool, error) { return false, nil }

type workerTestIdentifier struct {
	started chan struct{}
	release <-chan struct{}
}

func (identifier workerTestIdentifier) Identify(ctx context.Context, _, _ string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	close(identifier.started)
	<-identifier.release
	return nil
}

type panickingIdentifier struct{}

func (panickingIdentifier) Identify(context.Context, string, string) error {
	panic("identify panic")
}

type parallelTestQueue struct {
	mu        sync.Mutex
	jobs      []jobsdomain.Job
	claims    atomic.Int32
	completed atomic.Int32
}

func (*parallelTestQueue) Enqueue(context.Context, jobsdomain.Payload) error { return nil }

func (queue *parallelTestQueue) Claim(ctx context.Context, _ time.Time, _ time.Duration, _ jobsdomain.KindFilter) (*jobsdomain.Job, error) {
	queue.mu.Lock()
	if len(queue.jobs) > 0 {
		job := queue.jobs[0]
		queue.jobs = queue.jobs[1:]
		queue.claims.Add(1)
		queue.mu.Unlock()
		return &job, nil
	}
	queue.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (queue *parallelTestQueue) Complete(context.Context, string) error {
	queue.completed.Add(1)
	return nil
}

func (*parallelTestQueue) Reschedule(context.Context, string, uint8, time.Time) error { return nil }
func (*parallelTestQueue) DeadLetter(context.Context, jobsdomain.Job, uint8, string) error {
	return nil
}
func (*parallelTestQueue) ListDeadLetter(context.Context, int, *string) (jobsdomain.DeadLetterPage, error) {
	return jobsdomain.DeadLetterPage{}, nil
}
func (*parallelTestQueue) RetryDeadLetter(context.Context, string) (bool, error) { return false, nil }

type parallelTestIdentifier struct {
	active  atomic.Int32
	maximum atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (identifier *parallelTestIdentifier) Identify(context.Context, string, string) error {
	active := identifier.active.Add(1)
	for current := identifier.maximum.Load(); active > current; current = identifier.maximum.Load() {
		if identifier.maximum.CompareAndSwap(current, active) {
			break
		}
	}
	identifier.started <- struct{}{}
	<-identifier.release
	identifier.active.Add(-1)
	return nil
}

func TestRunDrainsClaimedJobOnShutdown(t *testing.T) {
	queue := &workerTestQueue{job: &jobsdomain.Job{
		ID:      "7",
		Payload: jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-1", ROMFileID: "rf-1"},
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, queue, identifyPool(1, workerTestIdentifier{started: started, release: release}, nil))
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the claimed job")
	}
	cancel()
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after draining its claimed job")
	}
	if queue.completed.Load() != 1 {
		t.Fatalf("completed jobs = %d, want 1", queue.completed.Load())
	}
	if queue.claimCount.Load() != 1 {
		t.Fatalf("claim calls = %d, want 1", queue.claimCount.Load())
	}
}

func TestProcessJobReschedulesAfterIdentifierPanic(t *testing.T) {
	queue := &workerTestQueue{}
	job := jobsdomain.Job{ID: "job-1", Payload: jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-1", ROMFileID: "rf-1"}}

	processJob(context.Background(), queue, ROMPipeline(panickingIdentifier{}, queue, nil), job)

	if queue.rescheduled.Load() != 1 || queue.lastAttempt.Load() != 1 {
		t.Fatalf("reschedules=%d attempt=%d, want one reschedule at attempt 1", queue.rescheduled.Load(), queue.lastAttempt.Load())
	}
}

func TestRunProcessesJobsConcurrentlyWithinWorkerLimit(t *testing.T) {
	queue := &parallelTestQueue{jobs: []jobsdomain.Job{
		{ID: "job-1", Payload: jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-1", ROMFileID: "rf-1"}},
		{ID: "job-2", Payload: jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-2", ROMFileID: "rf-2"}},
	}}
	identifier := &parallelTestIdentifier{started: make(chan struct{}, 2), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, queue, identifyPool(2, identifier, nil))
	}()
	for range 2 {
		select {
		case <-identifier.started:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("both workers did not start jobs concurrently")
		}
	}
	if identifier.maximum.Load() != 2 {
		cancel()
		t.Fatalf("maximum active jobs = %d, want 2", identifier.maximum.Load())
	}
	cancel()
	close(identifier.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("workers did not join after shutdown")
	}
	if queue.claims.Load() != 2 || queue.completed.Load() != 2 {
		t.Fatalf("claims=%d completions=%d, want 2 each", queue.claims.Load(), queue.completed.Load())
	}
}

// wakeTestQueue starts empty and hands out jobs as they are enqueued.
type wakeTestQueue struct {
	workerTestQueue
	mu      sync.Mutex
	pending []jobsdomain.Job
	claims  atomic.Int32
}

func (queue *wakeTestQueue) Enqueue(_ context.Context, payload jobsdomain.Payload) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.pending = append(queue.pending, jobsdomain.Job{ID: payload.UploadID, Payload: payload})
	return nil
}

func (queue *wakeTestQueue) Claim(context.Context, time.Time, time.Duration, jobsdomain.KindFilter) (*jobsdomain.Job, error) {
	queue.claims.Add(1)
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.pending) == 0 {
		return nil, nil
	}
	job := queue.pending[0]
	queue.pending = queue.pending[1:]
	return &job, nil
}

type recordingIdentifier struct{ identified chan string }

func (identifier recordingIdentifier) Identify(_ context.Context, uploadID, _ string) error {
	identifier.identified <- uploadID
	return nil
}

func TestRunWakesIdleWorkerOnEnqueueAndBacksOffWhileIdle(t *testing.T) {
	notifier := NewNotifier()
	inner := &wakeTestQueue{}
	queue := NewNotifyingQueue(inner, notifier)
	identifier := recordingIdentifier{identified: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, queue, identifyPool(1, identifier, notifier)) }()

	// Idle waits grow 200ms, 400ms, 800ms...: after 700ms the worker is mid-way through an 800ms
	// wait, having claimed only a few times rather than every 200ms forever.
	time.Sleep(700 * time.Millisecond)
	if claims := inner.claims.Load(); claims > 4 {
		t.Fatalf("idle worker claimed %d times in 700ms, want backoff", claims)
	}
	enqueuedAt := time.Now()
	if err := queue.Enqueue(ctx, jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-1", ROMFileID: "rf-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case uploadID := <-identifier.identified:
		if uploadID != "upl-1" {
			t.Fatalf("identified %q", uploadID)
		}
		if waited := time.Since(enqueuedAt); waited > 300*time.Millisecond {
			t.Fatalf("job picked up after %v, want an immediate wake-up", waited)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueued job was not processed")
	}
}

func TestNotifierCoalescesAndToleratesNil(t *testing.T) {
	var absent *Notifier
	absent.Notify() // must not panic
	if absent.wake() != nil {
		t.Fatal("nil notifier must never wake")
	}
	notifier := NewNotifier()
	notifier.Notify()
	notifier.Notify() // coalesces instead of blocking
	<-notifier.wake()
	select {
	case <-notifier.wake():
		t.Fatal("coalesced notifications delivered twice")
	default:
	}
}

type recordingEnqueuer struct{ payloads []jobsdomain.Payload }

func (enqueuer *recordingEnqueuer) Enqueue(_ context.Context, payload jobsdomain.Payload) error {
	enqueuer.payloads = append(enqueuer.payloads, payload)
	return nil
}

type recordingScraper struct{ uploads []string }

func (scraper *recordingScraper) ScrapeUpload(_ context.Context, uploadID string) error {
	scraper.uploads = append(scraper.uploads, uploadID)
	return nil
}

func TestROMPipelineQueuesAScrapeOnceIdentified(t *testing.T) {
	enqueuer, scraper := &recordingEnqueuer{}, &recordingScraper{}
	handlers := ROMPipeline(recordingIdentifier{identified: make(chan string, 1)}, enqueuer, scraper)
	ctx := context.Background()

	identify := jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: "upl-1", ROMFileID: "rf-1"}
	if err := handlers[jobsdomain.KindIdentifyROM].Handle(ctx, identify); err != nil {
		t.Fatal(err)
	}
	want := jobsdomain.Payload{Kind: jobsdomain.KindScrapeROM, UploadID: "upl-1", ROMFileID: "rf-1"}
	if len(enqueuer.payloads) != 1 || enqueuer.payloads[0] != want {
		t.Fatalf("enqueued %+v, want %+v", enqueuer.payloads, want)
	}
	if err := handlers[jobsdomain.KindScrapeROM].Handle(ctx, want); err != nil || len(scraper.uploads) != 1 || scraper.uploads[0] != "upl-1" {
		t.Fatalf("scrape handler err=%v scraped=%v", err, scraper.uploads)
	}
	if handlers[jobsdomain.KindScrapeROM].RetryBase <= handlers[jobsdomain.KindIdentifyROM].RetryBase {
		t.Fatal("scrape retries should back off longer than identification retries")
	}

	identifyOnly := ROMPipeline(recordingIdentifier{identified: make(chan string, 1)}, enqueuer, nil)
	if _, ok := identifyOnly[jobsdomain.KindScrapeROM]; ok {
		t.Fatal("no scrape handler expected without a scraper")
	}
}

// filterRecordingQueue records the kind filter each claim used.
type filterRecordingQueue struct {
	workerTestQueue
	mu      sync.Mutex
	filters []jobsdomain.KindFilter
}

func (queue *filterRecordingQueue) Claim(ctx context.Context, _ time.Time, _ time.Duration, filter jobsdomain.KindFilter) (*jobsdomain.Job, error) {
	queue.mu.Lock()
	queue.filters = append(queue.filters, filter)
	queue.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunClaimsWithEachPoolsKindFilter(t *testing.T) {
	queue := &filterRecordingQueue{}
	scrapes := jobsdomain.KindFilter{Kinds: []string{jobsdomain.KindScrapeROM}}
	others := jobsdomain.KindFilter{Kinds: scrapes.Kinds, Exclude: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, queue, Pool{Name: "identify", Workers: 1, Kinds: others}, Pool{Name: "scrape", Workers: 1, Kinds: scrapes})
	}()
	deadline := time.After(time.Second)
	for {
		queue.mu.Lock()
		claims := len(queue.filters)
		queue.mu.Unlock()
		if claims == 2 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("saw %d claims, want one per pool", claims)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	excluded, included := 0, 0
	for _, filter := range queue.filters {
		if len(filter.Kinds) != 1 || filter.Kinds[0] != jobsdomain.KindScrapeROM {
			t.Fatalf("unexpected filter %+v", filter)
		}
		if filter.Exclude {
			excluded++
		} else {
			included++
		}
	}
	if excluded != 1 || included != 1 {
		t.Fatalf("filters = %+v, want one excluding and one selecting scrapes", queue.filters)
	}
}

func TestRunRejectsInvalidPools(t *testing.T) {
	if err := Run(context.Background(), &workerTestQueue{}); err == nil {
		t.Fatal("Run() without pools succeeded")
	}
	if err := Run(context.Background(), &workerTestQueue{}, Pool{Workers: 0}); err == nil {
		t.Fatal("Run() with an empty pool succeeded")
	}
}

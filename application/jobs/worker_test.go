package jobs

import (
	"context"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workerTestQueue struct {
	job         *jobsdomain.Job
	claimCount  atomic.Int32
	completed   atomic.Int32
	rescheduled atomic.Int32
	lastAttempt atomic.Uint32
}

func (*workerTestQueue) Enqueue(context.Context, jobsdomain.Payload) error { return nil }

func (queue *workerTestQueue) Claim(ctx context.Context, _ time.Time, _ time.Duration) (*jobsdomain.Job, error) {
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

func (queue *parallelTestQueue) Claim(ctx context.Context, _ time.Time, _ time.Duration) (*jobsdomain.Job, error) {
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
		Payload: jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-1", ROMFileID: "rf-1"},
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, 1, queue, workerTestIdentifier{started: started, release: release})
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
	job := jobsdomain.Job{ID: "job-1", Payload: jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-1", ROMFileID: "rf-1"}}

	processJob(context.Background(), queue, panickingIdentifier{}, job)

	if queue.rescheduled.Load() != 1 || queue.lastAttempt.Load() != 1 {
		t.Fatalf("reschedules=%d attempt=%d, want one reschedule at attempt 1", queue.rescheduled.Load(), queue.lastAttempt.Load())
	}
}

func TestRunProcessesJobsConcurrentlyWithinWorkerLimit(t *testing.T) {
	queue := &parallelTestQueue{jobs: []jobsdomain.Job{
		{ID: "job-1", Payload: jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-1", ROMFileID: "rf-1"}},
		{ID: "job-2", Payload: jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-2", ROMFileID: "rf-2"}},
	}}
	identifier := &parallelTestIdentifier{started: make(chan struct{}, 2), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, 2, queue, identifier)
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

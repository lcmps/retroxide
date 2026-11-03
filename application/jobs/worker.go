package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
)

const (
	maxAttempts    = uint8(3)
	maxWorkerCount = 64
	claimTimeout   = 5 * time.Minute
	// Idle workers poll at minIdleWait, doubling up to maxIdleWait while the queue stays empty.
	// The polling is only a fallback: Notifier wakes a worker as soon as a job is enqueued.
	minIdleWait  = 200 * time.Millisecond
	maxIdleWait  = 5 * time.Second
	errorBackoff = 500 * time.Millisecond
	// Retries wait RetryBase << attempt: 200ms then 400ms for identification, 1m then 2m for scrapes.
	identifyRetryBase = 100 * time.Millisecond
	scrapeRetryBase   = 30 * time.Second
)

// Handler processes one kind of job. A failed job is retried after RetryBase, doubled on each
// attempt, and dead-lettered once maxAttempts attempts have failed.
type Handler struct {
	Handle    func(context.Context, jobsdomain.Payload) error
	RetryBase time.Duration
}

// Handlers maps job kinds to their handlers. Jobs of a kind without a handler are dead-lettered.
type Handlers map[string]Handler

// Pool is a set of workers that claim only the job kinds Kinds selects, so slow jobs of one kind
// cannot hold up another kind handled by a different pool.
type Pool struct {
	Name     string
	Workers  int
	Kinds    jobsdomain.KindFilter
	Handlers Handlers
	// Notifier wakes the pool's idle workers when a job is enqueued; nil relies on polling alone.
	Notifier *Notifier
}

// Run processes queued jobs with every pool's workers until ctx is cancelled.
func Run(ctx context.Context, queue Queue, pools ...Pool) error {
	if queue == nil || len(pools) == 0 {
		return errors.New("a worker queue and at least one pool are required")
	}
	total := 0
	for _, pool := range pools {
		if pool.Workers < 1 || pool.Workers > maxWorkerCount {
			return fault.Validation("worker count must be between 1 and 64")
		}
		total += pool.Workers
	}

	var workers sync.WaitGroup
	workers.Add(total)
	for _, pool := range pools {
		for range pool.Workers {
			go func() {
				defer workers.Done()
				runWorker(ctx, queue, pool)
			}()
		}
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

// ROMPipeline returns the handlers that turn uploads into ROMs. With a scraper, each identified
// upload queues a scrape of its ROM, processed independently of further identifications.
func ROMPipeline(identifier Identifier, queue Enqueuer, scraper UploadScraper) Handlers {
	identify := func(ctx context.Context, payload jobsdomain.Payload) error {
		return identifier.Identify(ctx, payload.UploadID, payload.ROMFileID)
	}
	handlers := Handlers{jobsdomain.KindIdentifyROM: {Handle: identify, RetryBase: identifyRetryBase}}
	if scraper == nil {
		return handlers
	}
	handlers[jobsdomain.KindIdentifyROM] = Handler{
		Handle: func(ctx context.Context, payload jobsdomain.Payload) error {
			if err := identify(ctx, payload); err != nil {
				return err
			}
			// A retry after a failed enqueue is safe: identifying an identified upload is a no-op.
			return queue.Enqueue(ctx, jobsdomain.Payload{Kind: jobsdomain.KindScrapeROM, UploadID: payload.UploadID, ROMFileID: payload.ROMFileID})
		},
		RetryBase: identifyRetryBase,
	}
	handlers[jobsdomain.KindScrapeROM] = Handler{
		Handle: func(ctx context.Context, payload jobsdomain.Payload) error {
			return scraper.ScrapeUpload(ctx, payload.UploadID)
		},
		// Scrapes fail when metadata services are down, which rarely resolves within seconds.
		RetryBase: scrapeRetryBase,
	}
	return handlers
}

func runWorker(ctx context.Context, queue Queue, pool Pool) {
	idleWait := minIdleWait
	for ctx.Err() == nil {
		job, err := claimSafely(ctx, queue, pool.Kinds)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "job queue claim failed", "pool", pool.Name, "error", err)
			if !wait(ctx, errorBackoff) {
				return
			}
			continue
		}
		if job == nil {
			woken, ok := waitForWork(ctx, idleWait, pool.Notifier.wake())
			if !ok {
				return
			}
			if woken {
				idleWait = minIdleWait
			} else {
				idleWait = min(idleWait*2, maxIdleWait)
			}
			continue
		}
		idleWait = minIdleWait
		// More jobs may be queued behind this one; let an idle peer look while this worker is busy.
		pool.Notifier.Notify()
		processJob(context.WithoutCancel(ctx), queue, pool.Handlers, *job)
	}
}

func processJob(ctx context.Context, queue Queue, handlers Handlers, job jobsdomain.Job) {
	handler, ok := handlers[job.Payload.Kind]
	if !ok || handler.Handle == nil {
		reason := "unknown job kind: " + job.Payload.Kind
		if err := callSafely("dead-lettering unknown job", func() error {
			return queue.DeadLetter(ctx, job, job.Payload.Attempt, reason)
		}); err != nil {
			slog.ErrorContext(ctx, "failed to dead-letter unknown job", "job_id", job.ID, "error", err)
			return
		}
		completeJob(ctx, queue, job.ID)
		return
	}

	if err := callSafely("processing "+job.Payload.Kind+" job", func() error {
		return handler.Handle(ctx, job.Payload)
	}); err != nil {
		nextAttempt := job.Payload.Attempt + 1
		slog.WarnContext(ctx, "job failed", "kind", job.Payload.Kind, "upload_id", job.Payload.UploadID, "attempt", nextAttempt, "error", err)
		if nextAttempt >= maxAttempts {
			if deadLetterErr := callSafely("dead-lettering exhausted job", func() error {
				return queue.DeadLetter(ctx, job, nextAttempt, err.Error())
			}); deadLetterErr != nil {
				slog.ErrorContext(ctx, "failed to dead-letter exhausted job", "job_id", job.ID, "error", deadLetterErr)
				return
			}
			completeJob(ctx, queue, job.ID)
			return
		}
		backoff := handler.RetryBase << nextAttempt
		if rescheduleErr := callSafely("rescheduling job", func() error {
			return queue.Reschedule(ctx, job.ID, nextAttempt, time.Now().Add(backoff))
		}); rescheduleErr != nil {
			slog.ErrorContext(ctx, "failed to reschedule job", "job_id", job.ID, "error", rescheduleErr)
		}
		return
	}
	completeJob(ctx, queue, job.ID)
}

func completeJob(ctx context.Context, queue Queue, jobID string) {
	if err := callSafely("completing job", func() error { return queue.Complete(ctx, jobID) }); err != nil {
		slog.ErrorContext(ctx, "failed to complete job", "job_id", jobID, "error", err)
	}
}

func claimSafely(ctx context.Context, queue Queue, kinds jobsdomain.KindFilter) (*jobsdomain.Job, error) {
	var job *jobsdomain.Job
	err := callSafely("claiming job", func() error {
		var claimErr error
		job, claimErr = queue.Claim(ctx, time.Now(), claimTimeout, kinds)
		return claimErr
	})
	return job, err
}

func callSafely(operation string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s panicked: %v", operation, recovered)
		}
	}()
	return call()
}

// waitForWork sleeps for duration or until woken. ok is false once ctx is cancelled.
func waitForWork(ctx context.Context, duration time.Duration, wake <-chan struct{}) (woken, ok bool) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, false
	case <-wake:
		return true, true
	case <-timer.C:
		return false, true
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

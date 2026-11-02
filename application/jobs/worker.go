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
)

// Run processes queued jobs with workerCount workers until ctx is cancelled. notifier may be nil,
// in which case workers rely on polling alone.
func Run(ctx context.Context, workerCount int, queue Queue, identifier Identifier, notifier *Notifier) error {
	if workerCount < 1 || workerCount > maxWorkerCount {
		return fault.Validation("worker count must be between 1 and 64")
	}
	if queue == nil || identifier == nil {
		return errors.New("worker queue and identifier are required")
	}

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			runWorker(ctx, queue, identifier, notifier)
		}()
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

func runWorker(ctx context.Context, queue Queue, identifier Identifier, notifier *Notifier) {
	idleWait := minIdleWait
	for ctx.Err() == nil {
		job, err := claimSafely(ctx, queue)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "job queue claim failed", "error", err)
			if !wait(ctx, errorBackoff) {
				return
			}
			continue
		}
		if job == nil {
			woken, ok := waitForWork(ctx, idleWait, notifier.wake())
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
		notifier.Notify()
		processJob(context.WithoutCancel(ctx), queue, identifier, *job)
	}
}

func processJob(ctx context.Context, queue Queue, identifier Identifier, job jobsdomain.Job) {
	if job.Payload.Kind != "identify_rom" {
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

	if err := callSafely("identifying rom", func() error {
		return identifier.Identify(ctx, job.Payload.UploadID, job.Payload.ROMFileID)
	}); err != nil {
		nextAttempt := job.Payload.Attempt + 1
		slog.WarnContext(ctx, "identify job failed", "upload_id", job.Payload.UploadID, "attempt", nextAttempt, "error", err)
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
		backoff := time.Duration(1<<nextAttempt) * 100 * time.Millisecond
		if rescheduleErr := callSafely("rescheduling identify job", func() error {
			return queue.Reschedule(ctx, job.ID, nextAttempt, time.Now().Add(backoff))
		}); rescheduleErr != nil {
			slog.ErrorContext(ctx, "failed to reschedule identify job", "job_id", job.ID, "error", rescheduleErr)
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

func claimSafely(ctx context.Context, queue Queue) (*jobsdomain.Job, error) {
	var job *jobsdomain.Job
	err := callSafely("claiming job", func() error {
		var claimErr error
		job, claimErr = queue.Claim(ctx, time.Now(), claimTimeout)
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

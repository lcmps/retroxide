package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lcmps/retroxide/internal/shared/fault"
)

const (
	maxAttempts    = uint8(3)
	maxWorkerCount = 64
	claimTimeout   = 5 * time.Minute
	pollInterval   = 200 * time.Millisecond
	errorBackoff   = 500 * time.Millisecond
)

func Run(ctx context.Context, workerCount int, queue Queue, identifier Identifier) error {
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
			runWorker(ctx, queue, identifier)
		}()
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

func runWorker(ctx context.Context, queue Queue, identifier Identifier) {
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
			if !wait(ctx, pollInterval) {
				return
			}
			continue
		}

		processJob(context.WithoutCancel(ctx), queue, identifier, *job)
	}
}

func processJob(ctx context.Context, queue Queue, identifier Identifier, job Job) {
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

func claimSafely(ctx context.Context, queue Queue) (*Job, error) {
	var job *Job
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

package jobs

import (
	"context"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	"time"
)

type Enqueuer interface {
	Enqueue(context.Context, jobsdomain.Payload) error
}

type Queue interface {
	Enqueuer
	// Claim leases the oldest available job whose kind matches filter, or returns nil if none.
	Claim(ctx context.Context, now time.Time, claimTimeout time.Duration, filter jobsdomain.KindFilter) (*jobsdomain.Job, error)
	Complete(context.Context, string) error
	Reschedule(context.Context, string, uint8, time.Time) error
	DeadLetter(context.Context, jobsdomain.Job, uint8, string) error
	ListDeadLetter(context.Context, int, *string) (jobsdomain.DeadLetterPage, error)
	RetryDeadLetter(context.Context, string) (bool, error)
}

type Identifier interface {
	Identify(context.Context, string, string) error
}

type FilenameCompleter interface {
	CompleteWithFilename(context.Context, string, string) error
}

// UploadScraper fetches metadata for the ROM an upload created.
type UploadScraper interface {
	ScrapeUpload(ctx context.Context, uploadID string) error
}

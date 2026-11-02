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
	Claim(context.Context, time.Time, time.Duration) (*jobsdomain.Job, error)
	Complete(context.Context, string) error
	Reschedule(context.Context, string, uint8, time.Time) error
	DeadLetter(context.Context, jobsdomain.Job, uint8, string) error
	ListDeadLetter(context.Context, int, *string) (jobsdomain.DeadLetterPage, error)
	RetryDeadLetter(context.Context, string) (bool, error)
}

type Identifier interface {
	Identify(context.Context, string, string) error
}

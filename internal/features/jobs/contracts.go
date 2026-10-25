package jobs

import (
	"context"
	"time"
)

type Payload struct {
	Kind      string
	UploadID  string
	ROMFileID string
	Attempt   uint8
}

type Job struct {
	ID      string
	Payload Payload
}

type DeadLetterJob struct {
	Payload Payload
	Reason  string
}

type DeadLetterJobRecord struct {
	ID      string
	Payload Payload
	Reason  string
}

type DeadLetterPage struct {
	Item       []DeadLetterJobRecord
	NextCursor *string
}

type Enqueuer interface {
	Enqueue(context.Context, Payload) error
}

type Queue interface {
	Enqueuer
	Claim(context.Context, time.Time, time.Duration) (*Job, error)
	Complete(context.Context, string) error
	Reschedule(context.Context, string, uint8, time.Time) error
	DeadLetter(context.Context, Job, uint8, string) error
	ListDeadLetter(context.Context, int, *string) (DeadLetterPage, error)
	RetryDeadLetter(context.Context, string) (bool, error)
}

type Identifier interface {
	Identify(context.Context, string, string) error
}

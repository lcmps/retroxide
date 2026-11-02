package jobs

import (
	"context"

	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
)

// Notifier wakes an idle worker as soon as work may be available, so workers can back off
// their polling while the queue is empty without delaying newly enqueued jobs.
type Notifier struct {
	signal chan struct{}
}

func NewNotifier() *Notifier {
	return &Notifier{signal: make(chan struct{}, 1)}
}

// Notify wakes one waiting worker; it never blocks, and pending signals coalesce.
func (notifier *Notifier) Notify() {
	if notifier == nil {
		return
	}
	select {
	case notifier.signal <- struct{}{}:
	default:
	}
}

// wake returns the channel workers wait on; nil (never ready) for a nil Notifier.
func (notifier *Notifier) wake() <-chan struct{} {
	if notifier == nil {
		return nil
	}
	return notifier.signal
}

// NotifyingQueue wakes workers whenever a job is enqueued.
type NotifyingQueue struct {
	Queue
	notifier *Notifier
}

func NewNotifyingQueue(queue Queue, notifier *Notifier) *NotifyingQueue {
	return &NotifyingQueue{Queue: queue, notifier: notifier}
}

func (queue *NotifyingQueue) Enqueue(ctx context.Context, payload jobsdomain.Payload) error {
	if err := queue.Queue.Enqueue(ctx, payload); err != nil {
		return err
	}
	queue.notifier.Notify()
	return nil
}

func (queue *NotifyingQueue) RetryDeadLetter(ctx context.Context, deadLetterID string) (bool, error) {
	retried, err := queue.Queue.RetryDeadLetter(ctx, deadLetterID)
	if retried {
		queue.notifier.Notify()
	}
	return retried, err
}

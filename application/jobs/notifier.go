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

// NotifyingQueue wakes workers whenever a job is enqueued. It signals every pool's notifier: a
// pool that does not handle the job's kind finds nothing to claim and goes back to waiting.
type NotifyingQueue struct {
	Queue
	notifiers []*Notifier
}

func NewNotifyingQueue(queue Queue, notifiers ...*Notifier) *NotifyingQueue {
	return &NotifyingQueue{Queue: queue, notifiers: notifiers}
}

// Notify wakes the workers for jobs enqueued outside this queue, such as within a transaction.
func (queue *NotifyingQueue) Notify() {
	queue.notify()
}

func (queue *NotifyingQueue) notify() {
	for _, notifier := range queue.notifiers {
		notifier.Notify()
	}
}

func (queue *NotifyingQueue) Enqueue(ctx context.Context, payload jobsdomain.Payload) error {
	if err := queue.Queue.Enqueue(ctx, payload); err != nil {
		return err
	}
	queue.notify()
	return nil
}

func (queue *NotifyingQueue) RetryDeadLetter(ctx context.Context, deadLetterID string) (bool, error) {
	retried, err := queue.Queue.RetryDeadLetter(ctx, deadLetterID)
	if retried {
		queue.notify()
	}
	return retried, err
}

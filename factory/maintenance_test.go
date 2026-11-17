package factory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeBIOSList struct {
	changed bool
	err     error
}

func (list fakeBIOSList) Refresh(context.Context) (bool, error) { return list.changed, list.err }

type countingRechecker struct{ calls int }

func (rechecker *countingRechecker) RecheckUnrecognized(context.Context) (int, error) {
	rechecker.calls++
	return 1, nil
}

func TestRefreshBIOSListRechecksOnlyWhenTheListChanged(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name  string
		list  fakeBIOSList
		calls int
	}{
		{"changed", fakeBIOSList{changed: true}, 1},
		{"unchanged", fakeBIOSList{}, 0},
		{"failed", fakeBIOSList{err: errors.New("offline")}, 0},
	} {
		rechecker := &countingRechecker{}
		refreshBIOSList(test.list, rechecker)(context.Background(), logger)
		if rechecker.calls != test.calls {
			t.Errorf("%s: rechecked %d times, want %d", test.name, rechecker.calls, test.calls)
		}
	}
}

type countingOptimizer struct {
	calls int
	err   error
}

func (optimizer *countingOptimizer) Optimize(context.Context) error {
	optimizer.calls++
	return optimizer.err
}

func TestOptimizeDatabaseRunsAndSurvivesFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, optimizer := range []*countingOptimizer{{}, {err: errors.New("locked")}} {
		optimizeDatabase(optimizer)(context.Background(), logger)
		if optimizer.calls != 1 {
			t.Fatalf("Optimize called %d times, want 1", optimizer.calls)
		}
	}
}

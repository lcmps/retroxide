package factory

import (
	"context"
	"log/slog"
	"time"

	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const (
	// maintenanceDelay lets startup settle before the first round of maintenance.
	maintenanceDelay = time.Minute
	// maintenanceInterval is how often maintenance runs afterwards.
	maintenanceInterval = 6 * time.Hour
	// unreferencedGrace is how long a stored file may go unreferenced before it is removed. An
	// upload stores its file a moment before recording it, so recent files are left alone.
	unreferencedGrace = time.Hour
	// idleSessionAge is how long an upload sent in parts may go without a new part before it is
	// abandoned and its reserved space freed.
	idleSessionAge = 24 * time.Hour
)

// maintenanceTask is one periodic chore.
type maintenanceTask func(ctx context.Context, logger *slog.Logger)

// runMaintenance runs tasks after maintenanceDelay and then every maintenanceInterval, until
// ctx ends.
func runMaintenance(ctx context.Context, logger *slog.Logger, tasks ...maintenanceTask) {
	timer := time.NewTimer(maintenanceDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for _, task := range tasks {
			if ctx.Err() != nil {
				return
			}
			task(ctx, logger)
		}
		timer.Reset(maintenanceInterval)
	}
}

// discMaintainer indexes and compresses discs.
type discMaintainer interface {
	IndexPending(ctx context.Context) (int, error)
	CompressPending(ctx context.Context) (int, error)
}

// indexPendingFiles returns a task inspecting files stored before discs were recognized, so
// CHD images already in the library get their tracks indexed.
func indexPendingFiles(discs discMaintainer) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		indexed, err := discs.IndexPending(ctx)
		if err != nil {
			logger.Warn("could not inspect stored files for discs", "error", err, "inspected", indexed)
		} else if indexed > 0 {
			logger.Info("inspected stored files for discs", "files", indexed)
		}
	}
}

// partExpirer forgets disc files uploaded but never assembled.
type partExpirer interface {
	DeleteUploadPartsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

func expireUploadParts(parts partExpirer) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		if expired, err := parts.DeleteUploadPartsBefore(ctx, time.Now().Add(-idleSessionAge)); err != nil {
			logger.Warn("could not expire unassembled disc files", "error", err)
		} else if expired > 0 {
			logger.Info("expired unassembled disc files", "files", expired)
		}
	}
}

// runDiscCompression compresses discs kept as tracks: after startup, whenever a disc is
// uploaded, and with the other maintenance. One disc at a time, so it never takes over a
// small server.
func runDiscCompression(ctx context.Context, logger *slog.Logger, discs discMaintainer, uploaded <-chan struct{}) {
	timer := time.NewTimer(maintenanceDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(maintenanceInterval)
		case <-uploaded:
		}
		if compressed, err := discs.CompressPending(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("could not compress discs", "error", err)
		} else if compressed > 0 {
			logger.Info("compressed discs to CHD", "discs", compressed)
		}
	}
}

// sessionExpirer ends idle uploads sent in parts.
type sessionExpirer interface {
	ExpireSessions(ctx context.Context, before time.Time) (int, error)
}

// expireUploadSessions returns a task ending uploads sent in parts that have been idle for
// idleSessionAge, freeing the space reserved for them.
func expireUploadSessions(uploads sessionExpirer) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		expired, err := uploads.ExpireSessions(ctx, time.Now().Add(-idleSessionAge))
		if err != nil {
			logger.Warn("could not expire idle uploads", "error", err)
			return
		}
		if expired > 0 {
			logger.Info("expired idle uploads", "sessions", expired)
		}
	}
}

// sweepUnreferencedFiles returns a task removing stored files that no ROM or image references
// any more, such as a cover and its thumbnail after the cover was replaced, or a file whose
// deletion failed after its ROM was deleted.
func sweepUnreferencedFiles(repositories *persistence.Repositories, files *storage.Filesystem) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		removed, err := removeUnreferencedFiles(ctx, repositories, files, time.Now().Add(-unreferencedGrace))
		if err != nil {
			logger.Warn("could not sweep unreferenced files", "error", err, "removed", removed)
			return
		}
		if removed > 0 {
			logger.Info("removed unreferenced files", "files", removed)
		}
	}
}

// removeUnreferencedFiles removes stored files last written before cutoff that nothing
// references, checking references a batch at a time, and returns how many it removed.
func removeUnreferencedFiles(ctx context.Context, repositories *persistence.Repositories, files *storage.Filesystem, cutoff time.Time) (int, error) {
	removed := 0
	var batch []string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		referenced, err := repositories.ReferencedStoragePaths(ctx, batch)
		if err != nil {
			return err
		}
		for _, key := range batch {
			if !referenced[key] && files.Delete(ctx, key) == nil {
				removed++
			}
		}
		batch = batch[:0]
		return nil
	}
	err := files.WalkKeys(ctx, func(stored storage.StoredKey) error {
		if stored.Modified.After(cutoff) {
			return nil
		}
		batch = append(batch, stored.Key)
		if len(batch) < relocateBatchSize {
			return nil
		}
		return flush()
	})
	if err == nil {
		err = flush()
	}
	return removed, err
}

// biosListRefresher downloads a newer list of known BIOS files.
type biosListRefresher interface {
	Refresh(ctx context.Context) (changed bool, err error)
}

// biosRechecker looks unrecognized BIOS files up again.
type biosRechecker interface {
	RecheckUnrecognized(ctx context.Context) (int, error)
}

// refreshBIOSList returns a task refreshing the known BIOS list (at most daily; the list itself
// skips fresh copies) and, when it changed, recognizing BIOS files it did not know before.
func refreshBIOSList(list biosListRefresher, bioses biosRechecker) maintenanceTask {
	return func(ctx context.Context, logger *slog.Logger) {
		changed, err := list.Refresh(ctx)
		if err != nil {
			logger.Warn("could not refresh the list of known BIOS files", "error", err)
			return
		}
		if !changed {
			return
		}
		recognized, err := bioses.RecheckUnrecognized(ctx)
		if err != nil {
			logger.Warn("could not check BIOS files against the new list", "error", err, "recognized", recognized)
		} else {
			logger.Info("refreshed the list of known BIOS files", "recognized", recognized)
		}
	}
}

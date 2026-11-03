package factory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const (
	// staleStagedAge is how long a staged upload must sit unwritten before startup removes it.
	// Uploads write continuously, so only files abandoned by a crash or power cut are that old.
	staleStagedAge = 10 * time.Minute
	// stalePartialAge is how long a partial upload must sit unwritten before startup removes it
	// even if a session still names it; idle sessions expire well before that.
	stalePartialAge = 72 * time.Hour
	// relocateBatchSize is how many files move between database commits.
	relocateBatchSize = 500
)

func NewStorage(settings config.Config) *storage.Filesystem {
	return storage.New(settings.StorageRoot)
}

// prepareStorage clears uploads abandoned by a crash and moves files recorded by earlier
// versions (absolute paths in one flat folder) to storage keys in the sharded layout. Files
// move in batches, each recorded only after its moves are flushed to disk. A file that cannot
// be found keeps its old path and is reported; it does not stop startup.
func prepareStorage(ctx context.Context, logger *slog.Logger, repositories *persistence.Repositories, files *storage.Filesystem) error {
	if removed, err := files.SweepStaged(staleStagedAge, stalePartialAge); err != nil {
		logger.Warn("could not clear abandoned uploads", "error", err)
	} else if removed > 0 {
		logger.Info("cleared abandoned uploads", "files", removed)
	}

	paths, err := repositories.ListLegacyStoragePaths(ctx)
	if err != nil || len(paths) == 0 {
		return err
	}
	logger.Info("moving stored files to the sharded layout", "files", len(paths))
	moved, missing := 0, 0
	for start := 0; start < len(paths); start += relocateBatchSize {
		batch := paths[start:min(start+relocateBatchSize, len(paths))]
		keys, errs := files.Relocate(batch)
		for _, err := range errs {
			if errors.Is(err, storage.ErrMissingContent) {
				missing++
				logger.Warn("stored file is missing", "error", err)
				continue
			}
			return fmt.Errorf("moving stored files: %w", err)
		}
		if err := repositories.ReplaceStoragePaths(ctx, keys); err != nil {
			return err
		}
		moved += len(keys)
	}
	logger.Info("moved stored files to the sharded layout", "files", moved, "missing", missing)
	return nil
}

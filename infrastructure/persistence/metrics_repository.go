package persistence

import "context"

// MetricCounts are the library totals monitoring reads.
type MetricCounts struct {
	ROMs, ROMFiles, ROMBytes, Users    int64
	UploadSessions, UploadSessionBytes int64
	DeadLetters                        int64
	JobsQueued                         map[string]int64
}

// CountForMetrics reads the library totals, a handful of aggregate queries.
func (repositories *Repositories) CountForMetrics(ctx context.Context) (MetricCounts, error) {
	db := repositories.DB.WithContext(ctx)
	counts := MetricCounts{JobsQueued: map[string]int64{}}
	var files struct {
		Count int64 `gorm:"column:count"`
		Bytes int64 `gorm:"column:bytes"`
	}
	var sessions struct {
		Count int64 `gorm:"column:count"`
		Bytes int64 `gorm:"column:bytes"`
	}
	var queued []struct {
		Kind  string `gorm:"column:kind"`
		Count int64  `gorm:"column:count"`
	}
	for _, query := range []func() error{
		func() error { return db.Model(&ROMModel{}).Count(&counts.ROMs).Error },
		func() error { return db.Model(&UserModel{}).Count(&counts.Users).Error },
		func() error { return db.Model(&JobDeadLetterModel{}).Count(&counts.DeadLetters).Error },
		func() error {
			return db.Model(&ROMFileModel{}).Select("COUNT(*) AS count, COALESCE(SUM(size), 0) AS bytes").Scan(&files).Error
		},
		func() error {
			return db.Model(&UploadSessionModel{}).Select("COUNT(*) AS count, COALESCE(SUM(size), 0) AS bytes").Scan(&sessions).Error
		},
		func() error {
			return db.Model(&JobQueueModel{}).Select("kind, COUNT(*) AS count").Group("kind").Scan(&queued).Error
		},
	} {
		if err := query(); err != nil {
			return MetricCounts{}, databaseError("counting for metrics", err)
		}
	}
	counts.ROMFiles, counts.ROMBytes = files.Count, files.Bytes
	counts.UploadSessions, counts.UploadSessionBytes = sessions.Count, sessions.Bytes
	for _, row := range queued {
		counts.JobsQueued[row.Kind] = row.Count
	}
	return counts, nil
}

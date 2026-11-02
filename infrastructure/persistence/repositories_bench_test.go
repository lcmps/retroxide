package persistence

import (
	"context"
	"fmt"
	"github.com/lcmps/retroxide/application/rom"
	"path/filepath"
	"testing"
)

var benchmarkROMRows any

func BenchmarkListROMsWithFiles(b *testing.B) {
	for _, count := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("rows=%d/unfiltered", count), func(b *testing.B) {
			repositories := newBenchmarkRepositories(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rows, err := repositories.ListROMsWithFiles(context.Background(), 50, rom.ROMFilter{})
				if err != nil {
					b.Fatal(err)
				}
				benchmarkROMRows = rows
			}
		})
		b.Run(fmt.Sprintf("rows=%d/platform-filter", count), func(b *testing.B) {
			repositories := newBenchmarkRepositories(b, count)
			platform := "sNeS"
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rows, err := repositories.ListROMsWithFiles(context.Background(), 50, rom.ROMFilter{Platform: &platform})
				if err != nil {
					b.Fatal(err)
				}
				benchmarkROMRows = rows
			}
		})
	}
}

func newBenchmarkRepositories(b *testing.B, count int) *Repositories {
	b.Helper()
	db, err := OpenDatabase(context.Background(), "sqlite", filepath.Join(b.TempDir(), "benchmark.sqlite3"), "")
	if err != nil {
		b.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			b.Error(err)
		}
	})

	files := make([]ROMFileModel, count)
	roms := make([]ROMModel, count)
	for index := range count {
		files[index] = ROMFileModel{
			ID:          fmt.Sprintf("file-%08d", index),
			SHA256:      fmt.Sprintf("%064x", index+1),
			SHA1:        fmt.Sprintf("%040x", index+1),
			MD5:         fmt.Sprintf("%032x", index+1),
			CRC32:       fmt.Sprintf("%08x", index+1),
			Size:        1024,
			StoragePath: "/benchmark",
		}
		platform := "NES"
		if index%2 == 0 {
			platform = "SNES"
		}
		roms[index] = ROMModel{
			ID:           fmt.Sprintf("rom-%08d", index),
			ROMFileID:    files[index].ID,
			UploaderID:   "benchmark-user",
			Title:        fmt.Sprintf("Benchmark ROM %d", index),
			Platform:     platform,
			IdentifiedBy: "benchmark",
			Confidence:   100,
		}
	}
	if err := db.CreateInBatches(&files, 50).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.CreateInBatches(&roms, 50).Error; err != nil {
		b.Fatal(err)
	}
	return NewRepositories(db)
}

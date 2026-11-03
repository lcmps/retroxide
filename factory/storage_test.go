package factory

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func TestPrepareStorageMovesLegacyPathsToKeys(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	root := filepath.Join(directory, "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)

	// An earlier version stored absolute paths in one flat folder. The image is shared by two
	// columns, and one ROM's file has gone missing.
	romAddress, imageAddress, missingAddress := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	for _, address := range []string{romAddress, imageAddress} {
		if err := os.WriteFile(filepath.Join(root, address), []byte(address[:1]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".staged.123.tmp"), []byte("abandoned"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, _ := romdomain.NewROMHash(strings.Repeat("1", 64), strings.Repeat("2", 40), strings.Repeat("3", 32), "deadbeef")
	image := filepath.Join(root, imageAddress)
	for _, row := range []struct{ id, address string }{{"present", romAddress}, {"missing", missingAddress}} {
		file := romdomain.ROMFile{ID: "rf-" + row.id, Hash: hash, Size: 1, StoragePath: filepath.Join(root, row.address)}
		file.Hash.SHA256 = strings.Repeat(row.address[:1], 64)
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{ID: row.id, ROMFileID: file.ID, UploaderID: "u", Identity: romdomain.ROMIdentity{
			Title: row.id, Platform: "snes", IdentifiedBy: "test", CoverArtPath: &image, ScreenshotPath: &image,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	stale := filepath.Join(root, ".staged.123.tmp")
	if err := os.Chtimes(stale, timeAgo(t, staleStagedAge*2), timeAgo(t, staleStagedAge*2)); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	files := storage.New(root)
	for range 2 { // a second start finds nothing left to do
		if err := prepareStorage(ctx, logger, repositories, files); err != nil {
			t.Fatal(err)
		}
	}

	present, err := repositories.FindROMByID(ctx, "present")
	if err != nil || present == nil {
		t.Fatal(err)
	}
	imageKey := "bb/" + imageAddress
	if *present.Identity.CoverArtPath != imageKey || *present.Identity.ScreenshotPath != imageKey {
		t.Fatalf("image paths = %s, %s; want %s", *present.Identity.CoverArtPath, *present.Identity.ScreenshotPath, imageKey)
	}
	romFile, err := repositories.FindROMFileByID(ctx, "rf-present")
	if err != nil || romFile.StoragePath != "aa/"+romAddress {
		t.Fatalf("rom file path = %+v, %v", romFile, err)
	}
	if content, err := files.Read(ctx, romFile.StoragePath); err != nil || string(content) != "a" {
		t.Fatalf("relocated rom = %q, %v", content, err)
	}
	if missing, _ := repositories.FindROMFileByID(ctx, "rf-missing"); missing.StoragePath != filepath.Join(root, missingAddress) {
		t.Fatalf("missing file's path changed to %s", missing.StoragePath)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("abandoned upload kept")
	}
	if left, _ := repositories.ListLegacyStoragePaths(ctx); len(left) != 1 {
		t.Fatalf("legacy paths left = %v, want only the missing file", left)
	}
}

func timeAgo(t *testing.T, age time.Duration) time.Time {
	t.Helper()
	return time.Now().Add(-age)
}

func TestRemoveUnreferencedFilesKeepsReferencedAndRecentFiles(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)
	files := storage.New(filepath.Join(directory, "storage"))
	write := func(fill string, age time.Duration) string {
		key, err := files.Write(ctx, strings.Repeat(fill, 64), []byte(fill))
		if err != nil {
			t.Fatal(err)
		}
		modified := time.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(directory, "storage", filepath.FromSlash(key)), modified, modified); err != nil {
			t.Fatal(err)
		}
		return key
	}
	romKey, thumbKey := write("a", 2*time.Hour), write("b", 2*time.Hour)
	oldOrphan, newOrphan := write("c", 2*time.Hour), write("d", time.Minute)
	hash, _ := romdomain.NewROMHash(strings.Repeat("a", 64), strings.Repeat("2", 40), strings.Repeat("3", 32), "deadbeef")
	if _, err := repositories.UpsertROMFile(ctx, romdomain.ROMFile{ID: "rf", Hash: hash, Size: 1, StoragePath: romKey}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveROM(ctx, romdomain.ROM{ID: "rom", ROMFileID: "rf", UploaderID: "u", Identity: romdomain.ROMIdentity{
		Title: "t", Platform: "snes", IdentifiedBy: "test", CoverThumbPath: &thumbKey,
	}}); err != nil {
		t.Fatal(err)
	}
	// An upload in progress is not a stored key and is never touched.
	inProgress := filepath.Join(directory, "storage", ".staged.1.tmp")
	if err := os.WriteFile(inProgress, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := removeUnreferencedFiles(ctx, repositories, files, time.Now().Add(-unreferencedGrace))
	if err != nil || removed != 1 {
		t.Fatalf("removeUnreferencedFiles() = %d, %v; want 1", removed, err)
	}
	for key, want := range map[string]bool{romKey: true, thumbKey: true, oldOrphan: false, newOrphan: true} {
		if _, err := files.Read(ctx, key); (err == nil) != want {
			t.Errorf("%s kept = %v, want %v", key, err == nil, want)
		}
	}
	if _, err := os.Stat(inProgress); err != nil {
		t.Fatal("an upload in progress was removed")
	}
}

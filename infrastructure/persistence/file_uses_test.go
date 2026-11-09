package persistence

import (
	"testing"

	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func TestMigrationsCreateBIOSTable(t *testing.T) {
	repositories, _ := openTestRepositories(t)
	files := storage.New(t.TempDir())
	file := storedROMFile(t, repositories, files, "rf-bios", "BIOS bytes")
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_missing", ROMFileID: "rf-none", UploaderID: "owner", Platform: "ps2", Filename: "x.bin"}).Error; err == nil {
		t.Fatal("a BIOS row was stored for a file that does not exist")
	}
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_1", ROMFileID: file.ID, UploaderID: "owner", Platform: "ps2", Filename: "a.bin"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_2", ROMFileID: file.ID, UploaderID: "other", Platform: "ps2", Filename: "b.bin"}).Error; err == nil {
		t.Fatal("two BIOS rows were stored for the same file")
	}
}

func TestHardDeleteROMKeepsAFileABIOSUses(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	file := storedROMFile(t, repositories, files, "rf-shared", "shared bytes")
	game := romdomain.ROM{ID: "rom-shared", ROMFileID: file.ID, UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "Not really a game", Platform: "ps2", IdentifiedBy: "filename"}}
	if err := repositories.SaveROM(ctx, game); err != nil {
		t.Fatal(err)
	}
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_shared", ROMFileID: file.ID, UploaderID: "owner", Platform: "ps2", Filename: "bios.bin"}).Error; err != nil {
		t.Fatal(err)
	}
	var deleted []string
	if err := repositories.HardDeleteROM(ctx, game.ID, func(path string) error { deleted = append(deleted, path); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, path := range deleted {
		if path == file.StoragePath {
			t.Fatal("deleting the game removed the file its BIOS still uses")
		}
	}
	assertDeleteCount(t, repositories, &ROMFileModel{}, 1)
}

func TestDeleteUnusedROMFileKeepsAFileABIOSUses(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := storedROMFile(t, repositories, storage.New(t.TempDir()), "rf-bios-only", "BIOS only")
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_only", ROMFileID: file.ID, UploaderID: "owner", Platform: "ps2", Filename: "bios.bin"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repositories.DeleteUnusedROMFile(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &ROMFileModel{}, 1)
}

func TestCountBIOSByROMFileID(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := storedROMFile(t, repositories, storage.New(t.TempDir()), "rf-count", "count me")
	if count, err := repositories.CountBIOSByROMFileID(ctx, file.ID); err != nil || count != 0 {
		t.Fatalf("count before = %d, %v", count, err)
	}
	if err := repositories.DB.Create(&BIOSModel{ID: "bio_count", ROMFileID: file.ID, UploaderID: "owner", Platform: "ps2", Filename: "bios.bin"}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := repositories.CountBIOSByROMFileID(ctx, file.ID); err != nil || count != 1 {
		t.Fatalf("count after = %d, %v", count, err)
	}
}

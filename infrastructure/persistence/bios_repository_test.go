package persistence

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func seedBIOS(t *testing.T, repositories *Repositories, files *storage.Filesystem, id, uploader, platform, content string) biosdomain.BIOS {
	t.Helper()
	file := storedROMFile(t, repositories, files, "rf-"+id, content)
	path, region := "pcsx2/bios/"+id+".bin", "Europe"
	entry := biosdomain.BIOS{
		ID: id, ROMFileID: file.ID, UploaderID: uploader, Platform: platform, Filename: id + ".bin",
		ExpectedPath: &path, Recognized: true, Labels: biosdomain.Labels{Region: &region},
	}
	if err := repositories.SaveBIOS(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestSaveAndListBIOSByPlatform(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	seedBIOS(t, repositories, files, "bio_ps2", "owner", "ps2", "ps2 bios")
	seedBIOS(t, repositories, files, "bio_psx", "other", "psx", "psx bios")

	platform := "ps2"
	listed, err := repositories.ListBIOS(ctx, &platform)
	if err != nil || len(listed) != 1 || listed[0].ID != "bio_ps2" {
		t.Fatalf("ListBIOS(ps2) = %+v, %v", listed, err)
	}
	got := listed[0]
	if got.Labels.Region == nil || *got.Labels.Region != "Europe" || got.ExpectedPath == nil || !got.Recognized {
		t.Fatalf("labels not stored: %+v", got)
	}
	if got.Hash.SHA1 == "" || got.Size != uint64(len("ps2 bios")) {
		t.Fatalf("hash and size not read from the stored file: %+v", got)
	}
	all, err := repositories.ListBIOS(ctx, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListBIOS(nil) = %d entries, %v", len(all), err)
	}
	byFile, err := repositories.FindBIOSByROMFileID(ctx, got.ROMFileID)
	if err != nil || byFile == nil || byFile.ID != "bio_ps2" {
		t.Fatalf("FindBIOSByROMFileID = %+v, %v", byFile, err)
	}
	if missing, err := repositories.FindBIOS(ctx, "bio_missing"); err != nil || missing != nil {
		t.Fatalf("FindBIOS(missing) = %+v, %v", missing, err)
	}
}

func TestSaveBIOSRejectsSecondEntryForSameFile(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	first := seedBIOS(t, repositories, storage.New(t.TempDir()), "bio_first", "owner", "ps2", "same bytes")
	second := first
	second.ID = "bio_second"
	err := repositories.SaveBIOS(ctx, second)
	if !fault.IsCode(err, fault.CodeBIOSAlreadyExists) {
		t.Fatalf("SaveBIOS duplicate = %v, want bios_already_exists", err)
	}
}

func TestUpdateAndListUnrecognizedBIOS(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	entry := seedBIOS(t, repositories, storage.New(t.TempDir()), "bio_unknown", "owner", "ps2", "mystery")
	entry.Recognized, entry.ExpectedPath = false, nil
	model := "SCPH-39004"
	entry.Labels = biosdomain.Labels{Model: &model}
	if err := repositories.UpdateBIOS(ctx, entry); err != nil {
		t.Fatal(err)
	}
	unrecognized, err := repositories.ListUnrecognizedBIOS(ctx)
	if err != nil || len(unrecognized) != 1 || unrecognized[0].Labels.Model == nil || *unrecognized[0].Labels.Model != model || unrecognized[0].Labels.Region != nil {
		t.Fatalf("ListUnrecognizedBIOS = %+v, %v", unrecognized, err)
	}
	if unrecognized[0].Hash.SHA1 == "" {
		t.Fatal("unrecognized entries need their SHA-1 to be checked again")
	}
}

func TestListPlatformsIncludesBIOSOnlyPlatforms(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	game := romdomain.ROM{ID: "rom-snes", ROMFileID: storedROMFile(t, repositories, files, "rf-snes", "snes game").ID, UploaderID: "user-a",
		Identity: romdomain.ROMIdentity{Title: "Game", Platform: "snes", IdentifiedBy: "filename"}}
	if err := repositories.SaveROM(ctx, game); err != nil {
		t.Fatal(err)
	}
	seedBIOS(t, repositories, files, "bio_b", "user-b", "ps2", "ps2 bios")
	seedBIOS(t, repositories, files, "bio_snes", "user-b", "snes", "snes bios")

	userA := "user-a"
	counts, err := repositories.ListPlatforms(ctx, &userA)
	if err != nil {
		t.Fatal(err)
	}
	byPlatform := map[string]rom.PlatformCount{}
	for _, count := range counts {
		byPlatform[count.Platform] = count
	}
	if len(byPlatform) != 2 || byPlatform["snes"] != (rom.PlatformCount{Platform: "snes", Count: 1, BIOSCount: 1}) || byPlatform["ps2"] != (rom.PlatformCount{Platform: "ps2", Count: 0, BIOSCount: 1}) {
		t.Fatalf("ListPlatforms(user-a) = %+v", counts)
	}
}

func TestHardDeleteBIOSRemovesUnsharedFile(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	entry := seedBIOS(t, repositories, files, "bio_delete", "owner", "ps2", "delete me")
	file, err := repositories.FindROMFileByID(ctx, entry.ROMFileID)
	if err != nil || file == nil {
		t.Fatal(err)
	}
	var deleted []string
	if err := repositories.HardDeleteBIOS(ctx, entry.ID, func(path string) error { deleted = append(deleted, path); return nil }); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &BIOSModel{}, 0)
	assertDeleteCount(t, repositories, &ROMFileModel{}, 0)
	if len(deleted) != 1 || deleted[0] != file.StoragePath {
		t.Fatalf("deleted paths = %v, want %s", deleted, file.StoragePath)
	}
	if err := repositories.HardDeleteBIOS(ctx, entry.ID, func(string) error { return nil }); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("deleting again = %v, want not found", err)
	}
}

func TestHardDeleteBIOSKeepsFileAGameUses(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	entry := seedBIOS(t, repositories, files, "bio_shared", "owner", "ps2", "shared bytes")
	game := romdomain.ROM{ID: "rom-shared", ROMFileID: entry.ROMFileID, UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "Old upload", Platform: "ps2", IdentifiedBy: "filename"}}
	if err := repositories.SaveROM(ctx, game); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	if err := repositories.HardDeleteBIOS(ctx, entry.ID, func(path string) error { deleted = append(deleted, path); return nil }); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &ROMFileModel{}, 1)
	if len(deleted) != 0 {
		t.Fatalf("deleted %v while a game still uses the file", deleted)
	}
}

func TestDeleteUserTransfersBIOSToHeir(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, user := range []*UserModel{{ID: "leaver", Email: "leaver@example.com", PasswordHash: "x", Role: "user"}, {ID: "heir", Email: "heir@example.com", PasswordHash: "x", Role: "user"}} {
		if err := repositories.DB.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	entry := seedBIOS(t, repositories, storage.New(t.TempDir()), "bio_leaver", "leaver", "ps2", "leaver bios")
	if _, err := repositories.DeleteUserTransferringData(ctx, "leaver", "heir"); err != nil {
		t.Fatal(err)
	}
	moved, err := repositories.FindBIOS(ctx, entry.ID)
	if err != nil || moved == nil || moved.UploaderID != "heir" {
		t.Fatalf("BIOS after deleting its uploader = %+v, %v", moved, err)
	}
}

func TestBIOSNeverAppearInGameListings(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	seedBIOS(t, repositories, storage.New(t.TempDir()), "bio_hidden", "owner", "ps2", "hidden bios")
	search := "bio_hidden"
	for _, filter := range []rom.ROMFilter{{}, {Search: &search}} {
		rows, err := repositories.ListROMsWithFiles(ctx, 50, filter)
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListROMsWithFiles(%+v) = %d rows, %v", filter, len(rows), err)
		}
	}
}

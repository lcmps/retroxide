package persistence

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/rom"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

func seedDiscROM(t *testing.T, repositories *Repositories, ctx context.Context, id, uploader string, group *string) romdomain.ROM {
	t.Helper()
	file := validROMFile("file-"+id, id)
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	game := romdomain.ROM{ID: id, ROMFileID: file.ID, UploaderID: uploader, DiscGroup: group,
		Identity: romdomain.ROMIdentity{Title: "Game " + id, Platform: "psx", IdentifiedBy: "test", Confidence: 100}}
	if err := repositories.SaveROM(ctx, game); err != nil {
		t.Fatal(err)
	}
	return game
}

func seedDiscFile(t *testing.T, repositories *Repositories, ctx context.Context, id string, size uint64) romdomain.ROMFile {
	t.Helper()
	file := validROMFile(id, id)
	file.Size = size
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestMigrationGivesEveryExistingROMOneDisc(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	db := repositories.DB
	for _, table := range []string{"rom_disc"} {
		if err := db.Exec("DROP TABLE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("DROP INDEX idx_rom_uploader_disc_group").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE rom DROP COLUMN disc_group").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migration WHERE version = 24").Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"A", "B"} {
		file := seedDiscFile(t, repositories, ctx, "file-"+id, 1)
		if err := db.Exec("INSERT INTO rom (id, rom_file_id, uploader_id, title, platform, identified_by, confidence) VALUES (?, ?, 'u', 'T', 'psx', 'x', 1)", id, file.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, upload := range []UploadModel{
		{ID: "u1", UploaderID: "u", OriginalFilename: "Older.cue", Status: "completed", ROMID: ptr("A")},
		{ID: "u2", UploaderID: "u", OriginalFilename: "Newer.cue", Status: "completed", ROMID: ptr("A")},
	} {
		if err := db.Create(&upload).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := RunMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	a, err := repositories.FindROMDiscs(ctx, "A")
	if err != nil || len(a) != 1 || a[0] != (romdomain.ROMDisc{ROMID: "A", ROMFileID: "file-A", Number: 1, Filename: "Older.cue"}) {
		t.Fatalf("A discs = %+v, %v", a, err)
	}
	b, err := repositories.FindROMDiscs(ctx, "B")
	if err != nil || len(b) != 1 || b[0] != (romdomain.ROMDisc{ROMID: "B", ROMFileID: "file-B", Number: 1}) {
		t.Fatalf("B discs = %+v, %v", b, err)
	}
	for _, id := range []string{"A", "B"} {
		found, err := repositories.FindROMByID(ctx, id)
		if err != nil || found == nil || found.DiscGroup != nil {
			t.Fatalf("ROM %s = %+v, %v", id, found, err)
		}
	}
}

func ptr(value string) *string { return &value }

func TestSavingAROMKeepsItsDiscAndPrimary(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	group := "g"
	game := seedDiscROM(t, repositories, ctx, "r1", "u", &group)
	discs, err := repositories.FindROMDiscs(ctx, "r1")
	if err != nil || len(discs) != 1 || discs[0].Number != 1 || discs[0].ROMFileID != game.ROMFileID {
		t.Fatalf("discs = %+v, %v", discs, err)
	}
	stale, err := repositories.FindROMByID(ctx, "r1")
	if err != nil || stale == nil {
		t.Fatal(err)
	}
	lower := seedDiscFile(t, repositories, ctx, "file-lower", 5)
	// Disc 1 becomes disc 2, so the new file can take number 1 and become the primary.
	if err := repositories.DB.Exec("UPDATE rom_disc SET number = 2 WHERE rom_id = 'r1'").Error; err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: "r1", ROMFileID: lower.ID, Number: 1, Filename: "x"}); err != nil {
		t.Fatal(err)
	}
	stale.Identity.Title = "New Title"
	stale.DiscGroup = nil
	if err := repositories.SaveROM(ctx, *stale); err != nil {
		t.Fatal(err)
	}
	saved, err := repositories.FindROMByID(ctx, "r1")
	if err != nil || saved == nil {
		t.Fatal(err)
	}
	if saved.Identity.Title != "New Title" || saved.ROMFileID != lower.ID || saved.DiscGroup == nil || *saved.DiscGroup != "g" {
		t.Fatalf("saved = %+v", saved)
	}
	if discs, _ := repositories.FindROMDiscs(ctx, "r1"); len(discs) != 2 {
		t.Fatalf("discs = %+v", discs)
	}
}

func TestRecordDiscMakesTheLowestNumberPrimary(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	one := seedDiscFile(t, repositories, ctx, "disc-1", 1)
	two := seedDiscFile(t, repositories, ctx, "disc-2", 1)
	if err := repositories.DB.Exec("INSERT INTO rom (id, rom_file_id, uploader_id, title, platform, identified_by, confidence) VALUES ('r', ?, 'u', 'T', 'psx', 'x', 1)", two.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: "r", ROMFileID: two.ID, Number: 2, Filename: "d2.cue"}); err != nil {
		t.Fatal(err)
	}
	if game, _ := repositories.FindROMByID(ctx, "r"); game.ROMFileID != two.ID {
		t.Fatalf("primary = %s, want disc 2's file", game.ROMFileID)
	}
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: "r", ROMFileID: one.ID, Number: 1, Filename: "d1.cue"}); err != nil {
		t.Fatal(err)
	}
	if game, _ := repositories.FindROMByID(ctx, "r"); game.ROMFileID != one.ID {
		t.Fatalf("primary = %s, want disc 1's file", game.ROMFileID)
	}
	// Recording the same file again updates it in place.
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: "r", ROMFileID: one.ID, Number: 1, Filename: "renamed.cue"}); err != nil {
		t.Fatal(err)
	}
	discs, err := repositories.FindROMDiscs(ctx, "r")
	if err != nil || len(discs) != 2 || discs[0].Filename != "renamed.cue" || discs[1].Number != 2 {
		t.Fatalf("discs = %+v, %v", discs, err)
	}
}

func TestDiscGroupIsUniquePerUploader(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	group := "same"
	seedDiscROM(t, repositories, ctx, "g1", "alice", &group)
	file := seedDiscFile(t, repositories, ctx, "file-g2", 2)
	duplicate := romdomain.ROM{ID: "g2", ROMFileID: file.ID, UploaderID: "alice", DiscGroup: &group,
		Identity: romdomain.ROMIdentity{Title: "T", Platform: "psx", IdentifiedBy: "test"}}
	if err := repositories.SaveROM(ctx, duplicate); err == nil {
		t.Fatal("second ROM in the same group and uploader was accepted")
	}
	seedDiscROM(t, repositories, ctx, "g3", "bob", &group)
	seedDiscROM(t, repositories, ctx, "n1", "alice", nil)
	seedDiscROM(t, repositories, ctx, "n2", "alice", nil)
	found, err := repositories.FindROMByDiscGroup(ctx, "bob", group)
	if err != nil || found == nil || found.ID != "g3" {
		t.Fatalf("found = %+v, %v", found, err)
	}
	if missing, err := repositories.FindROMByDiscGroup(ctx, "carol", group); err != nil || missing != nil {
		t.Fatalf("missing = %+v, %v", missing, err)
	}
}

func addDisc(t *testing.T, repositories *Repositories, ctx context.Context, romID string, number int, size uint64) romdomain.ROMFile {
	t.Helper()
	file := seedDiscFile(t, repositories, ctx, romID+"-extra-"+string(rune('0'+number)), size)
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: romID, ROMFileID: file.ID, Number: number}); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestStorageTotalsCountEveryDisc(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	seedDiscROM(t, repositories, ctx, "s1", "u", nil)
	if err := repositories.DB.Exec("UPDATE rom_file SET size = 100 WHERE id = 'file-s1'").Error; err != nil {
		t.Fatal(err)
	}
	addDisc(t, repositories, ctx, "s1", 2, 200)
	activity, err := repositories.UserActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := activity["u"]; got.StorageBytes != 300 || got.ROMCount != 1 {
		t.Fatalf("activity = %+v", got)
	}
}

func TestListingsCountDiscsWithoutRepeatingTheROM(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	game := seedDiscROM(t, repositories, ctx, "l1", "u", nil)
	addDisc(t, repositories, ctx, "l1", 2, 1)
	addDisc(t, repositories, ctx, "l1", 3, 1)
	primary, err := repositories.FindROMFileByID(ctx, game.ROMFileID)
	if err != nil || primary == nil {
		t.Fatal(err)
	}
	listed, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{})
	if err != nil || len(listed) != 1 || listed[0].DiscCount != 3 || listed[0].SHA256 != primary.Hash.SHA256 {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	if err := repositories.SaveLibrary(ctx, librariesdomain.Library{ID: "lib", UserID: "u", Name: "L"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AddLibraryItem(ctx, "lib", "l1"); err != nil {
		t.Fatal(err)
	}
	inLibrary, err := repositories.ListLibraryROMs(ctx, "lib")
	if err != nil || len(inLibrary) != 1 || inLibrary[0].DiscCount != 3 || inLibrary[0].SHA256 != primary.Hash.SHA256 {
		t.Fatalf("library = %+v, %v", inLibrary, err)
	}
}

func TestAFileUsedAsALaterDiscIsNotUnused(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	seedDiscROM(t, repositories, ctx, "d1", "u", nil)
	later := addDisc(t, repositories, ctx, "d1", 2, 1)
	count, err := repositories.CountROMsByROMFileID(ctx, later.ID)
	if err != nil || count != 1 {
		t.Fatalf("count = %d, %v", count, err)
	}
	if err := repositories.DeleteUnusedROMFile(ctx, later.ID); err != nil {
		t.Fatal(err)
	}
	if kept, err := repositories.FindROMFileByID(ctx, later.ID); err != nil || kept == nil {
		t.Fatalf("file was deleted: %v", err)
	}
}

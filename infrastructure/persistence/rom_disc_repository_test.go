package persistence

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
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

// numbers returns a ROM's disc numbers keyed by file ID.
func (d *discDeletion) numbers(romID string) map[string]int {
	d.t.Helper()
	discs, err := d.repositories.FindROMDiscs(d.ctx, romID)
	if err != nil {
		d.t.Fatal(err)
	}
	numbers := map[string]int{}
	for _, disc := range discs {
		numbers[disc.ROMFileID] = disc.Number
	}
	return numbers
}

// library creates a library holding the ROMs.
func (d *discDeletion) library(name string, romIDs ...string) string {
	d.t.Helper()
	id := d.id("lib-" + name)
	if err := d.repositories.DB.Create(&LibraryModel{ID: id, UserID: "owner", Name: name, CreatedAt: "2000-01-01 00:00:00"}).Error; err != nil {
		d.t.Fatal(err)
	}
	for _, romID := range romIDs {
		if err := d.repositories.DB.Create(&LibraryItemModel{LibraryID: id, ROMID: romID}).Error; err != nil {
			d.t.Fatal(err)
		}
	}
	return id
}

func (d *discDeletion) assertValidation(err error) {
	d.t.Helper()
	var appError *fault.Error
	if !errors.As(err, &appError) || appError.Code != fault.CodeValidation {
		d.t.Fatalf("error = %v, want a validation error", err)
	}
}

func TestMergeMovesDiscsAndRemovesTheSource(t *testing.T) {
	d := openDiscDeletion(t)
	target := d.rom("target", "owner", 1)
	source := d.rom("source", "owner", 1)
	both := d.library("both", target.ID, source.ID)
	sourceOnly := d.library("source-only", source.ID)
	var deleted deletedPaths
	if err := d.repositories.MergeROMs(d.ctx, target.ID, source.ID, deleted.delete); err != nil {
		t.Fatal(err)
	}
	numbers := d.numbers(target.ID)
	if len(numbers) != 2 || numbers[target.ROMFileID] != 1 || numbers[source.ROMFileID] != 2 {
		t.Errorf("target discs = %v", numbers)
	}
	if d.count(&ROMModel{}, "id = ?", source.ID)+d.count(&ROMDiscModel{}, "rom_id = ?", source.ID) != 0 {
		t.Error("the source remains")
	}
	merged, err := d.repositories.FindROMByID(d.ctx, target.ID)
	if err != nil || merged == nil || merged.ROMFileID != target.ROMFileID || merged.Identity.Title != target.Identity.Title {
		t.Fatalf("target = %+v, %v", merged, err)
	}
	if d.count(&UploadModel{}, "rom_id = ?", target.ID) != 2 || d.count(&UploadModel{}, "rom_id = ?", source.ID) != 0 {
		t.Error("the source's upload did not move to the target")
	}
	if d.count(&LibraryItemModel{}, "library_id = ? AND rom_id = ?", both, target.ID) != 1 || d.count(&LibraryItemModel{}, "library_id = ? AND rom_id = ?", sourceOnly, target.ID) != 1 {
		t.Error("library entries were not merged onto the target")
	}
	if d.count(&LibraryItemModel{}, "rom_id = ?", source.ID) != 0 {
		t.Error("the source still has library entries")
	}
	d.assertFile(source.ROMFileID, 1)
	if !deleted.has("/covers/"+source.ID) || deleted.has("/covers/"+target.ID) || len(deleted) != 1 {
		t.Errorf("deleted = %v, want only the source's cover", deleted)
	}
}

func TestMergeRenumbersOnlyOnConflict(t *testing.T) {
	d := openDiscDeletion(t)
	target := d.rom("target", "owner", 1)
	source := d.rom("source", "owner", 3)
	// The source's disc 2 goes, leaving 1 and 3.
	if err := d.repositories.HardDeleteROMDisc(d.ctx, source.ID, d.id("source-disc2"), func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := d.repositories.MergeROMs(d.ctx, target.ID, source.ID, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	numbers := d.numbers(target.ID)
	if len(numbers) != 3 || numbers[target.ROMFileID] != 1 || numbers[source.ROMFileID] != 2 || numbers[d.id("source-disc3")] != 3 {
		t.Errorf("discs = %v, want target 1, source's 1 as 2, source's 3 as 3", numbers)
	}
}

func TestMergeDropsADiscBothHave(t *testing.T) {
	d := openDiscDeletion(t)
	target := d.rom("target", "owner", 1)
	source := d.rom("source", "owner", 1)
	if err := d.repositories.RecordDisc(d.ctx, romdomain.ROMDisc{ROMID: source.ID, ROMFileID: target.ROMFileID, Number: 2, Filename: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := d.repositories.MergeROMs(d.ctx, target.ID, source.ID, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	numbers := d.numbers(target.ID)
	if len(numbers) != 2 || numbers[target.ROMFileID] != 1 || numbers[source.ROMFileID] != 2 {
		t.Errorf("discs = %v", numbers)
	}
	d.assertFile(target.ROMFileID, 1)
}

func TestMergeAdoptsTheSourcesGroupOnlyIfTheTargetHasNone(t *testing.T) {
	d := openDiscDeletion(t)
	setGroup := func(romID string, group string) {
		if err := d.repositories.DB.Model(&ROMModel{}).Where("id = ?", romID).Update("disc_group", group).Error; err != nil {
			t.Fatal(err)
		}
	}
	groupOf := func(romID string) *string {
		found, err := d.repositories.FindROMByID(d.ctx, romID)
		if err != nil || found == nil {
			t.Fatalf("finding %s: %v", romID, err)
		}
		return found.DiscGroup
	}
	target, source := d.rom("target", "owner", 1), d.rom("source", "owner", 1)
	setGroup(source.ID, d.id("group-a"))
	if err := d.repositories.MergeROMs(d.ctx, target.ID, source.ID, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := groupOf(target.ID); got == nil || *got != d.id("group-a") {
		t.Errorf("target group = %v, want the source's", got)
	}
	keeper, other := d.rom("keeper", "owner", 1), d.rom("other", "owner", 1)
	setGroup(keeper.ID, d.id("group-k"))
	setGroup(other.ID, d.id("group-o"))
	if err := d.repositories.MergeROMs(d.ctx, keeper.ID, other.ID, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := groupOf(keeper.ID); got == nil || *got != d.id("group-k") {
		t.Errorf("keeper group = %v, want its own", got)
	}
}

func TestMergeRefusesMissingROMs(t *testing.T) {
	d := openDiscDeletion(t)
	target := d.rom("target", "owner", 1)
	for _, ids := range [][2]string{{target.ID, d.id("none")}, {d.id("none"), target.ID}} {
		err := d.repositories.MergeROMs(d.ctx, ids[0], ids[1], func(string) error { return nil })
		var appError *fault.Error
		if !errors.As(err, &appError) || appError.Code != fault.CodeNotFound {
			t.Errorf("MergeROMs(%v) = %v, want not found", ids, err)
		}
	}
	if d.count(&ROMModel{}, "id = ?", target.ID) != 1 {
		t.Error("the target is gone")
	}
}

func TestSplitMakesANewTitle(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 2)
	group := d.id("group")
	if err := d.repositories.DB.Model(&ROMModel{}).Where("id = ?", game.ID).Update("disc_group", group).Error; err != nil {
		t.Fatal(err)
	}
	library := d.library("shelf", game.ID)
	disc2 := d.id("title-disc2")
	newID := d.id("split")
	if err := d.repositories.SplitROMDisc(d.ctx, game.ID, disc2, newID); err != nil {
		t.Fatal(err)
	}
	split, err := d.repositories.FindROMByID(d.ctx, newID)
	if err != nil || split == nil {
		t.Fatalf("split = %v, %v", split, err)
	}
	if split.Identity.Title != game.Identity.Title || split.Identity.Platform != "psx" || split.Identity.CoverArtPath == nil || *split.Identity.CoverArtPath != "/covers/"+game.ID || split.UploaderID != "owner" || split.DiscGroup != nil || split.ROMFileID != disc2 {
		t.Errorf("split = %+v, want the old identity and cover, no group, file %s", split, disc2)
	}
	discs, err := d.repositories.FindROMDiscs(d.ctx, newID)
	if err != nil || len(discs) != 1 || discs[0].Number != 1 || discs[0].ROMFileID != disc2 || discs[0].Filename != "title disc 2.cue" {
		t.Errorf("split discs = %+v, %v", discs, err)
	}
	old, err := d.repositories.FindROMByID(d.ctx, game.ID)
	if err != nil || old == nil || old.ROMFileID != game.ROMFileID || old.DiscGroup == nil || *old.DiscGroup != group {
		t.Errorf("old ROM = %+v, %v", old, err)
	}
	if numbers := d.numbers(game.ID); len(numbers) != 1 || numbers[game.ROMFileID] != 1 {
		t.Errorf("old discs = %v", numbers)
	}
	if d.count(&UploadModel{}, "id = ? AND rom_id = ?", disc2+"-upload", newID) != 1 || d.count(&UploadModel{}, "rom_id = ?", game.ID) != 1 {
		t.Error("the disc's upload did not move to the new ROM alone")
	}
	if d.count(&LibraryItemModel{}, "library_id = ? AND rom_id = ?", library, newID) != 1 {
		t.Error("the new ROM is not in the old ROM's library")
	}
	d.assertFile(disc2, 1)
}

func TestDeletingASplitTitleKeepsTheSharedCover(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 2)
	newID := d.id("split")
	if err := d.repositories.SplitROMDisc(d.ctx, game.ID, d.id("title-disc2"), newID); err != nil {
		t.Fatal(err)
	}
	var deleted deletedPaths
	if err := d.repositories.HardDeleteROM(d.ctx, newID, deleted.delete); err != nil {
		t.Fatal(err)
	}
	if deleted.has("/covers/" + game.ID) {
		t.Errorf("the shared cover was deleted: %v", deleted)
	}
}

func TestSplitTheOnlyDiscIsRefused(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 1)
	d.assertValidation(d.repositories.SplitROMDisc(d.ctx, game.ID, game.ROMFileID, d.id("split")))
	if d.count(&ROMModel{}, "id = ?", d.id("split")) != 0 || d.count(&ROMDiscModel{}, "rom_id = ?", game.ID) != 1 {
		t.Error("something changed")
	}
}

func TestSplitLeavesAnotherDiscsUploadWithTheSameFilename(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 2)
	disc2 := d.id("title-disc2")
	// After a merge both discs came from uploads named game.iso.
	for _, fileID := range []string{game.ROMFileID, disc2} {
		if err := d.repositories.DB.Model(&ROMDiscModel{}).Where("rom_id = ? AND rom_file_id = ?", game.ID, fileID).Update("filename", "game.iso").Error; err != nil {
			t.Fatal(err)
		}
		if err := d.repositories.DB.Model(&UploadModel{}).Where("id = ?", fileID+"-upload").Update("original_filename", "game.iso").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.repositories.SplitROMDisc(d.ctx, game.ID, disc2, d.id("split")); err != nil {
		t.Fatal(err)
	}
	if d.count(&UploadModel{}, "rom_id = ?", game.ID) != 2 || d.count(&UploadModel{}, "rom_id = ?", d.id("split")) != 0 {
		t.Error("an ambiguous upload was repointed")
	}
}

func TestRemovingADiscKeepsAnotherDiscsUploadWithTheSameFilename(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 2)
	disc2 := d.id("title-disc2")
	for _, fileID := range []string{game.ROMFileID, disc2} {
		if err := d.repositories.DB.Model(&ROMDiscModel{}).Where("rom_id = ? AND rom_file_id = ?", game.ID, fileID).Update("filename", "game.iso").Error; err != nil {
			t.Fatal(err)
		}
		if err := d.repositories.DB.Model(&UploadModel{}).Where("id = ?", fileID+"-upload").Update("original_filename", "game.iso").Error; err != nil {
			t.Fatal(err)
		}
	}
	var deleted deletedPaths
	if err := d.repositories.HardDeleteROMDisc(d.ctx, game.ID, disc2, deleted.delete); err != nil {
		t.Fatal(err)
	}
	d.assertFile(disc2, 0)
	if d.count(&UploadModel{}, "rom_id = ?", game.ID) != 2 {
		t.Error("an upload was deleted though two discs shared its filename")
	}
}

func TestReorderRenumbersAndChangesPrimary(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 3)
	file1, file2, file3 := game.ROMFileID, d.id("title-disc2"), d.id("title-disc3")
	if err := d.repositories.ReorderROMDiscs(d.ctx, game.ID, []string{file2, file1, file3}); err != nil {
		t.Fatal(err)
	}
	numbers := d.numbers(game.ID)
	if numbers[file2] != 1 || numbers[file1] != 2 || numbers[file3] != 3 {
		t.Errorf("numbers = %v", numbers)
	}
	saved, err := d.repositories.FindROMByID(d.ctx, game.ID)
	if err != nil || saved == nil || saved.ROMFileID != file2 {
		t.Errorf("primary = %+v, %v; want %s", saved, err, file2)
	}
	discs, _ := d.repositories.FindROMDiscs(d.ctx, game.ID)
	if got := []string{discs[0].Filename, discs[1].Filename}; !slices.Equal(got, []string{"title disc 2.cue", "title disc 1.cue"}) {
		t.Errorf("filenames did not follow their discs: %v", got)
	}
}

func TestReorderMustNameEveryDisc(t *testing.T) {
	d := openDiscDeletion(t)
	game := d.rom("title", "owner", 2)
	file1, file2 := game.ROMFileID, d.id("title-disc2")
	for name, ids := range map[string][]string{
		"missing":  {file2},
		"unknown":  {file2, file1, d.id("nope")},
		"repeated": {file2, file2},
		"swapped":  {file2, d.id("nope")},
		"empty":    {},
	} {
		d.assertValidation(d.repositories.ReorderROMDiscs(d.ctx, game.ID, ids))
		if numbers := d.numbers(game.ID); numbers[file1] != 1 || numbers[file2] != 2 {
			t.Errorf("%s: numbers changed to %v", name, numbers)
		}
	}
}

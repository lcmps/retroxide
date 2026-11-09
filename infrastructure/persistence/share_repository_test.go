package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func shareHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func createLink(t *testing.T, repositories *Repositories, ctx context.Context, token, romID, by string) {
	t.Helper()
	if err := repositories.CreateShareLink(ctx, shareHash(token), romID, by); err != nil {
		t.Fatal(err)
	}
}

func TestRedeemCopiesTheROMWithEveryDisc(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	original := seedDiscROM(t, repositories, ctx, "alice-game", "alice", nil)
	second := addDisc(t, repositories, ctx, original.ID, 2, 64)
	createLink(t, repositories, ctx, "token-1", original.ID, "alice")

	copyID, err := repositories.RedeemShareLink(ctx, shareHash("token-1"), "bob", "rom_bob_copy")
	if err != nil || copyID != "rom_bob_copy" {
		t.Fatalf("RedeemShareLink = %q, %v", copyID, err)
	}
	copied, err := repositories.FindROMByID(ctx, copyID)
	if err != nil || copied == nil || copied.UploaderID != "bob" || copied.Identity.Title != original.Identity.Title || copied.Identity.Platform != "psx" || copied.ROMFileID != original.ROMFileID {
		t.Fatalf("copy = %+v, %v", copied, err)
	}
	discs, err := repositories.FindROMDiscs(ctx, copyID)
	if err != nil || len(discs) != 2 || discs[0].ROMFileID != original.ROMFileID || discs[1].ROMFileID != second.ID || discs[1].Number != 2 {
		t.Fatalf("copied discs = %+v, %v", discs, err)
	}
	if _, found, err := repositories.FindUnusedShareLink(ctx, shareHash("token-1")); err != nil || found {
		t.Fatalf("link after redeeming: found %v, %v", found, err)
	}
}

func TestRedeemIsSingleUse(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	original := seedDiscROM(t, repositories, ctx, "alice-game", "alice", nil)
	createLink(t, repositories, ctx, "once", original.ID, "alice")
	if _, err := repositories.RedeemShareLink(ctx, shareHash("once"), "bob", "rom_bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RedeemShareLink(ctx, shareHash("once"), "carol", "rom_carol"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("second redeem = %v, want not found", err)
	}
	if _, err := repositories.RedeemShareLink(ctx, shareHash("never-made"), "carol", "rom_carol"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("unknown link = %v, want not found", err)
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 2)
}

func TestRedeemWhenAlreadyInLibraryKeepsTheLink(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	original := seedDiscROM(t, repositories, ctx, "alice-game", "alice", nil)
	createLink(t, repositories, ctx, "kept", original.ID, "alice")
	mine := romdomain.ROM{ID: "bob-own", ROMFileID: original.ROMFileID, UploaderID: "bob", Identity: romdomain.ROMIdentity{Title: "Mine", Platform: "psx", IdentifiedBy: "test"}}
	if err := repositories.SaveROM(ctx, mine); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"bob", "alice"} {
		if _, err := repositories.RedeemShareLink(ctx, shareHash("kept"), user, "rom_"+user+"_copy"); !fault.IsCode(err, fault.CodeAlreadyInLibrary) {
			t.Fatalf("%s redeeming = %v, want already_in_library", user, err)
		}
	}
	if romID, found, err := repositories.FindUnusedShareLink(ctx, shareHash("kept")); err != nil || !found || romID != original.ID {
		t.Fatalf("link after refusals = %q, %v, %v", romID, found, err)
	}
	if has, err := repositories.UserHasROMFile(ctx, "bob", original.ROMFileID); err != nil || !has {
		t.Fatalf("UserHasROMFile(bob) = %v, %v", has, err)
	}
	if has, err := repositories.UserHasROMFile(ctx, "carol", original.ROMFileID); err != nil || has {
		t.Fatalf("UserHasROMFile(carol) = %v, %v", has, err)
	}
}

func TestRedeemDropsAClashingDiscGroup(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	group := "final-fantasy-vii"
	original := seedDiscROM(t, repositories, ctx, "alice-ff7", "alice", &group)
	seedDiscROM(t, repositories, ctx, "bob-other", "bob", &group)
	createLink(t, repositories, ctx, "to-bob", original.ID, "alice")
	createLink(t, repositories, ctx, "to-carol", original.ID, "alice")
	for user, wantGroup := range map[string]bool{"bob": false, "carol": true} {
		copyID, err := repositories.RedeemShareLink(ctx, shareHash("to-"+user), user, "rom_"+user)
		if err != nil {
			t.Fatal(err)
		}
		copied, _ := repositories.FindROMByID(ctx, copyID)
		if hasGroup := copied.DiscGroup != nil && *copied.DiscGroup == group; hasGroup != wantGroup {
			t.Errorf("%s's copy group = %v, want kept %v", user, copied.DiscGroup, wantGroup)
		}
	}
}

func TestDeletingTheOriginalKeepsTheCopy(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	original, paths := seedDeleteROM(t, repositories, files)
	createLink(t, repositories, ctx, "shared", original.ID, original.UploaderID)
	createLink(t, repositories, ctx, "unused", original.ID, original.UploaderID)
	if _, err := repositories.RedeemShareLink(ctx, shareHash("shared"), "bob", "rom_bob_copy"); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	if err := repositories.HardDeleteROM(ctx, original.ID, func(path string) error { deleted = append(deleted, path); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleting the original removed %v, which the copy still uses (%v)", deleted, paths)
	}
	copied, err := repositories.FindROMByID(ctx, "rom_bob_copy")
	if err != nil || copied == nil || copied.Identity.CoverArtPath == nil || *copied.Identity.CoverArtPath != paths[1] {
		t.Fatalf("copy after the original was deleted = %+v, %v", copied, err)
	}
	if file, err := repositories.FindROMFileByID(ctx, copied.ROMFileID); err != nil || file == nil {
		t.Fatalf("the copy's file = %+v, %v", file, err)
	}
	assertDeleteCount(t, repositories, &ROMShareLinkModel{}, 0) // every link of a deleted ROM goes with it
}

func TestUnusedLinkOfADeletedROMIsNotFound(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	original := seedDiscROM(t, repositories, ctx, "gone", "alice", nil)
	createLink(t, repositories, ctx, "orphan", original.ID, "alice")
	if err := repositories.HardDeleteROM(ctx, original.ID, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repositories.FindUnusedShareLink(ctx, shareHash("orphan")); err != nil || found {
		t.Fatalf("link of a deleted ROM: found %v, %v", found, err)
	}
	if _, err := repositories.RedeemShareLink(ctx, shareHash("orphan"), "bob", "rom_bob"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("redeeming it = %v, want not found", err)
	}
}

// exerciseShareLinks runs on any dialect with unique IDs, for shared MySQL databases.
func exerciseShareLinks(t *testing.T, ctx context.Context, repositories *Repositories, prefix string) {
	t.Helper()
	original := seedDiscROM(t, repositories, ctx, prefix+"-orig", prefix+"-alice", nil)
	addDisc(t, repositories, ctx, original.ID, 2, 32)
	createLink(t, repositories, ctx, prefix+"-token", original.ID, prefix+"-alice")
	copyID, err := repositories.RedeemShareLink(ctx, shareHash(prefix+"-token"), prefix+"-bob", prefix+"-copy")
	if err != nil {
		t.Fatal(err)
	}
	if discs, err := repositories.FindROMDiscs(ctx, copyID); err != nil || len(discs) != 2 {
		t.Fatalf("copied discs = %+v, %v", discs, err)
	}
	if _, err := repositories.RedeemShareLink(ctx, shareHash(prefix+"-token"), prefix+"-carol", prefix+"-copy2"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("second redeem = %v, want not found", err)
	}
}

func TestShareLinksOnSQLite(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	exerciseShareLinks(t, ctx, repositories, "sq")
}

package persistence

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"time"
)

func openTestRepositories(t *testing.T) (*Repositories, context.Context) {
	t.Helper()
	db, err := OpenDatabase(context.Background(), "sqlite", t.TempDir()+"/test.sqlite3", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}
	})
	return NewRepositories(db), context.Background()
}

func validROMFile(id, title string) romdomain.ROMFile {
	content := []byte(id + ":" + title)
	sha256Digest := sha256.Sum256(content)
	sha1Digest := sha1.Sum(content)
	md5Digest := md5.Sum(content)
	hash := hex.EncodeToString(sha256Digest[:])
	romHash, _ := romdomain.NewROMHash(hash, hex.EncodeToString(sha1Digest[:]), hex.EncodeToString(md5Digest[:]), "deadbeef")
	return romdomain.ROMFile{ID: id, Hash: romHash, Size: uint64(len(title)), StoragePath: "/storage/" + hash}
}

func TestRepositoriesRecognizeDuplicateWithLegacyCRC(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := validROMFile("rf-legacy", "payload")
	legacy := file
	legacy.Hash.CRC32 = "00000000"
	if _, err := repositories.UpsertROMFile(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	file.ID = "rf-reupload"
	if _, err := repositories.UpsertROMFile(ctx, file); !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("legacy duplicate error = %v, want rom_already_exists", err)
	}
}

func TestRepositoriesDetectDuplicateContentAndVerifyFullHash(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := validROMFile("rf-first", "payload")
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	duplicate := file
	duplicate.ID = "rf-second"
	if _, err := repositories.UpsertROMFile(ctx, duplicate); !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("duplicate insert error = %v, want rom_already_exists", err)
	}
	collision := duplicate
	collision.Hash.MD5 = strings.Repeat("d", 32)
	if _, err := repositories.UpsertROMFile(ctx, collision); !fault.IsCode(err, fault.CodeInternal) {
		t.Fatalf("secondary-hash mismatch error = %v, want internal collision error", err)
	}
}

func TestRepositoriesFilterPlatformByCaseInsensitiveExactMatch(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, row := range []struct {
		id       string
		platform string
	}{
		{id: "rom-1", platform: "SNES"},
		{id: "rom-2", platform: "snes_dev"},
	} {
		file := validROMFile("rf-"+row.id, row.id)
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{
			ID: row.id, ROMFileID: file.ID, UploaderID: "user-1",
			Identity: romdomain.ROMIdentity{Title: row.id, Platform: row.platform, IdentifiedBy: "test", Confidence: 100},
		}); err != nil {
			t.Fatal(err)
		}
	}
	platform := "sNeS"
	rows, err := repositories.ListROMsWithFiles(ctx, 10, nil, &platform, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ROM.ID != "rom-1" {
		t.Fatalf("platform-filtered ROMs = %+v, want only rom-1", rows)
	}
}

func TestRepositoriesFilterROMListByUploader(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, row := range []struct{ id, uploader string }{
		{id: "rom-1", uploader: "user-1"},
		{id: "rom-2", uploader: "user-2"},
	} {
		file := validROMFile("rf-"+row.id, row.id)
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{
			ID: row.id, ROMFileID: file.ID, UploaderID: row.uploader,
			Identity: romdomain.ROMIdentity{Title: row.id, Platform: "snes", IdentifiedBy: "test", Confidence: 100},
		}); err != nil {
			t.Fatal(err)
		}
	}
	uploader := "user-2"
	rows, err := repositories.ListROMsWithFiles(ctx, 10, nil, nil, &uploader)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ROM.ID != "rom-2" {
		t.Fatalf("uploader-filtered ROMs = %+v, want only rom-2", rows)
	}
	all, err := repositories.ListROMsWithFiles(ctx, 10, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered ROMs = %d, want 2", len(all))
	}
}

func TestRepositoriesLibraryMembershipIsIdempotent(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := validROMFile("rf-1", "payload")
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveROM(ctx, romdomain.ROM{ID: "rom-1", ROMFileID: file.ID, UploaderID: "user-1", Identity: romdomain.ROMIdentity{Title: "Game", Platform: "snes", IdentifiedBy: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveLibrary(ctx, librariesdomain.Library{ID: "lib-1", UserID: "user-1", Name: "collection"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repositories.AddLibraryItem(ctx, "lib-1", "rom-1"); err != nil {
			t.Fatal(err)
		}
	}
	romIDs, err := repositories.ListLibraryROMIDs(ctx, "lib-1")
	if err != nil || len(romIDs) != 1 || romIDs[0] != "rom-1" {
		t.Fatalf("library ROM IDs = %v, %v, want [rom-1]", romIDs, err)
	}
	removed, err := repositories.RemoveLibraryItem(ctx, "lib-1", "rom-1")
	if err != nil || !removed {
		t.Fatalf("first removal = %t, %v", removed, err)
	}
	removed, err = repositories.RemoveLibraryItem(ctx, "lib-1", "rom-1")
	if err != nil || removed {
		t.Fatalf("second removal = %t, %v, want false", removed, err)
	}
	romIDs, err = repositories.ListLibraryROMIDs(ctx, "lib-1")
	if err != nil || len(romIDs) != 0 {
		t.Fatalf("ROM IDs after removal = %v, %v, want empty", romIDs, err)
	}
}

func TestRepositoriesQueueClaimsJobOnlyOnceUntilLeaseExpires(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	if err := repositories.Enqueue(ctx, jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-1", ROMFileID: "rf-1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first, err := repositories.Claim(ctx, now, time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	second, err := repositories.Claim(ctx, now.Add(time.Second), time.Minute)
	if err != nil || second != nil {
		t.Fatalf("second claim = %+v, %v, want no available job", second, err)
	}
	reclaimed, err := repositories.Claim(ctx, now.Add(2*time.Minute), time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != first.ID {
		t.Fatalf("reclaim = %+v, %v, want expired lease job %q", reclaimed, err, first.ID)
	}
}

func TestRepositoriesRefreshRotationIsAtomicAndReplayRevokesReplacement(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	if err := repositories.SaveUser(ctx, authdomain.User{ID: "user-1", Email: "user@example.com", PasswordHash: "hash", Role: identity.RoleUser}); err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	old := authdomain.RefreshToken{JTI: "old-jti", UserID: "user-1", TokenHash: strings.Repeat("a", 64), CreatedAt: now, ExpiresAt: now + 3600}
	if err := repositories.Store(ctx, old); err != nil {
		t.Fatal(err)
	}
	replacement := authdomain.RefreshToken{JTI: "new-jti", UserID: "user-1", TokenHash: strings.Repeat("b", 64), CreatedAt: now, ExpiresAt: now + 3600}
	rotated, err := repositories.Rotate(ctx, old.JTI, replacement)
	if err != nil || !rotated {
		t.Fatalf("first rotation = %t, %v", rotated, err)
	}
	rotated, err = repositories.Rotate(ctx, old.JTI, authdomain.RefreshToken{JTI: "replay-jti", UserID: "user-1", TokenHash: strings.Repeat("c", 64), CreatedAt: now, ExpiresAt: now + 3600})
	if err != nil || rotated {
		t.Fatalf("replay rotation = %t, %v, want false", rotated, err)
	}
	storedReplacement, err := repositories.FindByJTI(ctx, replacement.JTI)
	if err != nil || storedReplacement == nil || !storedReplacement.Revoked {
		t.Fatalf("replacement after replay = %+v, %v, want revoked", storedReplacement, err)
	}
}

func TestRepositoriesIncrementTokenVersionAtomicallyAndPersistIt(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	if err := repositories.SaveUser(ctx, authdomain.User{ID: "user-1", Email: "user@example.com", PasswordHash: "hash", Role: identity.RoleUser}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repositories.IncrementTokenVersion(ctx, "user-1"); err != nil {
			t.Fatal(err)
		}
	}
	user, err := repositories.FindByID(ctx, "user-1")
	if err != nil || user == nil || user.TokenVersion != 2 {
		t.Fatalf("user after two increments = %+v, %v, want token version 2", user, err)
	}
	user.Email = "renamed@example.com"
	if err := repositories.SaveUser(ctx, *user); err != nil {
		t.Fatal(err)
	}
	if saved, _ := repositories.FindByID(ctx, "user-1"); saved.TokenVersion != 2 {
		t.Fatalf("SaveUser reset token version to %d", saved.TokenVersion)
	}
	if err := repositories.IncrementTokenVersion(ctx, "missing"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("increment for unknown user = %v, want not found", err)
	}
}

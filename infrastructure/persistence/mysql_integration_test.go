//go:build integration

package persistence

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"time"
)

func TestMySQLMigrationAndAtomicRepositories(t *testing.T) {
	mysqlURL := os.Getenv("RETROXIDE_TEST_MYSQL_URL")
	if mysqlURL == "" {
		t.Skip("RETROXIDE_TEST_MYSQL_URL is not set")
	}
	ctx := context.Background()
	db, err := OpenDatabase(ctx, "mysql", "", mysqlURL)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repositories := NewRepositories(db)
	userID := fmt.Sprintf("int-%x", time.Now().UnixNano())
	defer func() {
		if err := db.WithContext(ctx).Where("id = ?", userID).Delete(&UserModel{}).Error; err != nil {
			t.Errorf("cleaning integration user: %v", err)
		}
	}()
	if err := repositories.SaveUser(ctx, authdomain.User{ID: userID, Email: userID + "@example.test", PasswordHash: "integration-hash", Role: identity.RoleUser}); err != nil {
		t.Fatal(err)
	}
	content := []byte(userID)
	sha256Digest := sha256.Sum256(content)
	sha1Digest := sha1.Sum(content)
	md5Digest := md5.Sum(content)
	romFileHash, err := romdomain.NewROMHash(
		hex.EncodeToString(sha256Digest[:]), hex.EncodeToString(sha1Digest[:]), hex.EncodeToString(md5Digest[:]), "deadbeef",
	)
	if err != nil {
		t.Fatal(err)
	}
	romFile := romdomain.ROMFile{ID: "rf-" + userID, Hash: romFileHash, Size: 10, StoragePath: "/tmp/integration.rom"}
	if _, err := repositories.UpsertROMFile(ctx, romFile); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveROM(ctx, romdomain.ROM{
		ID: "rom-" + userID, ROMFileID: romFile.ID, UploaderID: userID,
		Identity: romdomain.ROMIdentity{Title: "Integration ROM", Platform: "SNES", IdentifiedBy: "integration", Confidence: 70},
	}); err != nil {
		t.Fatal(err)
	}
	platform := "snes"
	romRows, err := repositories.ListROMsWithFiles(ctx, 10, nil, &platform)
	if err != nil {
		t.Fatal(err)
	}
	foundROM := false
	for _, row := range romRows {
		if row.ROM.ID == "rom-"+userID && row.SHA256 == romFileHash.SHA256 {
			foundROM = true
		}
	}
	if !foundROM {
		t.Fatalf("case-insensitive platform query did not return inserted ROM: %+v", romRows)
	}
	libraryID := "lib-" + userID
	if err := repositories.SaveLibrary(ctx, librariesdomain.Library{ID: libraryID, UserID: userID, Name: "Integration library"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AddLibraryItem(ctx, libraryID, "rom-"+userID); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AddLibraryItem(ctx, libraryID, "rom-"+userID); err != nil {
		t.Fatal(err)
	}
	removed, err := repositories.RemoveLibraryItem(ctx, libraryID, "rom-"+userID)
	if err != nil || !removed {
		t.Fatalf("removing integration library item = %t, %v", removed, err)
	}
	if err := repositories.Enqueue(ctx, jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-" + userID, ROMFileID: romFile.ID}); err != nil {
		t.Fatal(err)
	}
	claimedJob, err := repositories.Claim(ctx, time.Now(), time.Minute)
	if err != nil || claimedJob == nil || claimedJob.Payload.ROMFileID != romFile.ID {
		t.Fatalf("claimed MySQL queue job = %+v, %v", claimedJob, err)
	}
	if err := repositories.Complete(ctx, claimedJob.ID); err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	oldToken := authdomain.RefreshToken{JTI: userID + "-old", UserID: userID, TokenHash: strings.Repeat("a", 64), CreatedAt: now, ExpiresAt: now + 3600}
	if err := repositories.Store(ctx, oldToken); err != nil {
		t.Fatal(err)
	}
	replacement := authdomain.RefreshToken{JTI: userID + "-new", UserID: userID, TokenHash: strings.Repeat("b", 64), CreatedAt: now, ExpiresAt: now + 3600}
	rotated, err := repositories.Rotate(ctx, oldToken.JTI, replacement)
	if err != nil || !rotated {
		t.Fatalf("initial rotation = %t, %v", rotated, err)
	}
	replayed := replacement
	replayed.JTI = userID + "-replay"
	replayed.TokenHash = strings.Repeat("c", 64)
	rotated, err = repositories.Rotate(ctx, oldToken.JTI, replayed)
	if err != nil || rotated {
		t.Fatalf("replayed rotation = %t, %v, want false", rotated, err)
	}
	storedReplacement, err := repositories.FindByJTI(ctx, replacement.JTI)
	if err != nil || storedReplacement == nil || !storedReplacement.Revoked {
		t.Fatalf("replacement after replay = %+v, %v, want revoked", storedReplacement, err)
	}
}

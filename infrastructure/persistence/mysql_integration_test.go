//go:build integration

package persistence

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/lcmps/retroxide/application/rom"
	"os"
	"slices"
	"strings"
	"testing"

	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
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
	romFile := romdomain.ROMFile{ID: "rf_" + romFileHash.SHA256, Hash: romFileHash, Size: 10, StoragePath: "/tmp/integration.rom"}
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
	romRows, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{Platform: &platform})
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
	claimedJob, err := repositories.Claim(ctx, time.Now(), time.Minute, jobsdomain.KindFilter{})
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

func TestMySQLROMFileIdentifierUpgrade(t *testing.T) {
	mysqlURL := os.Getenv("RETROXIDE_TEST_MYSQL_UPGRADE_URL")
	if mysqlURL == "" {
		t.Skip("RETROXIDE_TEST_MYSQL_UPGRADE_URL must point to an empty disposable database")
	}
	dsn, err := mysqlDSN(mysqlURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	var tables int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("upgrade test requires an empty disposable database")
	}
	if err := db.Exec("CREATE TABLE schema_migration (version INT UNSIGNED PRIMARY KEY, name VARCHAR(255) NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range mysqlMigrations {
		if item.version > 10 {
			break
		}
		for _, statement := range splitMigrationStatements(item.sql) {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Exec("INSERT INTO schema_migration (version, name) VALUES (?, ?)", item.version, item.name).Error; err != nil {
			t.Fatal(err)
		}
	}
	repositories := NewRepositories(db)
	legacy := validROMFile("legacy-file", "legacy content")
	if _, err := repositories.UpsertROMFile(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	// Inserted directly: the repository writes columns that later migrations add.
	if err := db.Exec("INSERT INTO rom (id, rom_file_id, uploader_id, title, platform, identified_by, confidence) VALUES ('legacy-rom', ?, 'system', 'Legacy', 'snes', '', 0)", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Initialize(context.Background(), db, "mysql"); err != nil {
			t.Fatal(err)
		}
	}
	if restored, err := repositories.FindROMByID(context.Background(), "legacy-rom"); err != nil || restored == nil || restored.ROMFileID != legacy.ID {
		t.Fatalf("legacy ROM after upgrade = %+v, %v", restored, err)
	}
	file := validROMFile("new-file", "new content")
	file.ID = "rf_" + file.Hash.SHA256
	if _, err := repositories.UpsertROMFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveROM(context.Background(), romdomain.ROM{ID: "new-rom", ROMFileID: file.ID, UploaderID: "system", Identity: romdomain.ROMIdentity{Title: "New", Platform: "snes"}}); err != nil {
		t.Fatal(err)
	}
	job := jobsdomain.Payload{Kind: "identify_rom", UploadID: "new-upload", ROMFileID: file.ID}
	if err := repositories.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	claimed, err := repositories.Claim(context.Background(), time.Now(), time.Minute, jobsdomain.KindFilter{})
	if err != nil || claimed == nil || claimed.Payload.ROMFileID != file.ID {
		t.Fatalf("claimed job = %+v, %v", claimed, err)
	}
	if err := repositories.DeadLetter(context.Background(), *claimed, 1, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO rom (id, rom_file_id, title, platform, identified_by, confidence) VALUES ('invalid-reference', 'missing', 'Invalid', 'snes', 'test', 1)").Error; err == nil {
		t.Fatal("ROM-file foreign key was not restored")
	}
}

func TestMySQLSearchUsesTheFullTextIndex(t *testing.T) {
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
	exerciseROMSearch(t, ctx, NewRepositories(db), fmt.Sprintf("search-%x", time.Now().UnixNano()))
}

func TestMySQLUserManagement(t *testing.T) {
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
	exerciseUserManagement(t, ctx, NewRepositories(db), fmt.Sprintf("um%x", time.Now().UnixNano()))
}

func TestMySQLListROMsInSortOrder(t *testing.T) {
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
	exerciseROMSorting(t, ctx, NewRepositories(db), fmt.Sprintf("sort-%x", time.Now().UnixNano()))
}

func TestMySQLUploadSessionsAndStoragePaths(t *testing.T) {
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
	exerciseUploadSessions(t, ctx, repositories, fmt.Sprintf("ups_%x", time.Now().UnixNano()))
	if counts, err := repositories.CountForMetrics(ctx); err != nil || counts.ROMFiles < 0 || counts.JobsQueued == nil {
		t.Fatalf("CountForMetrics() = %+v, %v", counts, err)
	}

	// Legacy paths are found and rewritten; keys and references are recognized.
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	file := validROMFile("rf-paths-"+suffix, "paths-"+suffix)
	legacy := "/srv/storage/" + file.Hash.SHA256
	file.StoragePath = legacy
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repositories.DeleteROMFile(ctx, file.ID) })
	paths, err := repositories.ListLegacyStoragePaths(ctx)
	if err != nil || !slices.Contains(paths, legacy) {
		t.Fatalf("legacy paths = %v, %v; want %s", paths, err, legacy)
	}
	key := file.Hash.SHA256[:2] + "/" + file.Hash.SHA256
	if err := repositories.ReplaceStoragePaths(ctx, map[string]string{legacy: key}); err != nil {
		t.Fatal(err)
	}
	if paths, _ := repositories.ListLegacyStoragePaths(ctx); slices.Contains(paths, legacy) || slices.Contains(paths, key) {
		t.Fatalf("legacy paths after rewriting = %v", paths)
	}
	referenced, err := repositories.ReferencedStoragePaths(ctx, []string{key, "zz/" + strings.Repeat("z", 64)})
	if err != nil || !referenced[key] || len(referenced) != 1 {
		t.Fatalf("referenced = %v, %v; want only %s", referenced, err, key)
	}
}

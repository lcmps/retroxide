package persistence

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/lcmps/retroxide/application/rom"
	"slices"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
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
	rows, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{Platform: &platform})
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
	rows, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{UploaderID: &uploader})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ROM.ID != "rom-2" {
		t.Fatalf("uploader-filtered ROMs = %+v, want only rom-2", rows)
	}
	all, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{})
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
	roms, err := repositories.ListLibraryROMs(ctx, "lib-1")
	if err != nil || len(roms) != 1 || roms[0].ROM.ID != "rom-1" || roms[0].SHA256 != file.Hash.SHA256 {
		t.Fatalf("library ROMs = %+v, %v, want rom-1 with its hash", roms, err)
	}
	removed, err := repositories.RemoveLibraryItem(ctx, "lib-1", "rom-1")
	if err != nil || !removed {
		t.Fatalf("first removal = %t, %v", removed, err)
	}
	removed, err = repositories.RemoveLibraryItem(ctx, "lib-1", "rom-1")
	if err != nil || removed {
		t.Fatalf("second removal = %t, %v, want false", removed, err)
	}
	roms, err = repositories.ListLibraryROMs(ctx, "lib-1")
	if err != nil || len(roms) != 0 {
		t.Fatalf("ROMs after removal = %+v, %v, want empty", roms, err)
	}
}

func TestRepositoriesQueueClaimsJobOnlyOnceUntilLeaseExpires(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	if err := repositories.Enqueue(ctx, jobsdomain.Payload{Kind: "identify_rom", UploadID: "upl-1", ROMFileID: "rf-1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first, err := repositories.Claim(ctx, now, time.Minute, jobsdomain.KindFilter{})
	if err != nil || first == nil {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	second, err := repositories.Claim(ctx, now.Add(time.Second), time.Minute, jobsdomain.KindFilter{})
	if err != nil || second != nil {
		t.Fatalf("second claim = %+v, %v, want no available job", second, err)
	}
	reclaimed, err := repositories.Claim(ctx, now.Add(2*time.Minute), time.Minute, jobsdomain.KindFilter{})
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

func TestRepositoriesListLibraryFiltersByOwnerInSQL(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, library := range []librariesdomain.Library{
		{ID: "lib-1", UserID: "user-1", Name: "One"},
		{ID: "lib-2", UserID: "user-2", Name: "Two"},
	} {
		if err := repositories.SaveLibrary(ctx, library); err != nil {
			t.Fatal(err)
		}
	}
	owner := "user-2"
	owned, err := repositories.ListLibrary(ctx, &owner)
	if err != nil || len(owned) != 1 || owned[0].ID != "lib-2" {
		t.Fatalf("owner-filtered libraries = %+v, %v, want only lib-2", owned, err)
	}
	all, err := repositories.ListLibrary(ctx, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered libraries = %+v, %v, want both", all, err)
	}
}

// queryPlan returns SQLite's plan for a statement, one detail line per step.
func queryPlan(t *testing.T, repositories *Repositories, ctx context.Context, sql string, args ...any) string {
	t.Helper()
	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := repositories.DB.WithContext(ctx).Raw("EXPLAIN QUERY PLAN "+sql, args...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	details := make([]string, 0, len(plan))
	for _, step := range plan {
		details = append(details, step.Detail)
	}
	return strings.Join(details, " / ")
}

func TestRepositoriesListingQueriesUseCompositeIndexesWithoutSorting(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	const listing = "SELECT r.id, f.sha256 FROM rom AS r JOIN rom_file AS f ON f.id = r.rom_file_id WHERE "
	for _, test := range []struct {
		name, where, index string
		args               []any
	}{
		{"owner", "r.uploader_id = ? ORDER BY r.id LIMIT 51", "idx_rom_uploader_listing", []any{"usr"}},
		{"owner and platform", "r.uploader_id = ? AND r.platform_key = ? ORDER BY r.id LIMIT 51", "idx_rom_uploader_platform", []any{"usr", "snes"}},
		{"platform", "r.platform_key = ? ORDER BY r.id LIMIT 51", "idx_rom_platform_listing", []any{"snes"}},
	} {
		plan := queryPlan(t, repositories, ctx, listing+test.where, test.args...)
		if !strings.Contains(plan, test.index) || strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s: plan %q should use %s with no sort step", test.name, plan, test.index)
		}
	}
	platforms := queryPlan(t, repositories, ctx, "SELECT MIN(platform), COUNT(*) FROM rom WHERE uploader_id = ? GROUP BY platform_key", "usr")
	if !strings.Contains(platforms, "idx_rom_uploader_platform") || strings.Contains(platforms, "TEMP B-TREE") {
		t.Errorf("platform counts plan %q should read only idx_rom_uploader_platform", platforms)
	}
	for _, column := range []string{"cover_art_path", "title_screen_path", "screenshot_path"} {
		if plan := queryPlan(t, repositories, ctx, "SELECT count(*) FROM rom WHERE "+column+" = ?", "/x"); strings.Contains(plan, "SCAN rom") {
			t.Errorf("blob reference check on %s scans the table: %q", column, plan)
		}
	}
	if plan := queryPlan(t, repositories, ctx, "SELECT count(*) FROM rom_file WHERE storage_path = ?", "/x"); strings.Contains(plan, "SCAN rom_file") {
		t.Errorf("storage path check scans the table: %q", plan)
	}
}

func TestRepositoriesSearchUsesTheTrigramIndex(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	plan := queryPlan(t, repositories, ctx,
		"SELECT r.id FROM rom AS r WHERE r.id IN (SELECT k.rom_id FROM rom_search AS s JOIN rom_search_key AS k ON k.id = s.rowid WHERE rom_search MATCH ?)", `"mario"`)
	if !strings.Contains(plan, "VIRTUAL TABLE INDEX") {
		t.Fatalf("search plan %q does not use the full-text index", plan)
	}
}

func TestRepositoriesStoringRefreshTokenPurgesTheUsersExpiredTokens(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, id := range []string{"user-1", "user-2"} {
		if err := repositories.SaveUser(ctx, authdomain.User{ID: id, Email: id + "@example.com", PasswordHash: "hash", Role: identity.RoleUser}); err != nil {
			t.Fatal(err)
		}
	}
	now := uint64(time.Now().Unix())
	token := func(jti, userID string, expiresAt uint64, revoked bool) authdomain.RefreshToken {
		return authdomain.RefreshToken{JTI: jti, UserID: userID, TokenHash: strings.Repeat(jti[len(jti)-1:], 64), CreatedAt: now - 7200, ExpiresAt: expiresAt, Revoked: revoked}
	}
	for _, seed := range []authdomain.RefreshToken{
		token("expired-1", "user-1", now-60, false),
		token("revoked-live-2", "user-1", now+3600, true),
		token("expired-other-3", "user-2", now-60, false),
	} {
		if err := insertRefreshToken(repositories.DB.WithContext(ctx), authdomain.RefreshToken{
			JTI: seed.JTI, UserID: seed.UserID, TokenHash: seed.TokenHash, CreatedAt: seed.CreatedAt, ExpiresAt: seed.ExpiresAt, Revoked: seed.Revoked,
		}); err != nil {
			t.Fatal(err)
		}
	}
	fresh := token("fresh-4", "user-1", now+3600, false)
	fresh.CreatedAt = now
	if err := repositories.Store(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	for jti, wantKept := range map[string]bool{"expired-1": false, "revoked-live-2": true, "expired-other-3": true, "fresh-4": true} {
		found, err := repositories.FindByJTI(ctx, jti)
		if err != nil {
			t.Fatal(err)
		}
		if (found != nil) != wantKept {
			t.Errorf("token %s kept = %t, want %t", jti, found != nil, wantKept)
		}
	}
}

func TestRepositoriesQueueClaimUsesTheAvailabilityIndex(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	err := repositories.DB.WithContext(ctx).
		Raw("EXPLAIN QUERY PLAN SELECT id FROM job_queue WHERE available_at <= ? AND (claimed_at IS NULL OR claimed_at <= ?)", "2026-01-01 00:00:00", "2026-01-01 00:00:00").
		Scan(&plan).Error
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan {
		if strings.Contains(step.Detail, "idx_job_queue_available_at") {
			return
		}
	}
	t.Fatalf("query plan %+v does not use idx_job_queue_available_at", plan)
}

func TestRepositoriesSearchMatchesTitleAndPlatformLiterally(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	exerciseROMSearch(t, ctx, repositories, "user-1")
}

// exerciseROMSearch checks search on any dialect: substrings of the title or platform match
// case-insensitively, LIKE wildcards are literal, and the full-text index follows renames and
// deletions. Rows are namespaced by owner so a shared database is fine.
func exerciseROMSearch(t *testing.T, ctx context.Context, repositories *Repositories, owner string) {
	t.Helper()
	ids := map[string]string{}
	for _, row := range []struct{ key, title, platform string }{
		{"mario", "Super Mario World", "SNES"},
		{"zelda", "Zelda: 100% Edition", "nes"},
		{"sonic", "Sonic_Pro", "genesis"},
		{"quote", `The "Quoted" Game`, "gba"},
	} {
		id := owner + "-rom-" + row.key
		ids[row.key] = id
		file := validROMFile(owner+"-rf-"+row.key, id)
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{
			ID: id, ROMFileID: file.ID, UploaderID: owner,
			Identity: romdomain.ROMIdentity{Title: row.title, Platform: row.platform, IdentifiedBy: "test", Confidence: 100},
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for key, id := range ids {
			_ = repositories.DeleteROM(ctx, id)
			_ = repositories.DeleteROMFile(ctx, owner+"-rf-"+key)
		}
	})
	search := func(term string) string {
		rows, err := repositories.ListROMsWithFiles(ctx, 10, rom.ROMFilter{Search: &term, UploaderID: &owner})
		if err != nil {
			t.Fatalf("search %q: %v", term, err)
		}
		found := make([]string, 0, len(rows))
		for _, row := range rows {
			found = append(found, strings.TrimPrefix(row.ROM.ID, owner+"-rom-"))
		}
		return strings.Join(found, ",")
	}
	for term, want := range map[string]string{
		"MARIO":        "mario", // title, case-insensitive
		"mario w":      "mario", // several words, matched as a phrase
		"world mario":  "",      // ... in order
		"snes":         "mario", // platform
		"zelda":        "zelda",
		"100%":         "zelda", // % is literal, not a wildcard
		"sonic_p":      "sonic", // _ is literal
		"o_p":          "",      // would match "o p"-like titles if _ were a wildcard
		"ze":           "zelda", // shorter than the index's terms: scanned instead
		`"quoted"`:     "quote", // quotes are searchable, not query syntax
		"nomatch":      "",
		owner + "-rom": "", // IDs are no longer searched
	} {
		if got := search(term); got != want {
			t.Errorf("search %q = %q, want %q", term, got, want)
		}
	}

	// The index follows renames and deletions.
	renamed := romdomain.ROM{ID: ids["mario"], ROMFileID: owner + "-rf-mario", UploaderID: owner,
		Identity: romdomain.ROMIdentity{Title: "Yoshi's Island", Platform: "SNES", IdentifiedBy: "test", Confidence: 100}}
	if err := repositories.SaveROM(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	if got := search("mario"); got != "" {
		t.Errorf("old title still found after rename: %q", got)
	}
	if got := search("yoshi"); got != "mario" {
		t.Errorf("new title search = %q, want mario", got)
	}
	if err := repositories.DeleteROM(ctx, ids["zelda"]); err != nil {
		t.Fatal(err)
	}
	if got := search("zelda"); got != "" {
		t.Errorf("deleted ROM still found: %q", got)
	}
}

func TestRepositoriesFindUploadedFilenameByROMUsesTheIndex(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	romID := "rom-1"
	if err := repositories.DB.WithContext(ctx).Create(&UploadModel{
		ID: "upload-1", UploaderID: "user-1", OriginalFilename: "Zelda (USA).nes", Status: "completed", ROMID: &romID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	filename, err := repositories.FindUploadedFilename(ctx, romID)
	if err != nil || filename == nil || *filename != "Zelda (USA).nes" {
		t.Fatalf("FindUploadedFilename() = %v, %v", filename, err)
	}
	if missing, err := repositories.FindUploadedFilename(ctx, "rom-unknown"); err != nil || missing != nil {
		t.Fatalf("FindUploadedFilename(unknown) = %v, %v; want nil, nil", missing, err)
	}

	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := repositories.DB.WithContext(ctx).
		Raw("EXPLAIN QUERY PLAN SELECT original_filename FROM upload WHERE rom_id = ?", romID).
		Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range plan {
		if strings.Contains(step.Detail, "idx_upload_rom_id") {
			return
		}
	}
	t.Fatalf("query plan %+v does not use idx_upload_rom_id", plan)
}

func TestRepositoriesClaimHonoursTheKindFilter(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	for _, kind := range []string{jobsdomain.KindScrapeROM, jobsdomain.KindIdentifyROM} {
		if err := repositories.Enqueue(ctx, jobsdomain.Payload{Kind: kind, UploadID: "upl-" + kind, ROMFileID: "rf-1"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Add(time.Second)
	scrapes := []string{jobsdomain.KindScrapeROM}

	other, err := repositories.Claim(ctx, now, time.Minute, jobsdomain.KindFilter{Kinds: scrapes, Exclude: true})
	if err != nil || other == nil || other.Payload.Kind != jobsdomain.KindIdentifyROM {
		t.Fatalf("excluding scrapes claimed %+v, %v; want the identify job despite the older scrape", other, err)
	}
	scrape, err := repositories.Claim(ctx, now, time.Minute, jobsdomain.KindFilter{Kinds: scrapes})
	if err != nil || scrape == nil || scrape.Payload.Kind != jobsdomain.KindScrapeROM {
		t.Fatalf("selecting scrapes claimed %+v, %v", scrape, err)
	}
	if none, err := repositories.Claim(ctx, now, time.Minute, jobsdomain.KindFilter{}); err != nil || none != nil {
		t.Fatalf("claim with every job leased = %+v, %v; want nothing", none, err)
	}
}

func TestRepositoriesListPlatformsGroupsCaseInsensitivelyPerUploader(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file, err := repositories.UpsertROMFile(ctx, validROMFile("rf-1", "platforms"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rom := range []romdomain.ROM{
		{ID: "rom-1", ROMFileID: file.ID, UploaderID: "user-1", Identity: romdomain.ROMIdentity{Title: "A", Platform: "sms"}},
		{ID: "rom-2", ROMFileID: file.ID, UploaderID: "user-1", Identity: romdomain.ROMIdentity{Title: "B", Platform: "SMS"}},
		{ID: "rom-3", ROMFileID: file.ID, UploaderID: "user-1", Identity: romdomain.ROMIdentity{Title: "C", Platform: "Sega Pico"}},
		{ID: "rom-4", ROMFileID: file.ID, UploaderID: "user-2", Identity: romdomain.ROMIdentity{Title: "D", Platform: "vita"}},
	} {
		if err := repositories.SaveROM(ctx, rom); err != nil {
			t.Fatal(err)
		}
	}
	userID := "user-1"
	counts, err := repositories.ListPlatforms(ctx, &userID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint64{}
	for _, count := range counts {
		got[strings.ToLower(count.Platform)] = count.Count
	}
	if len(got) != 2 || got["sms"] != 2 || got["sega pico"] != 1 {
		t.Fatalf("ListPlatforms(user-1) = %+v", counts)
	}
	all, err := repositories.ListPlatforms(ctx, nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListPlatforms(nil) = %+v, %v; want 3 platforms", all, err)
	}

	romID := "rom-1"
	if err := repositories.DB.WithContext(ctx).Create(&UploadModel{ID: "upl-1", UploaderID: "user-1", OriginalFilename: "a.sms", Status: "completed", ROMID: &romID}).Error; err != nil {
		t.Fatal(err)
	}
	if found, err := repositories.FindROMIDByUploadID(ctx, "upl-1"); err != nil || found == nil || *found != romID {
		t.Fatalf("FindROMIDByUploadID() = %v, %v", found, err)
	}
	if missing, err := repositories.FindROMIDByUploadID(ctx, "upl-missing"); err != nil || missing != nil {
		t.Fatalf("FindROMIDByUploadID(missing) = %v, %v", missing, err)
	}
}

func TestRepositoriesPersistTheUsersLocale(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	locale, font := "pt-BR", "system"
	user := authdomain.User{ID: "usr-1", Email: "locale@example.com", PasswordHash: "hash", Role: identity.RoleUser, Locale: &locale, Font: &font}
	if err := repositories.SaveUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	found, err := repositories.FindByID(ctx, "usr-1")
	if err != nil || found == nil || found.Locale == nil || *found.Locale != "pt-BR" || found.Font == nil || *found.Font != "system" {
		t.Fatalf("FindByID() = %+v, %v; want the locale and font persisted", found, err)
	}
	found.Locale = nil
	if err := repositories.SaveUser(ctx, *found); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := repositories.FindByID(ctx, "usr-1"); cleared == nil || cleared.Locale != nil {
		t.Fatalf("cleared locale = %+v, want nil", cleared)
	}
}

func TestRepositoriesSaveUploadWithJobIsAtomic(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	upload := uploadsdomain.Upload{ID: "upl-atomic", UploaderID: "user-1", OriginalFilename: "a.nes", Status: uploadsdomain.UploadStatusProcessing}
	job := jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: upload.ID, ROMFileID: "rf-1"}
	if err := repositories.SaveUploadWithJob(ctx, upload, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := repositories.Claim(ctx, time.Now().Add(time.Second), time.Minute, jobsdomain.KindFilter{})
	if err != nil || claimed == nil || claimed.Payload.UploadID != upload.ID {
		t.Fatalf("claimed job = %+v, %v", claimed, err)
	}

	// Make the job insert fail: the upload written earlier in the transaction must roll back.
	if err := repositories.DB.Exec("CREATE TRIGGER fail_job BEFORE INSERT ON job_queue BEGIN SELECT RAISE(ABORT, 'job insert failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	second := upload
	second.ID = "upl-rolled-back"
	if err := repositories.SaveUploadWithJob(ctx, second, jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: second.ID, ROMFileID: "rf-1"}); err == nil {
		t.Fatal("SaveUploadWithJob() succeeded although the job insert failed")
	}
	if found, err := repositories.FindUpload(ctx, second.ID); err != nil || found != nil {
		t.Fatalf("upload after a failed job insert = %+v, %v; want it rolled back", found, err)
	}
}

func TestRepositoriesUserManagement(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	exerciseUserManagement(t, ctx, repositories, "um")
}

// exerciseUserManagement checks usernames, per-user activity, and delete-with-transfer on any
// dialect; prefix namespaces rows so a shared database is fine.
func exerciseUserManagement(t *testing.T, ctx context.Context, repositories *Repositories, prefix string) {
	t.Helper()
	heirID, leaverID := prefix+"-heir", prefix+"-leaver"
	username := prefix + "-player"
	for _, account := range []authdomain.User{
		{ID: heirID, Email: heirID + "@example.test", PasswordHash: "h", Role: identity.RoleAdmin},
		{ID: leaverID, Email: leaverID + "@example.test", Username: &username, PasswordHash: "h", Role: identity.RoleUser},
	} {
		if err := repositories.SaveUser(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		repositories.DB.Where("uploader_id IN ?", []string{heirID, leaverID}).Delete(&ROMModel{})
		repositories.DB.Where("id LIKE ?", prefix+"-rf-%").Delete(&ROMFileModel{})
		repositories.DB.Where("user_id IN ?", []string{heirID, leaverID}).Delete(&LibraryModel{})
		repositories.DB.Where("uploader_id IN ?", []string{heirID, leaverID}).Delete(&UploadModel{})
		repositories.DB.Where("id IN ?", []string{heirID, leaverID}).Delete(&UserModel{})
	})

	// Usernames are found case-insensitively and must be unique.
	if found, err := repositories.FindByUsername(ctx, strings.ToUpper(username)); err != nil || found == nil || found.ID != leaverID {
		t.Fatalf("FindByUsername() = %+v, %v", found, err)
	}
	clash := authdomain.User{ID: prefix + "-clash", Email: prefix + "-clash@example.test", Username: &username, PasswordHash: "h", Role: identity.RoleUser}
	if err := repositories.SaveUser(ctx, clash); !fault.IsCode(err, fault.CodeConflict) {
		repositories.DB.Where("id = ?", clash.ID).Delete(&UserModel{})
		t.Fatalf("saving a second account with the same username = %v, want a conflict", err)
	}
	// ... and the existing account is untouched, not overwritten through the unique key.
	if kept, err := repositories.FindByID(ctx, leaverID); err != nil || kept == nil || kept.Email != leaverID+"@example.test" {
		t.Fatalf("existing account after the clash = %+v, %v", kept, err)
	}
	sameEmail := authdomain.User{ID: prefix + "-clash", Email: heirID + "@example.test", PasswordHash: "h", Role: identity.RoleUser}
	if err := repositories.SaveUser(ctx, sameEmail); !fault.IsCode(err, fault.CodeConflict) {
		repositories.DB.Where("id = ?", sameEmail.ID).Delete(&UserModel{})
		t.Fatalf("saving a second account with the same email = %v, want a conflict", err)
	}

	for index, platform := range []string{"sms", "sms", "nes"} {
		file := validROMFile(fmt.Sprintf("%s-rf-%d", prefix, index), fmt.Sprintf("%s-%d", prefix, index))
		file.Size = 1000
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{ID: fmt.Sprintf("%s-rom-%d", prefix, index), ROMFileID: file.ID, UploaderID: leaverID,
			Identity: romdomain.ROMIdentity{Title: "Game", Platform: platform, IdentifiedBy: "test", Confidence: 100}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repositories.SaveUpload(ctx, uploadsdomain.Upload{ID: prefix + "-upl", UploaderID: leaverID, OriginalFilename: "a.sms", Status: uploadsdomain.UploadStatusProcessing}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveLibrary(ctx, librariesdomain.Library{ID: prefix + "-lib", UserID: leaverID, Name: "Favourites"}); err != nil {
		t.Fatal(err)
	}
	expires := uint64(time.Now().Add(time.Hour).Unix())
	if err := repositories.Store(ctx, authdomain.RefreshToken{JTI: prefix + "-jti", UserID: leaverID, TokenHash: prefix + "-token-hash", ExpiresAt: expires, CreatedAt: uint64(time.Now().Unix())}); err != nil {
		t.Fatal(err)
	}

	activity, err := repositories.UserActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaver := activity[leaverID]
	if leaver.ROMCount != 3 || leaver.StorageBytes != 3000 || leaver.UploadCount != 1 || leaver.ActiveSessions != 1 || len(leaver.Platforms) != 2 {
		t.Fatalf("activity = %+v", leaver)
	}

	transfer, err := repositories.DeleteUserTransferringData(ctx, leaverID, heirID)
	if err != nil {
		t.Fatal(err)
	}
	if transfer.ROMs != 3 || transfer.Libraries != 1 || transfer.Uploads != 1 {
		t.Fatalf("transfer = %+v", transfer)
	}
	if gone, _ := repositories.FindByID(ctx, leaverID); gone != nil {
		t.Fatal("the deleted user still exists")
	}
	if token, _ := repositories.FindByJTI(ctx, prefix+"-jti"); token != nil {
		t.Fatal("the deleted user's session survived")
	}
	after, err := repositories.UserActivity(ctx)
	if err != nil || after[heirID].ROMCount != 3 {
		t.Fatalf("heir activity after transfer = %+v, %v", after[heirID], err)
	}
	if _, err := repositories.DeleteUserTransferringData(ctx, leaverID, heirID); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("deleting a missing user = %v, want not found", err)
	}
}

func TestRepositoriesListROMsInSortOrder(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	exerciseROMSorting(t, ctx, repositories, "user-1")
}

// exerciseROMSorting checks sorted listings on any dialect by paging two ROMs at a time:
// text sorts ignore case, ties break by ID, and ROMs without a value come last either way.
func exerciseROMSorting(t *testing.T, ctx context.Context, repositories *Repositories, owner string) {
	t.Helper()
	text := func(value string) *string { return &value }
	rows := []struct {
		key, title       string
		publisher, genre *string
		year             *string
	}{
		{"a", "bravo", text("Sega"), text("Action"), text("1992")},
		{"b", "Alpha", nil, text("puzzle"), text("1990")},
		{"c", "charlie", text("capcom"), nil, nil},
		{"d", "Delta", text("Sega"), text("action"), text("1991")},
		{"e", "echo", nil, nil, nil},
	}
	for _, row := range rows {
		id := owner + "-sort-" + row.key
		file := validROMFile(owner+"-sortf-"+row.key, id)
		if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := repositories.SaveROM(ctx, romdomain.ROM{
			ID: id, ROMFileID: file.ID, UploaderID: owner,
			Identity: romdomain.ROMIdentity{
				Title: row.title, Platform: "snes", IdentifiedBy: "test", Confidence: 100,
				Publisher: row.publisher, Genre: row.genre, ReleaseYear: row.year,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, row := range rows {
			_ = repositories.DeleteROM(ctx, owner+"-sort-"+row.key)
			_ = repositories.DeleteROMFile(ctx, owner+"-sortf-"+row.key)
		}
	})

	list := func(sort rom.ROMSort) string {
		var order []string
		filter := rom.ROMFilter{UploaderID: &owner, Sort: sort}
		for range len(rows) + 1 {
			page, err := repositories.ListROMsWithFiles(ctx, 2, filter)
			if err != nil {
				t.Fatalf("%+v: %v", sort, err)
			}
			more := len(page) > 2
			if more {
				page = page[:2]
			}
			for _, row := range page {
				order = append(order, strings.TrimPrefix(row.ROM.ID, owner+"-sort-"))
			}
			if !more {
				return strings.Join(order, "")
			}
			last := page[len(page)-1].ROM
			filter.After = &rom.ROMCursor{ID: last.ID, Value: rom.SortValue(last, sort.Field)}
		}
		t.Fatalf("%+v: paging did not end", sort)
		return ""
	}
	for _, test := range []struct {
		sort rom.ROMSort
		want string
	}{
		{rom.ROMSort{}, "abcde"},
		{rom.ROMSort{Field: rom.SortTitle}, "bacde"},
		{rom.ROMSort{Field: rom.SortTitle, Descending: true}, "edcab"},
		{rom.ROMSort{Field: rom.SortRelease}, "bdace"},
		{rom.ROMSort{Field: rom.SortRelease, Descending: true}, "adbce"},
		{rom.ROMSort{Field: rom.SortPublisher}, "cadbe"},
		{rom.ROMSort{Field: rom.SortPublisher, Descending: true}, "dacbe"},
		{rom.ROMSort{Field: rom.SortGenre}, "adbce"},
		{rom.ROMSort{Field: rom.SortGenre, Descending: true}, "bdace"},
	} {
		if got := list(test.sort); got != test.want {
			t.Errorf("order %+v = %s, want %s", test.sort, got, test.want)
		}
	}
}

func TestRepositoriesSortedListingsWalkTheSortIndexes(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	const listing = "SELECT r.id FROM rom AS r JOIN rom_file AS f ON f.id = r.rom_file_id WHERE "
	for _, test := range []struct {
		name, where, index string
		args               []any
	}{
		{"title", "r.title IS NOT NULL ORDER BY r.title COLLATE NOCASE ASC, r.id ASC LIMIT 61", "idx_rom_sort_title", nil},
		{"owner by genre, descending", "r.uploader_id = ? AND r.genre IS NOT NULL AND (r.genre COLLATE NOCASE < ? OR (r.genre COLLATE NOCASE = ? AND r.id < ?)) ORDER BY r.genre COLLATE NOCASE DESC, r.id DESC LIMIT 61", "idx_rom_uploader_sort_genre", []any{"usr", "b", "b", "x"}},
		{"owner by release", "r.uploader_id = ? AND r.release_year IS NOT NULL ORDER BY r.release_year COLLATE NOCASE ASC, r.id ASC LIMIT 61", "idx_rom_uploader_sort_release", []any{"usr"}},
	} {
		plan := queryPlan(t, repositories, ctx, listing+test.where, test.args...)
		if !strings.Contains(plan, test.index) || strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s: plan %q should use %s with no sort step", test.name, plan, test.index)
		}
	}
}

func TestRepositoriesTrackUploadSessions(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	exerciseUploadSessions(t, ctx, repositories, "ups_sqlite")
}

// exerciseUploadSessions checks upload sessions on any dialect; IDs start with prefix.
func exerciseUploadSessions(t *testing.T, ctx context.Context, repositories *Repositories, prefix string) {
	t.Helper()
	mime := "application/octet-stream"
	session := uploadsdomain.Session{
		ID: prefix + "_1", UploaderID: "player", Filename: "game.sfc", MIMEType: &mime,
		Size: 5 << 30, HashState: []byte{1, 2, 3}, UpdatedAt: time.Now().Add(-48 * time.Hour),
	}
	if err := repositories.SaveUploadSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repositories.DeleteUploadSession(ctx, session.ID) })
	found, err := repositories.FindUploadSession(ctx, session.ID)
	if err != nil || found == nil || found.Size != 5<<30 || *found.MIMEType != mime || !bytes.Equal(found.HashState, []byte{1, 2, 3}) {
		t.Fatalf("FindUploadSession() = %+v, %v", found, err)
	}
	idle, err := repositories.ListIdleUploadSessions(ctx, time.Now().Add(-24*time.Hour))
	if err != nil || !slices.Contains(idle, session.ID) {
		t.Fatalf("idle sessions = %v, %v; want %s", idle, err, session.ID)
	}

	advanced := *found
	advanced.Received, advanced.HashState, advanced.UpdatedAt = 4<<30, []byte{4, 5}, time.Now()
	if ok, err := repositories.AdvanceUploadSession(ctx, advanced, 0); err != nil || !ok {
		t.Fatalf("advance = %v, %v", ok, err)
	}
	// A second request for the same part finds the session already moved on.
	if ok, err := repositories.AdvanceUploadSession(ctx, advanced, 0); err != nil || ok {
		t.Fatalf("stale advance = %v, %v; want refused", ok, err)
	}
	found, _ = repositories.FindUploadSession(ctx, session.ID)
	if found.Received != 4<<30 || !bytes.Equal(found.HashState, []byte{4, 5}) {
		t.Fatalf("advanced session = %+v", found)
	}
	if idle, _ := repositories.ListIdleUploadSessions(ctx, time.Now().Add(-24*time.Hour)); slices.Contains(idle, session.ID) {
		t.Fatal("an advanced session is still listed as idle")
	}
	if err := repositories.DeleteUploadSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if found, err := repositories.FindUploadSession(ctx, session.ID); err != nil || found != nil {
		t.Fatalf("deleted session = %+v, %v", found, err)
	}
}

func TestRepositoriesCountForMetrics(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	file := validROMFile("rf-metrics", "metrics")
	if _, err := repositories.UpsertROMFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveROM(ctx, romdomain.ROM{ID: "rom-metrics", ROMFileID: file.ID, UploaderID: "u", Identity: romdomain.ROMIdentity{Title: "t", Platform: "snes", IdentifiedBy: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveUploadSession(ctx, uploadsdomain.Session{ID: "ups_metrics", UploaderID: "u", Filename: "f.sfc", Size: 1 << 30, HashState: []byte{1}, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	counts, err := repositories.CountForMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.ROMs != 1 || counts.ROMFiles != 1 || counts.ROMBytes != int64(file.Size) || counts.UploadSessions != 1 || counts.UploadSessionBytes != 1<<30 {
		t.Fatalf("counts = %+v", counts)
	}
}

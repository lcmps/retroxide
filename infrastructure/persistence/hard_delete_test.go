package persistence

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	romapp "github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	"github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func storedROMFile(t *testing.T, repositories *Repositories, files *storage.Filesystem, id, content string) romdomain.ROMFile {
	t.Helper()
	data := []byte(content)
	sha256Digest, sha1Digest, md5Digest := sha256.Sum256(data), sha1.Sum(data), md5.Sum(data)
	hash, err := romdomain.NewROMHash(hex.EncodeToString(sha256Digest[:]), hex.EncodeToString(sha1Digest[:]), hex.EncodeToString(md5Digest[:]), fmt.Sprintf("%08x", crc32.ChecksumIEEE(data)))
	if err != nil {
		t.Fatal(err)
	}
	path, err := files.Write(context.Background(), hash.SHA256, data)
	if err != nil {
		t.Fatal(err)
	}
	file := romdomain.ROMFile{ID: id, Hash: hash, Size: uint64(len(data)), StoragePath: path}
	if _, err := repositories.UpsertROMFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	return file
}

func seedDeleteROM(t *testing.T, repositories *Repositories, files *storage.Filesystem) (romdomain.ROM, []string) {
	t.Helper()
	file := storedROMFile(t, repositories, files, "rf-delete", "ROM bytes")
	paths := []string{file.StoragePath}
	for _, content := range []string{"cover image", "title image", "screenshot image"} {
		digest := sha256.Sum256([]byte(content))
		path, err := files.Write(context.Background(), hex.EncodeToString(digest[:]), []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	rom := romdomain.ROM{ID: "rom-delete", ROMFileID: file.ID, UploaderID: "owner", Identity: romdomain.ROMIdentity{
		Title: "Game", Platform: "nes", IdentifiedBy: "filename", CoverArtPath: &paths[1], TitleScreenPath: &paths[2], ScreenshotPath: &paths[3],
	}}
	if err := repositories.SaveROM(context.Background(), rom); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{
		&UploadModel{ID: "upload-1", UploaderID: "owner", OriginalFilename: "game.nes", Status: "completed", ROMID: &rom.ID},
		&UploadModel{ID: "upload-2", UploaderID: "owner", OriginalFilename: "game.nes", Status: "completed", ROMID: &rom.ID},
		&UploadModel{ID: "upload-processing", UploaderID: "owner", OriginalFilename: "game.nes", Status: "processing"},
		&JobQueueModel{Kind: "identify_rom", UploadID: "upload-1", ROMFileID: file.ID, AvailableAt: "2000-01-01 00:00:00"},
		&JobQueueModel{Kind: "identify_rom", UploadID: "upload-processing", ROMFileID: file.ID, AvailableAt: "2000-01-01 00:00:00"},
		&JobDeadLetterModel{Kind: "identify_rom", UploadID: "upload-2", ROMFileID: file.ID, Reason: "failed"},
		&LibraryModel{ID: "library", UserID: "owner", Name: "Games"},
		&LibraryItemModel{LibraryID: "library", ROMID: rom.ID},
	} {
		if err := repositories.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	return rom, paths
}

func assertDeleteCount(t *testing.T, repositories *Repositories, model any, want int64) {
	t.Helper()
	var count int64
	if err := repositories.DB.Model(model).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("%T count = %d, want %d", model, count, want)
	}
}

func TestROMHardDeleteCleansMetadataAndBytes(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, paths := seedDeleteROM(t, repositories, files)
	service := romapp.NewService(repositories, files, nil, nil)
	if err := service.Delete(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, rom.ID); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&ROMModel{}, &ROMFileModel{}, &UploadModel{}, &JobQueueModel{}, &JobDeadLetterModel{}, &LibraryItemModel{}} {
		assertDeleteCount(t, repositories, model, 0)
	}
	assertDeleteCount(t, repositories, &LibraryModel{}, 1)
	for _, path := range paths {
		if stored(files, path) {
			t.Errorf("deleted path %s remains", path)
		}
	}
}

type failingDeleteStorage struct {
	*storage.Filesystem
	failPath     string
	beforeDelete func()
}

var errDeleteStorage = errors.New("storage unavailable")

func (files *failingDeleteStorage) Delete(ctx context.Context, path string) error {
	if files.beforeDelete != nil {
		files.beforeDelete()
	}
	if path == files.failPath {
		return errDeleteStorage
	}
	return files.Filesystem.Delete(ctx, path)
}

// stored reports whether files holds content at path.
func stored(files *storage.Filesystem, path string) bool {
	content, err := files.Open(context.Background(), path)
	if err != nil {
		return false
	}
	_ = content.Close()
	return true
}

// Files are removed only after the deletion commits, so a failed commit can never leave a
// record whose file is gone. A file that cannot be removed then stays behind for the storage
// sweep to reclaim; the deletion itself still succeeds.
func TestROMHardDeleteRemovesFilesOnlyAfterCommitting(t *testing.T) {
	for _, kind := range []string{"screenshot", "rom_bytes"} {
		t.Run(kind, func(t *testing.T) {
			repositories, ctx := openTestRepositories(t)
			files := &failingDeleteStorage{Filesystem: storage.New(t.TempDir())}
			rom, paths := seedDeleteROM(t, repositories, files.Filesystem)
			failed := paths[3]
			if kind == "rom_bytes" {
				failed = paths[0]
			}
			files.failPath = failed
			var committedBeforeDelete bool
			files.beforeDelete = func() {
				var count int64
				repositories.DB.Model(&ROMModel{}).Where("id = ?", rom.ID).Count(&count)
				committedBeforeDelete = count == 0
			}
			service := romapp.NewService(repositories, files, nil, nil)
			if err := service.Delete(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, rom.ID); err != nil {
				t.Fatalf("Delete error = %v, want success despite the storage failure", err)
			}
			if !committedBeforeDelete {
				t.Fatal("files were deleted before the deletion committed")
			}
			for _, model := range []any{&ROMModel{}, &ROMFileModel{}, &LibraryItemModel{}, &JobDeadLetterModel{}, &UploadModel{}, &JobQueueModel{}} {
				assertDeleteCount(t, repositories, model, 0)
			}
			for _, path := range paths {
				if stored(files.Filesystem, path) != (path == failed) {
					t.Errorf("path %s stored = %v, want only the failed one left", path, stored(files.Filesystem, path))
				}
			}
		})
	}
}

func TestROMHardDeleteConcealsUnauthorizedAndMissing(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, paths := seedDeleteROM(t, repositories, files)
	service := romapp.NewService(repositories, files, nil, nil)
	for _, id := range []string{rom.ID, "missing"} {
		if err := service.Delete(ctx, identity.Requester{UserID: "other", Role: identity.RoleUser}, id); !fault.IsCode(err, fault.CodeNotFound) {
			t.Errorf("Delete(%s) = %v, want not found", id, err)
		}
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
	assertDeleteCount(t, repositories, &UploadModel{}, 3)
	assertDeleteCount(t, repositories, &JobQueueModel{}, 2)
	assertDeleteCount(t, repositories, &JobDeadLetterModel{}, 1)
	assertDeleteCount(t, repositories, &LibraryItemModel{}, 1)
	for _, path := range paths {
		if _, err := files.Read(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestROMHardDeletePreservesSharedFileAndMedia(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, paths := seedDeleteROM(t, repositories, files)
	other := romdomain.ROM{ID: "rom-other", ROMFileID: rom.ROMFileID, UploaderID: "other", Identity: romdomain.ROMIdentity{
		Title: "Other", Platform: "nes", IdentifiedBy: "manual", ScreenshotPath: &paths[1], TitleScreenPath: &paths[0],
	}}
	if err := repositories.SaveROM(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := repositories.DB.Create(&UploadModel{ID: "upload-other", UploaderID: "other", OriginalFilename: "other.nes", Status: "completed", ROMID: &other.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repositories.Enqueue(ctx, jobsdomain.Payload{Kind: "identify_rom", UploadID: "upload-other", ROMFileID: rom.ROMFileID}); err != nil {
		t.Fatal(err)
	}
	service := romapp.NewService(repositories, files, nil, nil)
	if err := service.Delete(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, rom.ID); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &ROMFileModel{}, 1)
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
	assertDeleteCount(t, repositories, &UploadModel{}, 2)
	assertDeleteCount(t, repositories, &JobQueueModel{}, 2)
	assertDeleteCount(t, repositories, &JobDeadLetterModel{}, 0)
	for _, path := range paths[:2] {
		if _, err := files.Read(ctx, path); err != nil {
			t.Errorf("shared path was deleted: %v", err)
		}
	}
	for _, path := range paths[2:] {
		if stored(files, path) {
			t.Errorf("orphan media remains: %s", path)
		}
	}
}

func TestROMHardDeletePreservesCrossKindBlobReferences(t *testing.T) {
	for _, kind := range []string{"cover", "title", "screenshot", "rom_file"} {
		t.Run(kind, func(t *testing.T) {
			repositories, ctx := openTestRepositories(t)
			files := storage.New(t.TempDir())
			rom, paths := seedDeleteROM(t, repositories, files)
			otherFile := storedROMFile(t, repositories, files, "rf-other", "other bytes")
			other := romdomain.ROM{ID: "other", ROMFileID: otherFile.ID, UploaderID: "other", Identity: romdomain.ROMIdentity{Title: "Other", Platform: "nes", IdentifiedBy: "manual"}}
			switch kind {
			case "cover":
				other.Identity.CoverArtPath = &paths[0]
			case "title":
				other.Identity.TitleScreenPath = &paths[0]
			case "screenshot":
				other.Identity.ScreenshotPath = &paths[0]
			case "rom_file":
				if err := repositories.DB.Model(&ROMFileModel{}).Where("id = ?", otherFile.ID).Update("storage_path", paths[1]).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := repositories.SaveROM(ctx, other); err != nil {
				t.Fatal(err)
			}
			service := romapp.NewService(repositories, files, nil, nil)
			if err := service.Delete(ctx, identity.Requester{Role: identity.RoleAdmin}, rom.ID); err != nil {
				t.Fatal(err)
			}
			preserved := paths[0]
			if kind == "rom_file" {
				preserved = paths[1]
			}
			if _, err := files.Read(ctx, preserved); err != nil {
				t.Fatalf("cross-kind referenced path deleted: %v", err)
			}
			assertDeleteCount(t, repositories, &ROMFileModel{}, 1)
		})
	}
}

func TestROMHardDeleteAllowsUploadAgain(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"nes"}})
	requester := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	// Each upload streams its content once, so every attempt needs a fresh reader.
	input := func() uploads.UploadInput {
		return uploads.UploadInput{Filename: "game.nes", Content: strings.NewReader("actual ROM content")}
	}
	accepted, err := uploader.Create(ctx, requester, input())
	if err != nil {
		t.Fatal(err)
	}
	job, err := repositories.Claim(ctx, time.Now(), time.Minute, jobsdomain.KindFilter{})
	if err != nil || job == nil {
		t.Fatalf("claim = %v, %v", job, err)
	}
	identifier := NewIdentifier(repositories.DB)
	if err := identifier.Identify(ctx, accepted.UploadID, job.Payload.ROMFileID); err != nil {
		t.Fatal(err)
	}
	upload, err := repositories.FindUpload(ctx, accepted.UploadID)
	if err != nil || upload == nil || upload.ROMID == nil {
		t.Fatalf("upload = %v, %v", upload, err)
	}
	service := romapp.NewService(repositories, files, nil, nil)
	if err := service.Delete(ctx, requester, *upload.ROMID); err != nil {
		t.Fatal(err)
	}
	second, err := uploader.Create(ctx, requester, input())
	if err != nil {
		t.Fatalf("reupload = %v", err)
	}
	if err := identifier.Identify(ctx, second.UploadID, job.Payload.ROMFileID); err != nil {
		t.Fatalf("identifying reupload = %v", err)
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
}

func TestROMHardDeleteRejectsStaleJobWrites(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, _ := seedDeleteROM(t, repositories, files)
	upload, err := repositories.FindUpload(ctx, "upload-1")
	if err != nil {
		t.Fatal(err)
	}
	job, err := repositories.Claim(ctx, time.Now(), time.Minute, jobsdomain.KindFilter{})
	if err != nil || job == nil {
		t.Fatalf("claim = %v, %v", job, err)
	}
	if err := romapp.NewService(repositories, files, nil, nil).Delete(ctx, identity.Requester{Role: identity.RoleAdmin}, rom.ID); err != nil {
		t.Fatal(err)
	}
	if err := repositories.SaveUpload(ctx, *upload); err == nil {
		t.Error("stale upload completion succeeded")
	}
	if err := repositories.DeadLetter(ctx, *job, 3, "stale job failed"); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &UploadModel{}, 0)
	assertDeleteCount(t, repositories, &JobDeadLetterModel{}, 0)
}

func TestROMHardDeleteCannotBeRecreatedByCompletedSharedUpload(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, _ := seedDeleteROM(t, repositories, files)
	other := rom
	other.ID, other.UploaderID = "other", "other"
	if err := repositories.SaveROM(ctx, other); err != nil {
		t.Fatal(err)
	}
	identifier := NewIdentifier(repositories.DB)
	if err := identifier.Identify(ctx, "upload-1", rom.ROMFileID); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 2)
}

func TestROMHardDeleteCannotBeRecreatedByClaimedJob(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, _ := seedDeleteROM(t, repositories, files)
	job, err := repositories.Claim(ctx, time.Now(), time.Minute, jobsdomain.KindFilter{})
	if err != nil || job == nil {
		t.Fatalf("claim = %v, %v", job, err)
	}
	if err := romapp.NewService(repositories, files, nil, nil).Delete(ctx, identity.Requester{Role: identity.RoleAdmin}, rom.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewIdentifier(repositories.DB).Identify(ctx, job.Payload.UploadID, job.Payload.ROMFileID); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("stale identification = %v, want not found", err)
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 0)
}

func TestROMHardDeleteKeepsUnrelatedRecords(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	rom, _ := seedDeleteROM(t, repositories, files)
	file := storedROMFile(t, repositories, files, "rf-unrelated", "unrelated content")
	for _, model := range []any{
		&UploadModel{ID: "unrelated", UploaderID: "other", OriginalFilename: "other.nes", Status: string(uploadsdomain.UploadStatusProcessing)},
		&JobQueueModel{Kind: "identify_rom", UploadID: "unrelated", ROMFileID: file.ID, AvailableAt: "2000-01-01 00:00:00"},
		&JobDeadLetterModel{Kind: "identify_rom", UploadID: "unrelated", ROMFileID: file.ID, Reason: "failed"},
	} {
		if err := repositories.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := romapp.NewService(repositories, files, nil, nil).Delete(ctx, identity.Requester{Role: identity.RoleAdmin}, rom.ID); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&UploadModel{}, &JobQueueModel{}, &JobDeadLetterModel{}, &ROMFileModel{}} {
		assertDeleteCount(t, repositories, model, 1)
	}
	if _, err := files.Read(ctx, file.StoragePath); err != nil {
		t.Fatal(err)
	}
}

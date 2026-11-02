package persistence

import (
	"errors"
	"testing"

	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
	"github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/storage"
	"gorm.io/gorm"
)

func TestIdentifierRollsBackROMWhenUploadCompletionFails(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), repositories, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"nes"}})
	accepted, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "game.nes", Bytes: []byte("actual ROM content")})
	if err != nil {
		t.Fatal(err)
	}
	var queued JobQueueModel
	if err := repositories.DB.Take(&queued).Error; err != nil {
		t.Fatal(err)
	}
	completionError := errors.New("upload completion failed")
	if err := repositories.DB.Callback().Update().Before("gorm:update").Register("test:fail_upload_completion", func(tx *gorm.DB) {
		if tx.Statement.Table == "upload" {
			tx.AddError(completionError)
		}
	}); err != nil {
		t.Fatal(err)
	}
	identifier := NewIdentifier(repositories.DB)
	if err := identifier.Identify(ctx, accepted.UploadID, queued.ROMFileID); !errors.Is(err, completionError) {
		t.Fatalf("Identify error = %v, want upload completion error", err)
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 0)
	upload, err := repositories.FindUpload(ctx, accepted.UploadID)
	if err != nil || upload == nil || string(upload.Status) != "processing" || upload.ROMID != nil {
		t.Fatalf("upload after rollback = %+v, %v", upload, err)
	}
	if err := repositories.DB.Callback().Update().Remove("test:fail_upload_completion"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := identifier.Identify(ctx, accepted.UploadID, queued.ROMFileID); err != nil {
			t.Fatal(err)
		}
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
}

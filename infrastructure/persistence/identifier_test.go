package persistence

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lcmps/retroxide/application/jobs"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	"github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/storage"
	"gorm.io/gorm"
)

func TestIdentifierRollsBackROMWhenUploadCompletionFails(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"nes"}})
	accepted, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "game.nes", Content: strings.NewReader("actual ROM content")})
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

func TestConcurrentDiscsEndUpInOneTitle(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"iso"}})
	for _, disc := range []string{"1", "2"} {
		if _, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "Game (USA) (Disc " + disc + ").iso", Content: strings.NewReader("disc " + disc + " content")}); err != nil {
			t.Fatal(err)
		}
	}
	var queued []JobQueueModel
	if err := repositories.DB.Order("id").Find(&queued).Error; err != nil || len(queued) != 2 {
		t.Fatalf("queued jobs = %d, %v", len(queued), err)
	}
	identifier := NewIdentifier(repositories.DB)
	var group sync.WaitGroup
	errs := make([]error, len(queued))
	for index, job := range queued {
		group.Add(1)
		go func() {
			defer group.Done()
			// The job queue retries an errored job.
			for range 5 {
				if errs[index] = identifier.Identify(ctx, job.UploadID, job.ROMFileID); errs[index] == nil {
					return
				}
			}
		}()
	}
	group.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("job %d still failing after retries: %v", index, err)
		}
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
	var rom ROMModel
	if err := repositories.DB.Take(&rom).Error; err != nil {
		t.Fatal(err)
	}
	discs, err := repositories.FindROMDiscs(ctx, rom.ID)
	if err != nil || len(discs) != 2 || discs[0].Number != 1 || discs[1].Number != 2 {
		t.Fatalf("discs = %+v, %v", discs, err)
	}
	for _, job := range queued {
		upload, err := repositories.FindUpload(ctx, job.UploadID)
		if err != nil || upload == nil || string(upload.Status) != "completed" || upload.ROMID == nil || *upload.ROMID != rom.ID {
			t.Fatalf("upload = %+v, %v", upload, err)
		}
	}
}

// staleGroupRepositories misses the disc group once, as a worker does when another creates the
// ROM between its lookup and its insert.
type staleGroupRepositories struct {
	*Repositories
	missed bool
}

func (repositories *staleGroupRepositories) FindROMByDiscGroup(ctx context.Context, uploaderID, group string) (*romdomain.ROM, error) {
	if !repositories.missed {
		repositories.missed = true
		return nil, nil
	}
	return repositories.Repositories.FindROMByDiscGroup(ctx, uploaderID, group)
}

func TestDiscGroupConflictRollsBackAndTheRetryJoins(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"iso"}})
	for _, disc := range []string{"1", "2"} {
		if _, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "Game (USA) (Disc " + disc + ").iso", Content: strings.NewReader("disc " + disc + " content")}); err != nil {
			t.Fatal(err)
		}
	}
	var queued []JobQueueModel
	if err := repositories.DB.Order("id").Find(&queued).Error; err != nil || len(queued) != 2 {
		t.Fatalf("queued jobs = %d, %v", len(queued), err)
	}
	if err := NewIdentifier(repositories.DB).Identify(ctx, queued[0].UploadID, queued[0].ROMFileID); err != nil {
		t.Fatal(err)
	}
	err := repositories.DB.Transaction(func(tx *gorm.DB) error {
		stale := &staleGroupRepositories{Repositories: NewRepositories(tx)}
		return uploads.NewIdentifyService(stale, stale).Identify(ctx, queued[1].UploadID, queued[1].ROMFileID)
	})
	if err == nil {
		t.Fatal("a conflicting disc group must return an error so the job is retried")
	}
	assertDeleteCount(t, repositories, &ROMModel{}, 1)
	upload, err := repositories.FindUpload(ctx, queued[1].UploadID)
	if err != nil || upload == nil || string(upload.Status) != "processing" {
		t.Fatalf("upload after rollback = %+v, %v; want still processing", upload, err)
	}
	if err := NewIdentifier(repositories.DB).Identify(ctx, queued[1].UploadID, queued[1].ROMFileID); err != nil {
		t.Fatal(err)
	}
	var rom ROMModel
	if err := repositories.DB.Take(&rom).Error; err != nil {
		t.Fatal(err)
	}
	if discs, err := repositories.FindROMDiscs(ctx, rom.ID); err != nil || len(discs) != 2 {
		t.Fatalf("discs = %+v, %v", discs, err)
	}
}

// failingDiscIndexer fails every attempt, as an image that can't be read would.
type failingDiscIndexer struct{}

func (failingDiscIndexer) Index(context.Context, romdomain.ROMFile, string) error {
	return errors.New("cannot read image")
}

// The pipeline falls back to the filename only when the identifier it is given can do so. In
// production that is this Identifier, so the fallback must be reachable through it.
func TestFilenameFallbackIsWiredThroughTheProductionIdentifier(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"iso"}})
	accepted, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "Unreadable Game (USA).iso", Content: strings.NewReader("not a disc anyone can read")})
	if err != nil {
		t.Fatal(err)
	}
	var job JobQueueModel
	if err := repositories.DB.Where("upload_id = ?", accepted.UploadID).Take(&job).Error; err != nil {
		t.Fatal(err)
	}
	identifier := NewIdentifier(repositories.DB).WithDiscs(failingDiscIndexer{})
	if err := identifier.Identify(ctx, job.UploadID, job.ROMFileID); err == nil {
		t.Fatal("Identify succeeded although indexing fails")
	}
	handler := jobs.ROMPipeline(identifier, nil, nil)[jobsdomain.KindIdentifyROM]
	if handler.OnExhausted == nil {
		t.Fatal("the production identifier gives the pipeline no filename fallback")
	}
	if err := handler.OnExhausted(ctx, jobsdomain.Payload{Kind: jobsdomain.KindIdentifyROM, UploadID: job.UploadID, ROMFileID: job.ROMFileID}); err != nil {
		t.Fatalf("filename fallback = %v", err)
	}
	upload, err := repositories.FindUpload(ctx, job.UploadID)
	if err != nil || upload.Status != uploadsdomain.UploadStatusCompleted || upload.ROMID == nil {
		t.Fatalf("upload after the fallback = %+v, %v; want completed with a ROM", upload, err)
	}
	rom, err := repositories.FindROMByID(ctx, *upload.ROMID)
	if err != nil || rom == nil || rom.Identity.Title != "Unreadable Game (USA).iso" {
		t.Fatalf("ROM after the fallback = %+v, %v; want it titled with the filename", rom, err)
	}
}

// When even the fallback fails, the upload must end as failed with the reason, not stay
// "processing" forever: the transaction that failed cannot be what records it.
func TestAFailedFilenameFallbackMarksTheUploadFailed(t *testing.T) {
	repositories, ctx := openTestRepositories(t)
	files := storage.New(t.TempDir())
	uploader := uploads.NewUploadService(repositories, repositories, files, hash.New(), nil, nil, uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"iso"}})
	accepted, err := uploader.Create(ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser}, uploads.UploadInput{Filename: "Lost Game (USA).iso", Content: strings.NewReader("a file whose record is gone")})
	if err != nil {
		t.Fatal(err)
	}
	var job JobQueueModel
	if err := repositories.DB.Where("upload_id = ?", accepted.UploadID).Take(&job).Error; err != nil {
		t.Fatal(err)
	}
	// Without its ROM file the fallback cannot record a ROM.
	if err := repositories.DB.Where("id = ?", job.ROMFileID).Delete(&ROMFileModel{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewIdentifier(repositories.DB).CompleteWithFilename(ctx, job.UploadID, job.ROMFileID); err == nil {
		t.Fatal("the fallback succeeded without its ROM file")
	}
	upload, err := repositories.FindUpload(ctx, job.UploadID)
	if err != nil || upload.Status != uploadsdomain.UploadStatusFailed || upload.ErrorMessage == nil || *upload.ErrorMessage == "" {
		t.Fatalf("upload after a failed fallback = %+v, %v; want failed with a reason", upload, err)
	}
}

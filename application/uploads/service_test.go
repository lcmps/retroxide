package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	romhash "github.com/lcmps/retroxide/infrastructure/hash"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

type testROMRepository struct {
	existing  *romdomain.ROMFile
	saved     []romdomain.ROMFile
	upsertErr error
	// unused makes the existing file one no ROM uses, as a failed identification leaves it.
	unused bool
}

func (repository *testROMRepository) CountROMsByROMFileID(context.Context, string) (uint64, error) {
	if repository.unused {
		return 0, nil
	}
	return 1, nil
}

func (repository *testROMRepository) FindROMFileBySHA256(_ context.Context, _ string) (*romdomain.ROMFile, error) {
	return repository.existing, nil
}

func (repository *testROMRepository) UpsertROMFile(_ context.Context, romFile romdomain.ROMFile) (romdomain.ROMFile, error) {
	if repository.upsertErr != nil {
		return romdomain.ROMFile{}, repository.upsertErr
	}
	repository.saved = append(repository.saved, romFile)
	return romFile, nil
}

type testUploadRepository struct {
	saved []uploadsdomain.Upload
	jobs  []jobsdomain.Payload
	err   error
	// jobErr fails SaveUploadWithJob as a whole, as a rolled-back transaction would.
	jobErr error
}

func (repository *testUploadRepository) SaveUploadWithJob(_ context.Context, upload uploadsdomain.Upload, job jobsdomain.Payload) error {
	if repository.err != nil {
		return repository.err
	}
	if repository.jobErr != nil {
		return repository.jobErr
	}
	repository.saved = append(repository.saved, upload)
	repository.jobs = append(repository.jobs, job)
	return nil
}

func (repository *testUploadRepository) SaveUpload(_ context.Context, upload uploadsdomain.Upload) error {
	if repository.err != nil {
		return repository.err
	}
	repository.saved = append(repository.saved, upload)
	return nil
}

func (repository *testUploadRepository) FindUpload(_ context.Context, _ string) (*uploadsdomain.Upload, error) {
	return nil, nil
}

// testStorage stages in memory; writes counts commits and discards counts abandoned staging.
type testStorage struct {
	staged       int
	writes       int
	discards     int
	deletes      []string
	deleteCtxErr error
}

func (storage *testStorage) Stage(_ context.Context, content io.Reader, _ int64) (StagedContent, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	storage.staged++
	return &testStaged{storage: storage, size: int64(len(data))}, nil
}

// pending reports staged content that was neither committed nor discarded.
func (storage *testStorage) pending() int {
	return storage.staged - storage.writes - storage.discards
}

type testStaged struct {
	storage *testStorage
	size    int64
	done    bool
}

func (staged *testStaged) Size() int64 { return staged.size }

func (staged *testStaged) Commit(_ context.Context, contentAddress string) (string, error) {
	staged.done = true
	staged.storage.writes++
	return "/storage/" + contentAddress, nil
}

func (staged *testStaged) Discard() {
	if !staged.done {
		staged.done = true
		staged.storage.discards++
	}
}

func (storage *testStorage) Delete(ctx context.Context, storagePath string) error {
	storage.deletes = append(storage.deletes, storagePath)
	storage.deleteCtxErr = ctx.Err()
	return nil
}

// testQueue counts worker wake-ups.
type testQueue struct {
	notified int
}

func (queue *testQueue) Notify() { queue.notified++ }

type testAudit struct {
	accepted  int
	duplicate int
}

func (audit *testAudit) UploadAccepted(context.Context, string, string, string, uint64) {
	audit.accepted++
}

func (audit *testAudit) DuplicateUpload(context.Context, string, string) {
	audit.duplicate++
}

func TestUploadServiceCreateAcceptsAndEnqueues(t *testing.T) {
	romRepository := &testROMRepository{}
	uploadRepository := &testUploadRepository{}
	storage := &testStorage{}
	queue := &testQueue{}
	audit := &testAudit{}
	service := NewUploadService(romRepository, uploadRepository, storage, romhash.New(), queue, audit, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedMIMETypes:  []string{"application/octet-stream"},
		AllowedExtensions: []string{"sfc"},
	})

	result, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" || len(uploadRepository.jobs) != 1 || len(uploadRepository.saved) != 1 || queue.notified != 1 {
		t.Fatalf("upload result = %+v, queued=%d uploads=%d wake-ups=%d", result, len(uploadRepository.jobs), len(uploadRepository.saved), queue.notified)
	}
	if uploadRepository.saved[0].UploaderID != "user-1" || uploadRepository.saved[0].Status != uploadsdomain.UploadStatusProcessing {
		t.Fatalf("persisted upload = %+v", uploadRepository.saved[0])
	}
	if job := uploadRepository.jobs[0]; job.Kind != jobsdomain.KindIdentifyROM || job.Attempt != 0 || job.UploadID != uploadRepository.saved[0].ID {
		t.Fatalf("queued payload = %+v", job)
	}
	if storage.writes != 1 || audit.accepted != 1 {
		t.Fatalf("storage writes=%d audit accepted=%d", storage.writes, audit.accepted)
	}
}

func TestUploadServiceCreateRejectsDuplicateBeforeStorageWrite(t *testing.T) {
	hasher := romhash.New()
	romFileHash, err := hasher.Hash([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	romRepository := &testROMRepository{existing: &romdomain.ROMFile{ID: "rf-existing", Hash: romFileHash}}
	storage := &testStorage{}
	queue := &testQueue{}
	audit := &testAudit{}
	service := NewUploadService(romRepository, &testUploadRepository{}, storage, hasher, queue, audit, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})

	_, err = service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("Create() error = %v, want duplicate conflict", err)
	}
	if storage.writes != 0 || storage.pending() != 0 || queue.notified != 0 || audit.duplicate != 1 {
		t.Fatalf("storage writes=%d wake-ups=%d duplicate audits=%d", storage.writes, queue.notified, audit.duplicate)
	}
}

func TestUploadServiceCreateLeavesNothingBehindWhenTheJobCannotBeSaved(t *testing.T) {
	queueError := errors.New("queue unavailable")
	uploadRepository := &testUploadRepository{jobErr: queueError}
	storage := &testStorage{}
	queue := &testQueue{}
	service := NewUploadService(&testROMRepository{}, uploadRepository, storage, romhash.New(), queue, nil, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})

	_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if !errors.Is(err, queueError) {
		t.Fatalf("Create() error = %v, want queue error", err)
	}
	// The upload and its job are saved together or not at all, so no upload is left waiting on a
	// job that will never run.
	if len(uploadRepository.saved) != 0 || queue.notified != 0 {
		t.Fatalf("saved uploads = %+v, wake-ups = %d; want neither", uploadRepository.saved, queue.notified)
	}
	if len(storage.deletes) != 1 {
		t.Fatalf("storage deletes = %d, want 1", len(storage.deletes))
	}
}

func TestUploadServiceCreateRejectsInvalidInputBeforeStorage(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		mimeType *string
		content  []byte
		maxBytes int64
		wantCode fault.Code
	}{
		{name: "empty content", filename: "game.sfc", content: nil, maxBytes: 1024, wantCode: fault.CodeValidation},
		{name: "content too large", filename: "game.sfc", content: []byte("abc"), maxBytes: 2, wantCode: fault.CodeUploadTooLarge},
		{name: "unsupported extension and mime", filename: "game.exe", mimeType: uploadStringPointer("text/plain"), content: []byte("abc"), maxBytes: 1024, wantCode: fault.CodeUnsupportedUpload},
		{name: "invalid path in filename", filename: "../game.sfc", content: []byte("abc"), maxBytes: 1024, wantCode: fault.CodeValidation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := &testStorage{}
			service := NewUploadService(&testROMRepository{}, &testUploadRepository{}, storage, romhash.New(), &testQueue{}, nil, UploadPolicy{
				MaxUploadBytes:    test.maxBytes,
				AllowedMIMETypes:  []string{"application/octet-stream"},
				AllowedExtensions: []string{"sfc"},
			})
			_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
				Filename: test.filename,
				MIMEType: test.mimeType,
				Content:  bytes.NewReader(test.content),
			})
			if !fault.IsCode(err, test.wantCode) {
				t.Fatalf("Create() error = %v, want code %q", err, test.wantCode)
			}
			if storage.writes != 0 || storage.pending() != 0 {
				t.Fatalf("storage writes = %d, pending staged = %d, want none", storage.writes, storage.pending())
			}
		})
	}
}

func TestUploadServiceCreateRemovesContentWhenUploadSaveFails(t *testing.T) {
	storage := &testStorage{}
	service := NewUploadService(&testROMRepository{}, &testUploadRepository{err: errors.New("database unavailable")}, storage, romhash.New(), &testQueue{}, nil, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})
	_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if err == nil {
		t.Fatal("Create() succeeded, want persistence error")
	}
	if len(storage.deletes) != 1 {
		t.Fatalf("storage deletes = %d, want 1", len(storage.deletes))
	}
}

func TestUploadServiceDoesNotDeleteSharedContentAfterDuplicateInsertRace(t *testing.T) {
	hash, err := romhash.New().Hash([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	storage := &testStorage{}
	service := NewUploadService(
		&testROMRepository{upsertErr: fault.ROMAlreadyExists("rf-winner", hash.SHA256)},
		&testUploadRepository{}, storage, romhash.New(), &testQueue{}, nil,
		UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"sfc"}},
	)
	_, err = service.Create(context.Background(), identity.Requester{UserID: "user-2", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("Create() error = %v, want duplicate conflict", err)
	}
	if len(storage.deletes) != 0 {
		t.Fatalf("storage deletes = %d, want no deletion of shared content", len(storage.deletes))
	}
}

func TestUploadServiceCleanupSurvivesRequestCancellation(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	storage := &testStorage{}
	service := NewUploadService(&testROMRepository{}, &testUploadRepository{err: context.Canceled}, storage, romhash.New(), &testQueue{}, nil, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})
	_, err := service.Create(requestContext, identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context canceled", err)
	}
	if len(storage.deletes) != 1 || storage.deleteCtxErr != nil {
		t.Fatalf("cleanup deletes=%d context error=%v", len(storage.deletes), storage.deleteCtxErr)
	}
}

func uploadStringPointer(value string) *string {
	return &value
}

// roomyStorage is storage that reports how much content fits.
type roomyStorage struct {
	testStorage
	room int64
}

func (storage *roomyStorage) HasRoomFor(size int64) bool { return size <= storage.room }

func TestUploadServiceRefusesUploadsThatDoNotFitBeforeWritingThem(t *testing.T) {
	storage := &roomyStorage{room: 100}
	service := NewUploadService(&testROMRepository{}, &testUploadRepository{}, storage, romhash.New(), &testQueue{}, &testAudit{}, UploadPolicy{
		MaxUploadBytes:    1 << 30,
		AllowedMIMETypes:  []string{"application/octet-stream"},
		AllowedExtensions: []string{"sfc"},
	})
	requester := identity.Requester{UserID: "user-1", Role: identity.RoleUser}
	upload := func(expected int64) error {
		_, err := service.Create(context.Background(), requester, UploadInput{
			Filename: "game.sfc", Content: bytes.NewReader([]byte("abc")), ExpectedSize: expected,
		})
		return err
	}
	if err := upload(1 << 20); !fault.IsCode(err, fault.CodeInsufficientStorage) {
		t.Fatalf("upload larger than free space: %v, want insufficient storage", err)
	}
	if storage.staged != 0 {
		t.Fatal("content was staged although it could not fit")
	}
	// Fitting uploads, and uploads of unknown size, go ahead.
	for _, expected := range []int64{50, 0} {
		if err := upload(expected); err != nil && !fault.IsCode(err, fault.CodeROMAlreadyExists) {
			t.Fatalf("upload of %d bytes: %v", expected, err)
		}
	}
	// An announced size beyond the upload limit is left to the limit, reported as too large.
	service.policy.MaxUploadBytes = 10
	if err := upload(1 << 30); fault.IsCode(err, fault.CodeInsufficientStorage) {
		t.Fatalf("over-limit upload reported as a full disk")
	}
}

// A file that no title uses is what a failed identification leaves behind. Uploading it again
// must reach the library rather than be refused as a copy of something the library doesn't show.
func TestUploadServiceIdentifiesAStoredFileNoTitleUsesAgain(t *testing.T) {
	hasher := romhash.New()
	romFileHash, err := hasher.Hash([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	romRepository := &testROMRepository{existing: &romdomain.ROMFile{ID: "rf-existing", Hash: romFileHash}, unused: true}
	uploads := &testUploadRepository{}
	storage := &testStorage{}
	queue := &testQueue{}
	service := NewUploadService(romRepository, uploads, storage, hasher, queue, &testAudit{}, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})

	accepted, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Content:  bytes.NewReader([]byte("abc")),
	})
	if err != nil {
		t.Fatalf("Create() = %v, want the upload accepted", err)
	}
	if accepted.UploadID == "" || len(uploads.jobs) != 1 || uploads.jobs[0].Kind != jobsdomain.KindIdentifyROM || uploads.jobs[0].ROMFileID != "rf-existing" {
		t.Fatalf("accepted = %+v, jobs = %+v; want identification queued for the stored file", accepted, uploads.jobs)
	}
	if uploads.saved[0].UploaderID != "user-1" || uploads.saved[0].OriginalFilename != "game.sfc" {
		t.Fatalf("upload = %+v, want the new upload's owner and name", uploads.saved[0])
	}
	// The identical content is already stored: nothing new is written or recorded.
	if storage.writes != 0 || storage.pending() != 0 || len(romRepository.saved) != 0 {
		t.Fatalf("storage writes=%d pending=%d rom files saved=%d, want none", storage.writes, storage.pending(), len(romRepository.saved))
	}
}

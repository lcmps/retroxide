package uploads

import (
	"context"
	"errors"
	"testing"

	romhash "github.com/lcmps/retroxide/internal/adapters/hash"
	"github.com/lcmps/retroxide/internal/features/jobs"
	"github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type testROMRepository struct {
	existing  *rom.ROMFile
	saved     []rom.ROMFile
	upsertErr error
}

func (repository *testROMRepository) FindROMFileBySHA256(_ context.Context, _ string) (*rom.ROMFile, error) {
	return repository.existing, nil
}

func (repository *testROMRepository) UpsertROMFile(_ context.Context, romFile rom.ROMFile) (rom.ROMFile, error) {
	if repository.upsertErr != nil {
		return rom.ROMFile{}, repository.upsertErr
	}
	repository.saved = append(repository.saved, romFile)
	return romFile, nil
}

type testUploadRepository struct {
	saved []Upload
	err   error
}

func (repository *testUploadRepository) SaveUpload(_ context.Context, upload Upload) error {
	if repository.err != nil {
		return repository.err
	}
	repository.saved = append(repository.saved, upload)
	return nil
}

func (repository *testUploadRepository) FindUpload(_ context.Context, _ string) (*Upload, error) {
	return nil, nil
}

type testStorage struct {
	writes       int
	deletes      []string
	deleteCtxErr error
}

func (storage *testStorage) Write(_ context.Context, contentAddress string, _ []byte) (string, error) {
	storage.writes++
	return "/storage/" + contentAddress, nil
}

func (storage *testStorage) Delete(ctx context.Context, storagePath string) error {
	storage.deletes = append(storage.deletes, storagePath)
	storage.deleteCtxErr = ctx.Err()
	return nil
}

type testQueue struct {
	payloads []jobs.Payload
	err      error
}

func (queue *testQueue) Enqueue(_ context.Context, payload jobs.Payload) error {
	if queue.err != nil {
		return queue.err
	}
	queue.payloads = append(queue.payloads, payload)
	return nil
}

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
		Bytes:    []byte("abc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" || len(queue.payloads) != 1 || len(uploadRepository.saved) != 1 {
		t.Fatalf("upload result = %+v, queued=%d uploads=%d", result, len(queue.payloads), len(uploadRepository.saved))
	}
	if uploadRepository.saved[0].UploaderID != "user-1" || uploadRepository.saved[0].Status != UploadStatusProcessing {
		t.Fatalf("persisted upload = %+v", uploadRepository.saved[0])
	}
	if queue.payloads[0].Kind != "identify_rom" || queue.payloads[0].Attempt != 0 {
		t.Fatalf("queued payload = %+v", queue.payloads[0])
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
	romRepository := &testROMRepository{existing: &rom.ROMFile{ID: "rf-existing", Hash: romFileHash}}
	storage := &testStorage{}
	queue := &testQueue{}
	audit := &testAudit{}
	service := NewUploadService(romRepository, &testUploadRepository{}, storage, hasher, queue, audit, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})

	_, err = service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Bytes:    []byte("abc"),
	})
	if !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("Create() error = %v, want duplicate conflict", err)
	}
	if storage.writes != 0 || len(queue.payloads) != 0 || audit.duplicate != 1 {
		t.Fatalf("storage writes=%d queued=%d duplicate audits=%d", storage.writes, len(queue.payloads), audit.duplicate)
	}
}

func TestUploadServiceCreateCompensatesWhenEnqueueFails(t *testing.T) {
	queueError := errors.New("queue unavailable")
	uploadRepository := &testUploadRepository{}
	storage := &testStorage{}
	service := NewUploadService(&testROMRepository{}, uploadRepository, storage, romhash.New(), &testQueue{err: queueError}, nil, UploadPolicy{
		MaxUploadBytes:    1024,
		AllowedExtensions: []string{"sfc"},
	})

	_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
		Filename: "game.sfc",
		Bytes:    []byte("abc"),
	})
	if !errors.Is(err, queueError) {
		t.Fatalf("Create() error = %v, want queue error", err)
	}
	if len(uploadRepository.saved) != 2 || uploadRepository.saved[1].Status != UploadStatusFailed {
		t.Fatalf("saved upload states = %+v, want processing then failed", uploadRepository.saved)
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
				Bytes:    test.content,
			})
			if !fault.IsCode(err, test.wantCode) {
				t.Fatalf("Create() error = %v, want code %q", err, test.wantCode)
			}
			if storage.writes != 0 {
				t.Fatalf("storage writes = %d, want 0", storage.writes)
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
		Bytes:    []byte("abc"),
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
		Bytes:    []byte("abc"),
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
		Bytes:    []byte("abc"),
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

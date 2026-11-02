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
	err   error
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

func (storage *testStorage) Stage(_ context.Context, content io.Reader) (StagedContent, error) {
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

type testQueue struct {
	payloads []jobsdomain.Payload
	err      error
}

func (queue *testQueue) Enqueue(_ context.Context, payload jobsdomain.Payload) error {
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
		Content:  bytes.NewReader([]byte("abc")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" || len(queue.payloads) != 1 || len(uploadRepository.saved) != 1 {
		t.Fatalf("upload result = %+v, queued=%d uploads=%d", result, len(queue.payloads), len(uploadRepository.saved))
	}
	if uploadRepository.saved[0].UploaderID != "user-1" || uploadRepository.saved[0].Status != uploadsdomain.UploadStatusProcessing {
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
	if storage.writes != 0 || storage.pending() != 0 || len(queue.payloads) != 0 || audit.duplicate != 1 {
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
		Content:  bytes.NewReader([]byte("abc")),
	})
	if !errors.Is(err, queueError) {
		t.Fatalf("Create() error = %v, want queue error", err)
	}
	if len(uploadRepository.saved) != 2 || uploadRepository.saved[1].Status != uploadsdomain.UploadStatusFailed {
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

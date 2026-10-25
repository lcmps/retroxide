package uploads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lcmps/retroxide/internal/features/jobs"
	"github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/platform/id"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type uploadROMRepository interface {
	FindROMFileBySHA256(context.Context, string) (*rom.ROMFile, error)
	UpsertROMFile(context.Context, rom.ROMFile) (rom.ROMFile, error)
}

type uploadRecordRepository interface {
	SaveUpload(context.Context, Upload) error
	FindUpload(context.Context, string) (*Upload, error)
}

type uploadStorage interface {
	Write(context.Context, string, []byte) (string, error)
	Delete(context.Context, string) error
}

type uploadHasher interface {
	Hash([]byte) (rom.ROMHash, error)
}

type uploadQueue interface {
	Enqueue(context.Context, jobs.Payload) error
}

type uploadAudit interface {
	UploadAccepted(context.Context, string, string, string, uint64)
	DuplicateUpload(context.Context, string, string)
}

type UploadPolicy struct {
	MaxUploadBytes    int64
	AllowedMIMETypes  []string
	AllowedExtensions []string
}

type UploadService struct {
	romRepository    uploadROMRepository
	uploadRepository uploadRecordRepository
	storage          uploadStorage
	hasher           uploadHasher
	queue            uploadQueue
	audit            uploadAudit
	policy           UploadPolicy
}

func NewUploadService(
	romRepository uploadROMRepository,
	uploadRepository uploadRecordRepository,
	storage uploadStorage,
	hasher uploadHasher,
	queue uploadQueue,
	audit uploadAudit,
	policy UploadPolicy,
) *UploadService {
	return &UploadService{
		romRepository:    romRepository,
		uploadRepository: uploadRepository,
		storage:          storage,
		hasher:           hasher,
		queue:            queue,
		audit:            audit,
		policy:           policy,
	}
}

func (service *UploadService) Create(ctx context.Context, requester identity.Requester, input UploadInput) (UploadAcceptedDTO, error) {
	if len(input.Bytes) == 0 {
		return UploadAcceptedDTO{}, fault.Validation("empty upload")
	}
	if int64(len(input.Bytes)) > service.policy.MaxUploadBytes {
		return UploadAcceptedDTO{}, fault.UploadTooLarge(int64(len(input.Bytes)), service.policy.MaxUploadBytes)
	}
	if err := ValidateFilename(input.Filename); err != nil {
		return UploadAcceptedDTO{}, err
	}
	if !service.isAllowedUploadType(input.Filename, input.MIMEType) {
		return UploadAcceptedDTO{}, fault.UnsupportedUploadType("mime type or extension is not allowed")
	}

	hash, err := service.hasher.Hash(input.Bytes)
	if err != nil {
		return UploadAcceptedDTO{}, fault.Internal(fmt.Errorf("hashing upload: %w", err))
	}

	existing, err := service.romRepository.FindROMFileBySHA256(ctx, hash.SHA256)
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	if existing != nil {
		if existing.Hash != hash {
			return UploadAcceptedDTO{}, fault.Internal(errors.New("sha256 lookup returned different hashes"))
		}
		if service.audit != nil {
			service.audit.DuplicateUpload(ctx, existing.ID, hash.SHA256)
		}
		return UploadAcceptedDTO{}, fault.ROMAlreadyExists(existing.ID, hash.SHA256)
	}

	storagePath, err := service.storage.Write(ctx, hash.SHA256, input.Bytes)
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	removeStoredContent := func() {
		cleanupContext := context.WithoutCancel(ctx)
		if deleteErr := service.storage.Delete(cleanupContext, storagePath); deleteErr != nil {
			slog.ErrorContext(cleanupContext, "failed to remove orphaned rom bytes", "storage_path", storagePath, "error", deleteErr)
		}
	}

	romFileID := "rf_" + hash.SHA256
	romFile := rom.ROMFile{
		ID:          romFileID,
		Hash:        hash,
		Size:        uint64(len(input.Bytes)),
		StoragePath: storagePath,
	}
	savedROMFile, err := service.romRepository.UpsertROMFile(ctx, romFile)
	if err != nil {
		if fault.IsCode(err, fault.CodeROMAlreadyExists) {
			return UploadAcceptedDTO{}, err
		}
		removeStoredContent()
		return UploadAcceptedDTO{}, err
	}

	uploadID, err := id.New("upl")
	if err != nil {
		removeStoredContent()
		return UploadAcceptedDTO{}, fault.Internal(fmt.Errorf("generating upload id: %w", err))
	}
	upload := Upload{
		ID:               uploadID,
		UploaderID:       requester.UserID,
		OriginalFilename: input.Filename,
		Status:           UploadStatusProcessing,
	}
	if err := service.uploadRepository.SaveUpload(ctx, upload); err != nil {
		removeStoredContent()
		return UploadAcceptedDTO{}, err
	}

	if err := service.queue.Enqueue(ctx, jobs.Payload{
		Kind:      "identify_rom",
		UploadID:  uploadID,
		ROMFileID: savedROMFile.ID,
	}); err != nil {
		upload.Status = UploadStatusFailed
		message := err.Error()
		upload.ErrorMessage = &message
		if saveErr := service.uploadRepository.SaveUpload(ctx, upload); saveErr != nil {
			slog.ErrorContext(ctx, "failed to mark upload failed after enqueue error", "upload_id", uploadID, "error", saveErr)
		}
		removeStoredContent()
		return UploadAcceptedDTO{}, err
	}

	if service.audit != nil {
		service.audit.UploadAccepted(ctx, uploadID, savedROMFile.ID, savedROMFile.Hash.SHA256, savedROMFile.Size)
	}
	return UploadAcceptedDTO{UploadID: uploadID, Status: string(UploadStatusProcessing)}, nil
}

func (service *UploadService) Status(ctx context.Context, requester identity.Requester, uploadID string) (UploadStatusDTO, error) {
	upload, err := service.uploadRepository.FindUpload(ctx, uploadID)
	if err != nil {
		return UploadStatusDTO{}, err
	}
	if upload == nil || !requester.CanAccess(upload.UploaderID) {
		return UploadStatusDTO{}, fault.NotFound()
	}
	return UploadStatusDTO{
		UploadID: upload.ID,
		Status:   string(upload.Status),
		ROMID:    upload.ROMID,
		Error:    upload.ErrorMessage,
	}, nil
}

func (service *UploadService) isAllowedUploadType(filename string, mimeType *string) bool {
	if extensionIndex := strings.LastIndexByte(filename, '.'); extensionIndex >= 0 {
		extension := strings.ToLower(filename[extensionIndex+1:])
		for _, allowed := range service.policy.AllowedExtensions {
			if extension == allowed {
				return true
			}
		}
	}
	if mimeType != nil {
		mime := strings.ToLower(*mimeType)
		for _, allowed := range service.policy.AllowedMIMETypes {
			if mime == allowed {
				return true
			}
		}
	}
	return false
}

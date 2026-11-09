package uploads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

type uploadROMRepository interface {
	FindROMFileBySHA256(context.Context, string) (*romdomain.ROMFile, error)
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	// CountROMsByROMFileID counts the ROMs using a stored file as one of their discs.
	CountROMsByROMFileID(context.Context, string) (uint64, error)
}

// libraryShows reports whether a stored file belongs to a ROM. One that doesn't is left by an
// upload whose identification failed: the library never shows it, so it is not a duplicate.
func (service *UploadService) libraryShows(ctx context.Context, romFile romdomain.ROMFile) (bool, error) {
	uses, err := service.romRepository.CountROMsByROMFileID(ctx, romFile.ID)
	return uses > 0, err
}

type uploadRecordRepository interface {
	SaveUpload(context.Context, uploadsdomain.Upload) error
	// SaveUploadWithJob records a new upload and queues its first job atomically.
	SaveUploadWithJob(context.Context, uploadsdomain.Upload, jobsdomain.Payload) error
	FindUpload(context.Context, string) (*uploadsdomain.Upload, error)
}

type uploadStorage interface {
	// Stage writes content to temporary storage; sizeHint is its expected size, or 0.
	Stage(ctx context.Context, content io.Reader, sizeHint int64) (StagedContent, error)
	Delete(ctx context.Context, storagePath string) error
}

// multipartAllowance covers the multipart framing counted in an upload's announced size. Larger
// announced sizes are left to the size limit to reject, so they report as too large rather than
// as a full disk.
const multipartAllowance = 1 << 20

// roomChecker is implemented by storage that can tell whether content fits on its disk.
type roomChecker interface {
	HasRoomFor(size int64) bool
}

type uploadHasher interface {
	NewDigest() romdomain.Digest
}

// workerWaker wakes the background workers once a job has been committed.
type workerWaker interface {
	Notify()
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
	romRepository     uploadROMRepository
	uploadRepository  uploadRecordRepository
	storage           uploadStorage
	hasher            uploadHasher
	workers           workerWaker
	audit             uploadAudit
	policy            UploadPolicy
	sessions          *sessionSupport
	discs             DiscAssembler
	wakeCompressor    func()
	compressByDefault bool
	known             KnownList
	bioses            BIOSLookup
}

func NewUploadService(
	romRepository uploadROMRepository,
	uploadRepository uploadRecordRepository,
	storage uploadStorage,
	hasher uploadHasher,
	workers workerWaker,
	audit uploadAudit,
	policy UploadPolicy,
) *UploadService {
	return &UploadService{
		romRepository:    romRepository,
		uploadRepository: uploadRepository,
		storage:          storage,
		hasher:           hasher,
		workers:          workers,
		audit:            audit,
		policy:           policy,
	}
}

func (service *UploadService) Create(ctx context.Context, requester identity.Requester, input UploadInput) (UploadAcceptedDTO, error) {
	if err := ValidateFilename(input.Filename); err != nil {
		return UploadAcceptedDTO{}, err
	}
	limit := service.policy.MaxUploadBytes
	if input.BIOS.Enabled {
		// The request carries a little multipart framing beyond the file.
		if err := service.checkBIOSUpload(input.BIOS, input.Filename, max(input.ExpectedSize-multipartAllowance, 0)); err != nil {
			return UploadAcceptedDTO{}, err
		}
		limit = service.biosUploadLimit()
	} else if !service.isAllowedUploadType(input.Filename, input.MIMEType) {
		return UploadAcceptedDTO{}, fault.UnsupportedUploadType("mime type or extension is not allowed")
	}
	if input.Content == nil {
		return UploadAcceptedDTO{}, fault.Validation("empty upload")
	}

	if checker, ok := service.storage.(roomChecker); ok && input.ExpectedSize > 0 &&
		input.ExpectedSize <= limit+multipartAllowance && !checker.HasRoomFor(input.ExpectedSize) {
		return UploadAcceptedDTO{}, fault.InsufficientStorage()
	}

	// Stage and hash in one pass, reading at most one byte past the limit to detect oversize uploads.
	digest := service.hasher.NewDigest()
	sizeHint := input.ExpectedSize
	if sizeHint > limit+multipartAllowance {
		sizeHint = 0 // an over-limit upload is rejected; reserve nothing for it
	}
	staged, err := service.storage.Stage(ctx, io.TeeReader(io.LimitReader(input.Content, limit+1), digest), sizeHint)
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	committed := false
	defer func() {
		if !committed {
			staged.Discard()
		}
	}()
	size := staged.Size()
	if size == 0 {
		return UploadAcceptedDTO{}, fault.Validation("empty upload")
	}
	if size > limit {
		return UploadAcceptedDTO{}, fault.UploadTooLarge(size, limit)
	}
	hash, err := digest.Sum()
	if err != nil {
		return UploadAcceptedDTO{}, fault.Internal(fmt.Errorf("hashing upload: %w", err))
	}
	accepted, stored, err := service.accept(ctx, requester, input.Filename, input.BIOS, hash, size, staged.Commit)
	committed = stored
	return accepted, err
}

// accept records hashed upload content as a new ROM file and queues it for identification.
// commit moves the content to its address; stored reports whether it did, after which the
// content is the ROM file's (or was removed again if recording it failed). Duplicates are
// rejected before commit.
func (service *UploadService) accept(
	ctx context.Context,
	requester identity.Requester,
	filename string,
	bios BIOSOptions,
	hash romdomain.ROMHash,
	size int64,
	commit func(ctx context.Context, contentAddress string) (string, error),
) (accepted UploadAcceptedDTO, stored bool, err error) {
	existing, err := service.romRepository.FindROMFileBySHA256(ctx, hash.SHA256)
	if err != nil {
		return UploadAcceptedDTO{}, false, err
	}
	if existing != nil {
		if !existing.Hash.MatchesContent(hash) {
			return UploadAcceptedDTO{}, false, fault.Internal(errors.New("sha256 lookup returned different hashes"))
		}
		// A file already kept as a BIOS is a duplicate whatever this upload is; a file kept only
		// as a game can still become a BIOS, reusing the stored content.
		existingBIOS, err := service.existingBIOS(ctx, *existing)
		if err != nil {
			return UploadAcceptedDTO{}, false, err
		}
		if existingBIOS != nil {
			if service.audit != nil {
				service.audit.DuplicateUpload(ctx, existing.ID, hash.SHA256)
			}
			return UploadAcceptedDTO{}, false, fault.BIOSAlreadyExists(existingBIOS.ID, hash.SHA256)
		}
		if service.isBIOSTarget(bios, hash, size) {
			accepted, err := service.record(ctx, requester, filename, bios, *existing)
			return accepted, false, err
		}
		shown, err := service.libraryShows(ctx, *existing)
		if err != nil {
			return UploadAcceptedDTO{}, false, err
		}
		if !shown {
			// The same content is already stored, so this upload's copy is not kept: the stored
			// file is identified again for this upload.
			accepted, err := service.record(ctx, requester, filename, bios, *existing)
			return accepted, false, err
		}
		if service.audit != nil {
			service.audit.DuplicateUpload(ctx, existing.ID, hash.SHA256)
		}
		return UploadAcceptedDTO{}, false, fault.ROMAlreadyExists(existing.ID, hash.SHA256)
	}

	storagePath, err := commit(ctx, hash.SHA256)
	if err != nil {
		return UploadAcceptedDTO{}, false, err
	}
	removeStoredContent := func() {
		cleanupContext := context.WithoutCancel(ctx)
		if deleteErr := service.storage.Delete(cleanupContext, storagePath); deleteErr != nil {
			slog.ErrorContext(cleanupContext, "failed to remove orphaned rom bytes", "storage_path", storagePath, "error", deleteErr)
		}
	}

	romFileID := "rf_" + hash.SHA256
	romFile := romdomain.ROMFile{
		ID:          romFileID,
		Hash:        hash,
		Size:        uint64(size),
		StoragePath: storagePath,
	}
	savedROMFile, err := service.romRepository.UpsertROMFile(ctx, romFile)
	if err != nil {
		if fault.IsCode(err, fault.CodeROMAlreadyExists) {
			return UploadAcceptedDTO{}, true, err
		}
		removeStoredContent()
		return UploadAcceptedDTO{}, true, err
	}
	accepted, err = service.record(ctx, requester, filename, bios, savedROMFile)
	if err != nil {
		removeStoredContent()
		return UploadAcceptedDTO{}, true, err
	}
	return accepted, true, nil
}

// record saves the upload of a stored ROM file and queues its identification.
func (service *UploadService) record(ctx context.Context, requester identity.Requester, filename string, bios BIOSOptions, romFile romdomain.ROMFile) (UploadAcceptedDTO, error) {
	uploadID, err := id.New("upl")
	if err != nil {
		return UploadAcceptedDTO{}, fault.Internal(fmt.Errorf("generating upload id: %w", err))
	}
	upload := uploadsdomain.Upload{
		ID:               uploadID,
		UploaderID:       requester.UserID,
		OriginalFilename: filename,
		Status:           uploadsdomain.UploadStatusProcessing,
	}
	if bios.Enabled {
		platform := bios.Platform
		upload.BIOS, upload.BIOSPlatform = true, &platform
	}
	if err := service.uploadRepository.SaveUploadWithJob(ctx, upload, jobsdomain.Payload{
		Kind:      jobsdomain.KindIdentifyROM,
		UploadID:  uploadID,
		ROMFileID: romFile.ID,
	}); err != nil {
		return UploadAcceptedDTO{}, err
	}
	if service.workers != nil {
		service.workers.Notify()
	}
	if service.audit != nil {
		service.audit.UploadAccepted(ctx, uploadID, romFile.ID, romFile.Hash.SHA256, romFile.Size)
	}
	return UploadAcceptedDTO{UploadID: uploadID, Status: string(uploadsdomain.UploadStatusProcessing)}, nil
}

func (service *UploadService) Status(ctx context.Context, requester identity.Requester, uploadID string) (UploadStatusDTO, error) {
	upload, err := service.uploadRepository.FindUpload(ctx, uploadID)
	if err != nil {
		return UploadStatusDTO{}, err
	}
	if upload == nil || !requester.CanAccess(upload.UploaderID) {
		return UploadStatusDTO{}, fault.NotFound()
	}
	result, err := service.biosResult(ctx, upload.BIOSID)
	if err != nil {
		return UploadStatusDTO{}, err
	}
	return UploadStatusDTO{
		UploadID: upload.ID,
		Status:   string(upload.Status),
		ROMID:    upload.ROMID,
		Error:    upload.ErrorMessage,
		BIOSID:   upload.BIOSID,
		BIOS:     result,
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

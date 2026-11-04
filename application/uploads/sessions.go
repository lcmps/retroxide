package uploads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/lcmps/retroxide/application/discs"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

// MaxChunkBytes is the largest part of a session upload accepted in one request.
const MaxChunkBytes = 64 << 20

// SessionRepository stores uploads sent in parts.
type SessionRepository interface {
	SaveUploadSession(context.Context, uploadsdomain.Session) error
	FindUploadSession(context.Context, string) (*uploadsdomain.Session, error)
	// AdvanceUploadSession records session's new progress if it still had received bytes.
	AdvanceUploadSession(ctx context.Context, session uploadsdomain.Session, received int64) (bool, error)
	DeleteUploadSession(context.Context, string) error
	ListIdleUploadSessions(ctx context.Context, before time.Time) ([]string, error)
	SaveUploadPart(context.Context, uploadsdomain.Part) error
	FindUploadParts(ctx context.Context, uploaderID string, ids []string) ([]uploadsdomain.Part, error)
	DeleteUploadParts(ctx context.Context, ids []string) error
}

// PartialStorage holds uploads sent in parts until they are complete.
type PartialStorage interface {
	ReservePartial(ctx context.Context, sessionID string, size int64) error
	AppendPartial(ctx context.Context, sessionID string, offset int64, content io.Reader) (int64, error)
	CommitPartial(ctx context.Context, sessionID, contentAddress string) (string, error)
	DiscardPartial(sessionID string) error
}

// ResumableHasher hashes uploads sent in parts, saving progress between them.
type ResumableHasher interface {
	// ResumeDigest continues from saved progress, or starts afresh when state is empty.
	ResumeDigest(state []byte) (romdomain.ResumableDigest, error)
}

type sessionSupport struct {
	repository SessionRepository
	partials   PartialStorage
	hasher     ResumableHasher
	mu         sync.Mutex
	busy       map[string]bool
}

// WithSessions lets the service take uploads in parts that resume after a dropped connection.
func (service *UploadService) WithSessions(repository SessionRepository, partials PartialStorage, hasher ResumableHasher) *UploadService {
	service.sessions = &sessionSupport{repository: repository, partials: partials, hasher: hasher, busy: map[string]bool{}}
	return service
}

// CreateSession starts an upload sent in parts, reserving disk space for all of it.
func (service *UploadService) CreateSession(ctx context.Context, requester identity.Requester, input CreateSessionInput) (SessionDTO, error) {
	sessions, err := service.sessionSupport()
	if err != nil {
		return SessionDTO{}, err
	}
	if err := ValidateFilename(input.Filename); err != nil {
		return SessionDTO{}, err
	}
	// A disc's track files are checked against its sheet when the disc is assembled.
	if !input.Part && !service.isAllowedUploadType(input.Filename, input.MIMEType) {
		return SessionDTO{}, fault.UnsupportedUploadType("mime type or extension is not allowed")
	}
	if input.Size <= 0 {
		return SessionDTO{}, fault.Validation("size must be greater than zero")
	}
	if input.Size > service.policy.MaxUploadBytes {
		return SessionDTO{}, fault.UploadTooLarge(input.Size, service.policy.MaxUploadBytes)
	}
	if checker, ok := service.storage.(roomChecker); ok && !checker.HasRoomFor(input.Size) {
		return SessionDTO{}, fault.InsufficientStorage()
	}
	digest, err := sessions.hasher.ResumeDigest(nil)
	if err != nil {
		return SessionDTO{}, fault.Internal(err)
	}
	state, err := digest.MarshalBinary()
	if err != nil {
		return SessionDTO{}, fault.Internal(err)
	}
	sessionID, err := id.New("ups")
	if err != nil {
		return SessionDTO{}, fault.Internal(fmt.Errorf("generating upload session id: %w", err))
	}
	if err := sessions.partials.ReservePartial(ctx, sessionID, input.Size); err != nil {
		return SessionDTO{}, err
	}
	session := uploadsdomain.Session{
		ID: sessionID, UploaderID: requester.UserID, Filename: input.Filename, MIMEType: input.MIMEType,
		Size: input.Size, HashState: state, UpdatedAt: time.Now(), Part: input.Part,
	}
	if err := sessions.repository.SaveUploadSession(ctx, session); err != nil {
		_ = sessions.partials.DiscardPartial(sessionID)
		return SessionDTO{}, err
	}
	return sessionDTO(session, nil), nil
}

// SessionStatus reports how much of an upload sent in parts has arrived.
func (service *UploadService) SessionStatus(ctx context.Context, requester identity.Requester, sessionID string) (SessionDTO, error) {
	sessions, err := service.sessionSupport()
	if err != nil {
		return SessionDTO{}, err
	}
	session, err := sessions.find(ctx, requester, sessionID)
	if err != nil {
		return SessionDTO{}, err
	}
	return sessionDTO(*session, nil), nil
}

// AppendToSession adds the part of an upload starting at offset, which must be the number of
// bytes received so far. A part that fails midway changes nothing, so the client resends it.
// Once every byte has arrived the upload is accepted like a single-request upload; sending an
// empty part at the end retries that step if it failed.
func (service *UploadService) AppendToSession(ctx context.Context, requester identity.Requester, sessionID string, offset int64, content io.Reader) (SessionDTO, error) {
	sessions, err := service.sessionSupport()
	if err != nil {
		return SessionDTO{}, err
	}
	release, err := sessions.claim(sessionID)
	if err != nil {
		return SessionDTO{}, err
	}
	defer release()
	session, err := sessions.find(ctx, requester, sessionID)
	if err != nil {
		return SessionDTO{}, err
	}
	if offset != session.Received {
		return SessionDTO{}, &fault.Error{Code: fault.CodeConflict, Message: fmt.Sprintf("the upload has %d bytes; send the part starting there", session.Received)}
	}
	digest, err := sessions.hasher.ResumeDigest(session.HashState)
	if err != nil {
		return SessionDTO{}, fault.Internal(fmt.Errorf("restoring upload hash progress: %w", err))
	}
	if !session.Complete() {
		remaining := session.Size - session.Received
		written, err := sessions.partials.AppendPartial(ctx, session.ID, offset, io.TeeReader(io.LimitReader(content, remaining+1), digest))
		if err != nil {
			return SessionDTO{}, err
		}
		if written > remaining {
			return SessionDTO{}, fault.Validation("the part goes past the upload's declared size")
		}
		if written > 0 {
			if session.HashState, err = digest.MarshalBinary(); err != nil {
				return SessionDTO{}, fault.Internal(err)
			}
			session.Received += written
			session.UpdatedAt = time.Now()
			advanced, err := sessions.repository.AdvanceUploadSession(ctx, *session, offset)
			if err != nil {
				return SessionDTO{}, err
			}
			if !advanced {
				return SessionDTO{}, fault.Conflict()
			}
		}
	}
	if !session.Complete() {
		return sessionDTO(*session, nil), nil
	}
	return service.finishSession(ctx, requester, sessions, *session, digest)
}

// finishSession accepts a complete session upload. The session ends once its content is
// stored or turns out to be a duplicate; after other failures it stays, so the client can retry.
func (service *UploadService) finishSession(ctx context.Context, requester identity.Requester, sessions *sessionSupport, session uploadsdomain.Session, digest romdomain.Digest) (SessionDTO, error) {
	hash, err := digest.Sum()
	if err != nil {
		return SessionDTO{}, fault.Internal(fmt.Errorf("hashing upload: %w", err))
	}
	if session.Part {
		return service.finishPart(ctx, sessions, session, hash)
	}
	accepted, stored, err := service.accept(ctx, requester, session.Filename, hash, session.Size, func(ctx context.Context, address string) (string, error) {
		return sessions.partials.CommitPartial(ctx, session.ID, address)
	})
	if stored || err == nil || fault.IsCode(err, fault.CodeROMAlreadyExists) {
		sessions.end(context.WithoutCancel(ctx), session.ID)
	}
	if err != nil {
		return SessionDTO{}, err
	}
	return sessionDTO(session, &accepted), nil
}

// CheckSessionDuplicate compares the SHA-256 the client computed of the whole file with the
// library, so a ROM the library already has stops uploading early instead of after its last
// byte. A duplicate ends the session and reports rom_already_exists; otherwise the upload goes on
// (the server still hashes every byte itself, so a wrong hash cannot change what is stored).
func (service *UploadService) CheckSessionDuplicate(ctx context.Context, requester identity.Requester, sessionID, sha256 string) error {
	sessions, err := service.sessionSupport()
	if err != nil {
		return err
	}
	if !isSHA256(sha256) {
		return fault.Validation("sha256 must be 64 lowercase hex digits")
	}
	release, err := sessions.claim(sessionID)
	if err != nil {
		return err
	}
	defer release()
	if _, err := sessions.find(ctx, requester, sessionID); err != nil {
		return err
	}
	existing, err := service.romRepository.FindROMFileBySHA256(ctx, sha256)
	if err != nil || existing == nil {
		return err
	}
	if service.audit != nil {
		service.audit.DuplicateUpload(ctx, existing.ID, sha256)
	}
	sessions.end(ctx, sessionID)
	return fault.ROMAlreadyExists(existing.ID, sha256)
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

// finishPart stores a complete disc file as a part, for AssembleDisc to use.
func (service *UploadService) finishPart(ctx context.Context, sessions *sessionSupport, session uploadsdomain.Session, hash romdomain.ROMHash) (SessionDTO, error) {
	key, err := sessions.partials.CommitPartial(ctx, session.ID, hash.SHA256)
	if err != nil {
		return SessionDTO{}, err
	}
	partID, err := id.New("upp")
	if err != nil {
		return SessionDTO{}, fault.Internal(err)
	}
	part := uploadsdomain.Part{
		ID: partID, UploaderID: session.UploaderID, Filename: session.Filename, StoragePath: key, Size: session.Size,
		SHA256: hash.SHA256, SHA1: hash.SHA1, MD5: hash.MD5, CRC32: hash.CRC32, CreatedAt: time.Now(),
	}
	if err := sessions.repository.SaveUploadPart(ctx, part); err != nil {
		return SessionDTO{}, err
	}
	sessions.end(context.WithoutCancel(ctx), session.ID)
	dto := sessionDTO(session, nil)
	dto.PartID = partID
	return dto, nil
}

// DiscAssembler turns a disc's sheet and uploaded files into a stored disc.
type DiscAssembler interface {
	// Assemble stores the disc, asking for it to be compressed to CHD when compress is set.
	Assemble(ctx context.Context, sheetName, sheetText string, parts []discs.Part, compress bool) (romdomain.ROMFile, error)
	// CanCompress reports whether discs can be compressed to CHD (chdman is installed).
	CanCompress() bool
}

// maxSheetBytes bounds a CUE sheet or GDI file; real ones are a few kilobytes.
const maxSheetBytes = 64 << 10

// WithDiscAssembly lets the service accept discs uploaded as a sheet and track files. wake, if
// set, is called once a disc asked to be compressed is stored; compressByDefault is the
// upload page's initial choice.
func (service *UploadService) WithDiscAssembly(assembler DiscAssembler, wake func(), compressByDefault bool) *UploadService {
	service.discs, service.wakeCompressor, service.compressByDefault = assembler, wake, compressByDefault
	return service
}

// Options tells the upload page what it can offer.
func (service *UploadService) Options() UploadOptionsDTO {
	canCompress := service.discs != nil && service.discs.CanCompress()
	return UploadOptionsDTO{
		DiscCompression:        canCompress,
		DiscCompressionDefault: canCompress && service.compressByDefault,
	}
}

// AssembleDisc stores a disc from its sheet and the parts holding its track files, and queues
// it like any upload. The parts are consumed, whatever the outcome but a passing failure.
func (service *UploadService) AssembleDisc(ctx context.Context, requester identity.Requester, input AssembleDiscInput) (UploadAcceptedDTO, error) {
	sessions, err := service.sessionSupport()
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	if service.discs == nil {
		return UploadAcceptedDTO{}, fault.Internal(errors.New("disc uploads are not configured"))
	}
	if err := ValidateFilename(input.SheetName); err != nil {
		return UploadAcceptedDTO{}, err
	}
	if len(input.Sheet) == 0 || len(input.Sheet) > maxSheetBytes {
		return UploadAcceptedDTO{}, fault.Validation("the sheet must be between 1 byte and 64 KB")
	}
	if len(input.Parts) == 0 || len(input.Parts) > 99 {
		return UploadAcceptedDTO{}, fault.Validation("a disc has between 1 and 99 track files")
	}
	parts, err := sessions.repository.FindUploadParts(ctx, requester.UserID, input.Parts)
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	if len(parts) != len(input.Parts) {
		return UploadAcceptedDTO{}, fault.Validation("some of the disc's files are missing or expired; upload them again")
	}
	discParts := make([]discs.Part, len(parts))
	for index, part := range parts {
		hash, err := romdomain.NewROMHash(part.SHA256, part.SHA1, part.MD5, part.CRC32)
		if err != nil {
			return UploadAcceptedDTO{}, fault.Internal(err)
		}
		discParts[index] = discs.Part{Name: part.Filename, StoragePath: part.StoragePath, Size: part.Size, Hash: hash}
	}
	romFile, err := service.discs.Assemble(ctx, input.SheetName, input.Sheet, discParts, input.Compress)
	if err != nil {
		if fault.IsCode(err, fault.CodeROMAlreadyExists) || fault.IsCode(err, fault.CodeValidation) {
			// Nothing a retry with the same files could fix.
			_ = sessions.repository.DeleteUploadParts(context.WithoutCancel(ctx), input.Parts)
			if service.audit != nil && fault.IsCode(err, fault.CodeROMAlreadyExists) {
				service.audit.DuplicateUpload(ctx, "", "")
			}
		}
		return UploadAcceptedDTO{}, err
	}
	accepted, err := service.record(ctx, requester, input.SheetName, romFile)
	if err != nil {
		return UploadAcceptedDTO{}, err
	}
	_ = sessions.repository.DeleteUploadParts(context.WithoutCancel(ctx), input.Parts)
	if input.Compress && service.wakeCompressor != nil {
		service.wakeCompressor()
	}
	return accepted, nil
}

// CancelSession abandons an upload sent in parts and frees its space.
func (service *UploadService) CancelSession(ctx context.Context, requester identity.Requester, sessionID string) error {
	sessions, err := service.sessionSupport()
	if err != nil {
		return err
	}
	release, err := sessions.claim(sessionID)
	if err != nil {
		return err
	}
	defer release()
	if _, err := sessions.find(ctx, requester, sessionID); err != nil {
		return err
	}
	sessions.end(ctx, sessionID)
	return nil
}

// ExpireSessions ends sessions idle since before, freeing their space, and returns how many.
func (service *UploadService) ExpireSessions(ctx context.Context, before time.Time) (int, error) {
	sessions, err := service.sessionSupport()
	if err != nil {
		return 0, err
	}
	ids, err := sessions.repository.ListIdleUploadSessions(ctx, before)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, sessionID := range ids {
		release, err := sessions.claim(sessionID)
		if err != nil {
			continue // a part is arriving right now
		}
		sessions.end(ctx, sessionID)
		release()
		expired++
	}
	return expired, nil
}

func (service *UploadService) sessionSupport() (*sessionSupport, error) {
	if service.sessions == nil {
		return nil, fault.Internal(errors.New("upload sessions are not configured"))
	}
	return service.sessions, nil
}

// find returns the requester's session; other users' sessions are reported as missing.
func (sessions *sessionSupport) find(ctx context.Context, requester identity.Requester, sessionID string) (*uploadsdomain.Session, error) {
	session, err := sessions.repository.FindUploadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.UploaderID != requester.UserID {
		return nil, fault.NotFound()
	}
	return session, nil
}

// claim reserves a session for one request at a time: parts written at once would interleave.
func (sessions *sessionSupport) claim(sessionID string) (func(), error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.busy[sessionID] {
		return nil, &fault.Error{Code: fault.CodeConflict, Message: "another part of this upload is being received"}
	}
	sessions.busy[sessionID] = true
	return func() {
		sessions.mu.Lock()
		delete(sessions.busy, sessionID)
		sessions.mu.Unlock()
	}, nil
}

// end removes a session and its partial file.
func (sessions *sessionSupport) end(ctx context.Context, sessionID string) {
	if err := sessions.partials.DiscardPartial(sessionID); err != nil {
		slog.WarnContext(ctx, "could not remove partial upload", "session_id", sessionID, "error", err)
	}
	if err := sessions.repository.DeleteUploadSession(ctx, sessionID); err != nil {
		slog.WarnContext(ctx, "could not remove upload session", "session_id", sessionID, "error", err)
	}
}

func sessionDTO(session uploadsdomain.Session, accepted *UploadAcceptedDTO) SessionDTO {
	if session.Part {
		accepted = nil
	}
	return SessionDTO{SessionID: session.ID, Size: session.Size, Received: session.Received, MaxChunkSize: MaxChunkBytes, Upload: accepted}
}

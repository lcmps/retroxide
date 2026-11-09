package uploads

import (
	"context"
	"io"

	"github.com/lcmps/retroxide/application/jobs"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

type Repository interface {
	SaveUpload(context.Context, uploadsdomain.Upload) error
	FindUpload(context.Context, string) (*uploadsdomain.Upload, error)
}

type ROMRepository interface {
	FindROMFileBySHA256(context.Context, string) (*romdomain.ROMFile, error)
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error)
	SaveROM(context.Context, romdomain.ROM) error
}

type HashService interface {
	NewDigest() romdomain.Digest
}

// StagedContent is upload content written to storage but not yet visible at its content address.
type StagedContent interface {
	// Size is the number of bytes staged.
	Size() int64
	// Commit moves the content to its content address and returns the storage path.
	Commit(ctx context.Context, contentAddress string) (string, error)
	// Discard removes uncommitted content; it is a no-op after Commit.
	Discard()
}

type UploadAudit interface {
	UploadAccepted(context.Context, string, string, string, uint64)
	DuplicateUpload(context.Context, string, string)
}

type Service interface {
	Create(context.Context, identity.Requester, UploadInput) (UploadAcceptedDTO, error)
	Status(context.Context, identity.Requester, string) (UploadStatusDTO, error)
	CreateSession(context.Context, identity.Requester, CreateSessionInput) (SessionDTO, error)
	SessionStatus(context.Context, identity.Requester, string) (SessionDTO, error)
	AppendToSession(ctx context.Context, requester identity.Requester, sessionID string, offset int64, content io.Reader) (SessionDTO, error)
	CancelSession(context.Context, identity.Requester, string) error
	// CheckSessionDuplicate ends the session if the library already has the file with sha256.
	CheckSessionDuplicate(ctx context.Context, requester identity.Requester, sessionID, sha256 string) error
	// AssembleDisc stores a disc uploaded as its sheet and track files.
	AssembleDisc(context.Context, identity.Requester, AssembleDiscInput) (UploadAcceptedDTO, error)
	// Options tells the upload page what it can offer.
	Options() UploadOptionsDTO
}

var _ jobs.Identifier = (*IdentifyService)(nil)
var _ jobs.FilenameCompleter = (*IdentifyService)(nil)

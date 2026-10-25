package uploads

import (
	"context"

	"github.com/lcmps/retroxide/internal/features/jobs"
	"github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type Repository interface {
	SaveUpload(context.Context, Upload) error
	FindUpload(context.Context, string) (*Upload, error)
}

type ROMRepository interface {
	FindROMFileBySHA256(context.Context, string) (*rom.ROMFile, error)
	UpsertROMFile(context.Context, rom.ROMFile) (rom.ROMFile, error)
	FindROMFileByID(context.Context, string) (*rom.ROMFile, error)
	SaveROM(context.Context, rom.ROM) error
}

type HashService interface {
	Hash([]byte) (rom.ROMHash, error)
}

type UploadAudit interface {
	UploadAccepted(context.Context, string, string, string, uint64)
	DuplicateUpload(context.Context, string, string)
}

type Service interface {
	Create(context.Context, identity.Requester, UploadInput) (UploadAcceptedDTO, error)
	Status(context.Context, identity.Requester, string) (UploadStatusDTO, error)
}

var _ jobs.Identifier = (*IdentifyService)(nil)

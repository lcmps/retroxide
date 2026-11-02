package uploads

import (
	"context"

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
	Hash([]byte) (romdomain.ROMHash, error)
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

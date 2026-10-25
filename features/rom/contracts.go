package rom

import (
	"context"
	"io"

	"github.com/lcmps/retroxide/internal/shared/identity"
)

type ROMWithFile struct {
	ROM    ROM
	SHA256 string
}

type Repository interface {
	UpsertROMFile(context.Context, ROMFile) (ROMFile, error)
	SaveROM(context.Context, ROM) error
	ListROMsWithFiles(context.Context, int, *string, *string) ([]ROMWithFile, error)
	FindROMByID(context.Context, string) (*ROM, error)
	FindROMFileByID(context.Context, string) (*ROMFile, error)
	FindROMFileBySHA256(context.Context, string) (*ROMFile, error)
	FindROMFileByROMID(context.Context, string) (*ROMFile, error)
	CountROMsByROMFileID(context.Context, string) (uint64, error)
	DeleteROM(context.Context, string) error
	DeleteROMFile(context.Context, string) error
}

type Storage interface {
	Write(context.Context, string, []byte) (string, error)
	Read(context.Context, string) ([]byte, error)
	Open(context.Context, string) (io.ReadCloser, error)
	LastModified(context.Context, string) (*uint64, error)
	Delete(context.Context, string) error
}

type Audit interface {
	DownloadServed(context.Context, string, string, string, uint64)
}

type API interface {
	List(context.Context, int, *string, *string, *bool) (ROMListResponseDTO, error)
	Get(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	Update(context.Context, identity.Requester, string, UpdateROMRequest) (ROMDetailDTO, error)
	Delete(context.Context, identity.Requester, string) error
	Download(context.Context, identity.Requester, string) (DownloadOutput, error)
	UploadCover(context.Context, identity.Requester, string, []byte) (ROMDetailDTO, error)
	ReadCover(context.Context, identity.Requester, string) ([]byte, error)
}

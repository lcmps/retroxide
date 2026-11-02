package rom

import (
	"context"
	"io"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Repository interface {
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	SaveROM(context.Context, romdomain.ROM) error
	ListROMsWithFiles(context.Context, int, *string, *string) ([]romdomain.ROMWithFile, error)
	FindROMByID(context.Context, string) (*romdomain.ROM, error)
	FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileBySHA256(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileByROMID(context.Context, string) (*romdomain.ROMFile, error)
	CountROMsByROMFileID(context.Context, string) (uint64, error)
	DeleteROM(context.Context, string) error
	DeleteROMFile(context.Context, string) error
	HardDeleteROM(context.Context, string, func(string) error) error
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

const (
	MediaTitleScreen = "title_screen"
	MediaScreenshot  = "screenshot"
)

type ScrapeQuery struct {
	Platform string
	SHA1     string
	CRC32    string
	MD5      string
}

// ScrapeResult leaves fields empty when the source had no value; images are validated PNG, JPEG, or WebP bytes.
type ScrapeResult struct {
	Source      string
	Title       string
	Region      string
	Publisher   string
	ReleaseYear string
	Description string
	Cover       []byte
	TitleScreen []byte
	Screenshot  []byte
}

// Scraper returns nil, nil when no source recognizes the ROM.
type Scraper interface {
	Scrape(context.Context, ScrapeQuery) (*ScrapeResult, error)
}

type API interface {
	List(context.Context, int, *string, *string, *bool) (ROMListResponseDTO, error)
	Get(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	Update(context.Context, identity.Requester, string, UpdateROMRequest) (ROMDetailDTO, error)
	Delete(context.Context, identity.Requester, string) error
	Download(context.Context, identity.Requester, string) (DownloadOutput, error)
	UploadCover(context.Context, identity.Requester, string, []byte) (ROMDetailDTO, error)
	ReadCover(context.Context, identity.Requester, string) ([]byte, error)
	Scrape(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	ReadMedia(context.Context, identity.Requester, string, string) ([]byte, error)
}

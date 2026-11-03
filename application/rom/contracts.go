package rom

import (
	"context"
	"io"
	"time"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Repository interface {
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	SaveROM(context.Context, romdomain.ROM) error
	// ListROMsWithFiles returns up to limit ROMs ordered by ID that match filter.
	ListROMsWithFiles(ctx context.Context, limit int, filter ROMFilter) ([]romdomain.ROMWithFile, error)
	FindROMByID(context.Context, string) (*romdomain.ROM, error)
	FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileBySHA256(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileByROMID(context.Context, string) (*romdomain.ROMFile, error)
	// FindUploadedFilename returns the original filename of the upload that created the ROM,
	// or nil when no upload is linked to it.
	FindUploadedFilename(ctx context.Context, romID string) (*string, error)
	// FindROMIDByUploadID returns the ROM an upload created, or nil while it has none.
	FindROMIDByUploadID(ctx context.Context, uploadID string) (*string, error)
	// ListPlatforms counts ROMs per platform, restricted to one uploader when uploaderID is set.
	ListPlatforms(ctx context.Context, uploaderID *string) ([]PlatformCount, error)
	CountROMsByROMFileID(context.Context, string) (uint64, error)
	DeleteROM(context.Context, string) error
	DeleteROMFile(context.Context, string) error
	HardDeleteROM(context.Context, string, func(string) error) error
}

// PlatformCount is how many ROMs carry a platform. Platform is the stored value as first seen;
// values differing only in case are counted together.
type PlatformCount struct {
	Platform string
	Count    uint64
}

// StoredContent is an open, seekable handle on stored bytes; the caller must close it.
type StoredContent struct {
	io.ReadSeekCloser
	Size       int64
	ModifiedAt time.Time
}

// ROMFilter selects ROMs for listing; nil fields are not applied.
type ROMFilter struct {
	// AfterCursor returns only ROMs whose ID sorts after it.
	AfterCursor *string
	// Platform matches the platform case-insensitively.
	Platform *string
	// UploaderID restricts results to one owner's uploads.
	UploaderID *string
	// Search matches a case-insensitive substring of the title, platform, or ROM ID.
	Search *string
}

// ListRequest holds the optional parameters of a ROM listing.
type ListRequest struct {
	Limit      int
	Cursor     *string
	Platform   *string
	Search     *string
	Identified *bool
}

type Storage interface {
	Write(ctx context.Context, contentAddress string, content []byte) (string, error)
	Read(ctx context.Context, storagePath string) ([]byte, error)
	Open(ctx context.Context, storagePath string) (StoredContent, error)
	Delete(ctx context.Context, storagePath string) error
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
	Source string
	// Platform is the platform key the source reported (see romdomain.PlatformKeyForName).
	Platform    string
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
	List(context.Context, identity.Requester, ListRequest) (ROMListResponseDTO, error)
	Get(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	Update(context.Context, identity.Requester, string, UpdateROMRequest) (ROMDetailDTO, error)
	Delete(context.Context, identity.Requester, string) error
	Download(context.Context, identity.Requester, string) (DownloadOutput, error)
	// DownloadFilename returns the name an accessible ROM is downloaded as.
	DownloadFilename(context.Context, identity.Requester, string) (string, error)
	UploadCover(context.Context, identity.Requester, string, []byte) (ROMDetailDTO, error)
	ReadCover(context.Context, identity.Requester, string) ([]byte, error)
	Scrape(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	ReadMedia(context.Context, identity.Requester, string, string) ([]byte, error)
	// Platforms lists the platforms of the ROMs the requester can see.
	Platforms(context.Context, identity.Requester) ([]PlatformDTO, error)
	// PlatformCatalog lists every known platform, for choosing one when editing a ROM.
	PlatformCatalog() []PlatformDTO
}

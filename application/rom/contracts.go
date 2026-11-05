package rom

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Repository interface {
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	SaveROM(context.Context, romdomain.ROM) error
	// FindDiscTracks returns a disc's tracks in order; none for other files.
	FindDiscTracks(ctx context.Context, romFileID string) ([]romdomain.DiscTrack, error)
	// SetCoverThumbnail records a ROM's cover thumbnail, unless its cover has changed since.
	SetCoverThumbnail(ctx context.Context, romID, coverPath, thumbnailPath string) error
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

// SortField names a listing order. The zero value lists ROMs by ID, the order they were added in
// no particular sense, which is the cheapest to page through.
type SortField string

const (
	SortDefault   SortField = ""
	SortTitle     SortField = "title"
	SortRelease   SortField = "release"
	SortPublisher SortField = "publisher"
	SortGenre     SortField = "genre"
)

// ROMSort orders a listing by Field, ties broken by ID. ROMs without a value for Field come
// last in either direction.
type ROMSort struct {
	Field      SortField
	Descending bool
}

// ROMCursor is the position after which the next page starts: the last ROM's ID and, for a
// sorted listing, its value for the sort field (nil once the listing reached ROMs without one).
type ROMCursor struct {
	ID    string
	Value *string
}

// ROMFilter selects ROMs for listing; nil fields are not applied.
type ROMFilter struct {
	// After returns only ROMs that come after it in Sort order.
	After *ROMCursor
	Sort  ROMSort
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
	// Sort is one of the SortField values; Order is "asc" (the default) or "desc".
	Sort  string
	Order string
}

// Discs writes discs in their download formats and compresses them.
type Discs interface {
	Archive(ctx context.Context, romFile romdomain.ROMFile, base string, out io.Writer) error
	ISO(ctx context.Context, romFile romdomain.ROMFile, out io.Writer) error
	RequestCompression(ctx context.Context, romFile romdomain.ROMFile) error
	CanCompressFile(ctx context.Context, romFile romdomain.ROMFile) bool
}

// DownloadFormat names a way to download a ROM.
type DownloadFormat string

const (
	// DownloadFile is the stored file as it is: a cartridge ROM, an ISO, a CHD.
	DownloadFile DownloadFormat = "file"
	// DownloadCHD is a disc's CHD image.
	DownloadCHD DownloadFormat = "chd"
	// DownloadTracks is a zip of a disc's CUE (or GDI) sheet and track files.
	DownloadTracks DownloadFormat = "tracks"
	// DownloadISO is a single-track data disc as an ISO image.
	DownloadISO DownloadFormat = "iso"
)

// Thumbnailer makes the small cover copies library grids show.
type Thumbnailer interface {
	// Thumbnail returns a small copy of an image, or ErrThumbnailNotSmaller when the image is
	// already small enough to serve as its own thumbnail.
	Thumbnail(content []byte) ([]byte, error)
}

// ErrThumbnailNotSmaller reports that an image needs no smaller copy.
var ErrThumbnailNotSmaller = errors.New("image is already thumbnail-sized")

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
	Genre       string
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
	// DownloadAs downloads a ROM in one of the formats its details list.
	DownloadAs(ctx context.Context, requester identity.Requester, romID string, format DownloadFormat) (DownloadOutput, error)
	// RequestCompression asks for a disc kept as tracks to be compressed to CHD.
	RequestCompression(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	// DownloadFilename returns the name an accessible ROM is downloaded as.
	DownloadFilename(ctx context.Context, requester identity.Requester, romID string, format DownloadFormat) (string, error)
	UploadCover(context.Context, identity.Requester, string, []byte) (ROMDetailDTO, error)
	OpenCover(context.Context, identity.Requester, string) (Image, error)
	// OpenCoverThumbnail opens the small copy of the cover that library grids show.
	OpenCoverThumbnail(context.Context, identity.Requester, string) (Image, error)
	Scrape(context.Context, identity.Requester, string) (ROMDetailDTO, error)
	OpenMedia(context.Context, identity.Requester, string, string) (Image, error)
	// Platforms lists the platforms of the ROMs the requester can see.
	Platforms(context.Context, identity.Requester) ([]PlatformDTO, error)
	// PlatformCatalog lists every known platform, for choosing one when editing a ROM.
	PlatformCatalog() []PlatformDTO
}

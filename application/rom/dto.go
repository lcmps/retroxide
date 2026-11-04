package rom

import (
	"context"
	"io"
)

type ROMSummaryDTO struct {
	ROMID    string `json:"rom_id"`
	Title    string `json:"title"`
	Platform string `json:"platform"`
	// PlatformName is the platform's display name.
	PlatformName string  `json:"platform_name"`
	SHA256       string  `json:"sha256"`
	CoverArtURL  *string `json:"cover_art_url"`
	// CoverThumbURL is a small copy of the cover, for grids of many ROMs.
	CoverThumbURL *string `json:"cover_thumb_url"`
}

type ROMListResponseDTO struct {
	Item       []ROMSummaryDTO `json:"item"`
	NextCursor *string         `json:"next_cursor"`
}

type ROMDetailDTO struct {
	ROMID          string     `json:"rom_id"`
	Title          string     `json:"title"`
	Platform       string     `json:"platform"`
	PlatformName   string     `json:"platform_name"`
	Region         *string    `json:"region"`
	Version        *string    `json:"version"`
	Publisher      *string    `json:"publisher"`
	ReleaseYear    *string    `json:"release_year"`
	Genre          *string    `json:"genre"`
	Description    *string    `json:"description"`
	CoverArtURL    *string    `json:"cover_art_url"`
	TitleScreenURL *string    `json:"title_screen_url"`
	ScreenshotURL  *string    `json:"screenshot_url"`
	Hash           ROMHashDTO `json:"hash"`
	// Disc describes a CD or GD-ROM image's tracks; absent for other ROMs.
	Disc *DiscDTO `json:"disc,omitempty"`
	// Downloads lists the formats the ROM downloads in, the one it was uploaded in first.
	Downloads []DownloadOptionDTO `json:"downloads,omitempty"`
}

// DownloadOptionDTO is one format a ROM downloads in.
type DownloadOptionDTO struct {
	Format DownloadFormat `json:"format"`
	// Extension is the downloaded file's: .chd, .iso, .zip (for a disc's sheet and tracks), or
	// the uploaded file's own (.sfc, .gba...).
	Extension string `json:"extension"`
	// Sheet says what a .zip holds: cue or gdi.
	Sheet string `json:"sheet,omitempty"`
}

// DiscDTO is a disc image's storage and tracks.
type DiscDTO struct {
	// Format is chd for a CHD image, tracks for a sheet and track files.
	Format string         `json:"format"`
	Tracks []DiscTrackDTO `json:"tracks"`
	// CanCompress says the disc can be compressed to CHD now (it is kept as tracks and chdman
	// is installed); CompressionRequested that it is waiting for, or being, compressed.
	CanCompress          bool `json:"can_compress"`
	CompressionRequested bool `json:"compression_requested"`
}

// DiscTrackDTO is one track and the hashes of its file, as disc databases list them.
type DiscTrackDTO struct {
	Number int    `json:"number"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA1   string `json:"sha1"`
	MD5    string `json:"md5"`
	CRC32  string `json:"crc32"`
}

// PlatformDTO is a platform present in the library. Key is the value to filter listings by.
type PlatformDTO struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	ROMCount uint64 `json:"rom_count"`
}

type ROMHashDTO struct {
	SHA256 string `json:"sha256"`
	SHA1   string `json:"sha1"`
	MD5    string `json:"md5"`
	CRC32  string `json:"crc32"`
}

// UpdateROMRequest replaces a ROM's metadata. Title and platform are required; a null region or
// version clears it. Publisher, release year, genre, and description are left unchanged when omitted
// and cleared when empty.
type UpdateROMRequest struct {
	Title       string  `json:"title"`
	Platform    string  `json:"platform"`
	Region      *string `json:"region"`
	Version     *string `json:"version"`
	Publisher   *string `json:"publisher,omitempty"`
	ReleaseYear *string `json:"release_year,omitempty"`
	Genre       *string `json:"genre,omitempty"`
	Description *string `json:"description,omitempty"`
}

// DownloadOutput streams a ROM file. The caller must close Content and should call Served
// once the body, or a range of it, has actually been sent.
type DownloadOutput struct {
	Filename string
	MIMEType string
	SHA256   string
	Content  StoredContent
	// Archive, when set instead of Content, writes the download (a zip of a disc's sheet and
	// tracks) as it goes; its size is not known up front.
	Archive func(io.Writer) error
	// Served records the download in the audit log with the number of body bytes sent.
	Served func(ctx context.Context, bytesSent int64)
}

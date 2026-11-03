package rom

import "context"

type ROMSummaryDTO struct {
	ROMID    string `json:"rom_id"`
	Title    string `json:"title"`
	Platform string `json:"platform"`
	// PlatformName is the platform's display name.
	PlatformName string  `json:"platform_name"`
	SHA256       string  `json:"sha256"`
	CoverArtURL  *string `json:"cover_art_url"`
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
	Description    *string    `json:"description"`
	CoverArtURL    *string    `json:"cover_art_url"`
	TitleScreenURL *string    `json:"title_screen_url"`
	ScreenshotURL  *string    `json:"screenshot_url"`
	Hash           ROMHashDTO `json:"hash"`
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
// version clears it. Publisher, release year, and description are left unchanged when omitted
// and cleared when empty.
type UpdateROMRequest struct {
	Title       string  `json:"title"`
	Platform    string  `json:"platform"`
	Region      *string `json:"region"`
	Version     *string `json:"version"`
	Publisher   *string `json:"publisher,omitempty"`
	ReleaseYear *string `json:"release_year,omitempty"`
	Description *string `json:"description,omitempty"`
}

// DownloadOutput streams a ROM file. The caller must close Content and should call Served
// once the body, or a range of it, has actually been sent.
type DownloadOutput struct {
	Filename string
	MIMEType string
	SHA256   string
	Content  StoredContent
	// Served records the download in the audit log with the number of body bytes sent.
	Served func(ctx context.Context, bytesSent int64)
}

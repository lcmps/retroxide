package rom

type ROMSummaryDTO struct {
	ROMID       string  `json:"rom_id"`
	Title       string  `json:"title"`
	Platform    string  `json:"platform"`
	SHA256      string  `json:"sha256"`
	CoverArtURL *string `json:"cover_art_url"`
}

type ROMListResponseDTO struct {
	Item       []ROMSummaryDTO `json:"item"`
	NextCursor *string         `json:"next_cursor"`
}

type ROMDetailDTO struct {
	ROMID       string     `json:"rom_id"`
	Title       string     `json:"title"`
	Platform    string     `json:"platform"`
	Region      *string    `json:"region"`
	Version     *string    `json:"version"`
	CoverArtURL *string    `json:"cover_art_url"`
	Hash        ROMHashDTO `json:"hash"`
}

type ROMHashDTO struct {
	SHA256 string `json:"sha256"`
	SHA1   string `json:"sha1"`
	MD5    string `json:"md5"`
	CRC32  string `json:"crc32"`
}

type UpdateROMRequest struct {
	Title    string  `json:"title"`
	Platform string  `json:"platform"`
	Region   *string `json:"region"`
	Version  *string `json:"version"`
}

type DownloadOutput struct {
	Filename                string
	MIMEType                string
	Bytes                   []byte
	SHA256                  string
	LastModifiedUnixSeconds *uint64
}

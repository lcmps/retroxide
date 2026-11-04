package persistence

type UploadModel struct {
	ID               string  `gorm:"column:id;primaryKey;size:64"`
	UploaderID       string  `gorm:"column:uploader_id;not null;size:255"`
	OriginalFilename string  `gorm:"column:original_filename;not null;size:255"`
	Status           string  `gorm:"column:status;not null;size:32"`
	ErrorMessage     *string `gorm:"column:error_message"`
	ROMID            *string `gorm:"column:rom_id;size:64"`
}

func (UploadModel) TableName() string { return "upload" }

type UploadSessionModel struct {
	ID         string  `gorm:"column:id;primaryKey;size:64"`
	UploaderID string  `gorm:"column:uploader_id;not null;size:64"`
	Filename   string  `gorm:"column:filename;not null;size:255"`
	MIMEType   *string `gorm:"column:mime_type;size:255"`
	Size       int64   `gorm:"column:size;not null"`
	Received   int64   `gorm:"column:received;not null"`
	HashState  []byte  `gorm:"column:hash_state;not null"`
	UpdatedAt  int64   `gorm:"column:updated_at;not null;autoUpdateTime:false"`
	Part       bool    `gorm:"column:part;not null"`
}

type UploadPartModel struct {
	ID          string `gorm:"column:id;primaryKey;size:64"`
	UploaderID  string `gorm:"column:uploader_id;not null;size:64"`
	Filename    string `gorm:"column:filename;not null;size:255"`
	StoragePath string `gorm:"column:storage_path;not null;type:text"`
	Size        int64  `gorm:"column:size;not null"`
	SHA256      string `gorm:"column:sha256;not null;size:64"`
	SHA1        string `gorm:"column:sha1;not null;size:40"`
	MD5         string `gorm:"column:md5;not null;size:32"`
	CRC32       string `gorm:"column:crc32;not null;size:8"`
	CreatedAt   int64  `gorm:"column:created_at;not null;autoCreateTime:false"`
}

func (UploadPartModel) TableName() string { return "upload_part" }

func (UploadSessionModel) TableName() string { return "upload_session" }

type ROMFileModel struct {
	ID                string  `gorm:"column:id;primaryKey;size:67"`
	SHA256            string  `gorm:"column:sha256;not null;uniqueIndex;size:64"`
	SHA1              string  `gorm:"column:sha1;not null;size:40"`
	MD5               string  `gorm:"column:md5;not null;size:32"`
	CRC32             string  `gorm:"column:crc32;not null;default:00000000;size:8"`
	Size              uint64  `gorm:"column:size;not null"`
	StoragePath       string  `gorm:"column:storage_path;not null;type:text"`
	Format            string  `gorm:"column:format;not null;size:16"`
	DiscIdentity      *string `gorm:"column:disc_identity;size:64"`
	SheetPath         *string `gorm:"column:sheet_path;type:text"`
	CompressRequested bool    `gorm:"column:compress_requested;not null"`
}

func (ROMFileModel) TableName() string { return "rom_file" }

type DiscTrackModel struct {
	ROMFileID    string  `gorm:"column:rom_file_id;primaryKey;size:67"`
	Number       int     `gorm:"column:number;primaryKey"`
	Mode         string  `gorm:"column:mode;not null;size:16"`
	SectorBytes  int     `gorm:"column:sector_bytes;not null"`
	Frames       int     `gorm:"column:frames;not null"`
	Pregap       int     `gorm:"column:pregap;not null"`
	PregapStored bool    `gorm:"column:pregap_stored;not null"`
	Postgap      int     `gorm:"column:postgap;not null"`
	Size         int64   `gorm:"column:size;not null"`
	SHA256       string  `gorm:"column:sha256;not null;size:64"`
	SHA1         string  `gorm:"column:sha1;not null;size:40"`
	MD5          string  `gorm:"column:md5;not null;size:32"`
	CRC32        string  `gorm:"column:crc32;not null;size:8"`
	Filename     string  `gorm:"column:filename;not null;size:255"`
	StoragePath  *string `gorm:"column:storage_path;type:text"`
}

func (DiscTrackModel) TableName() string { return "disc_track" }

type ROMModel struct {
	ID              string  `gorm:"column:id;primaryKey;size:64"`
	ROMFileID       string  `gorm:"column:rom_file_id;not null;size:67;index:idx_rom_rom_file_id"`
	UploaderID      string  `gorm:"column:uploader_id;not null;default:system;size:64;index:idx_rom_uploader_id"`
	Title           string  `gorm:"column:title;not null;size:255"`
	Platform        string  `gorm:"column:platform;not null;size:64;index:idx_rom_platform"`
	Region          *string `gorm:"column:region;size:64"`
	Version         *string `gorm:"column:version;size:64"`
	IdentifiedBy    string  `gorm:"column:identified_by;not null;size:128"`
	Confidence      uint8   `gorm:"column:confidence;not null"`
	CoverArtPath    *string `gorm:"column:cover_art_path;type:text"`
	CoverThumbPath  *string `gorm:"column:cover_thumb_path;type:text"`
	Publisher       *string `gorm:"column:publisher;size:255"`
	ReleaseYear     *string `gorm:"column:release_year;size:16"`
	Genre           *string `gorm:"column:genre;size:64"`
	Description     *string `gorm:"column:description;type:text"`
	TitleScreenPath *string `gorm:"column:title_screen_path;type:text"`
	ScreenshotPath  *string `gorm:"column:screenshot_path;type:text"`
}

func (ROMModel) TableName() string { return "rom" }

type JobQueueModel struct {
	ID          uint64  `gorm:"column:id;primaryKey;autoIncrement"`
	Kind        string  `gorm:"column:kind;not null;size:64"`
	UploadID    string  `gorm:"column:upload_id;not null;size:64"`
	ROMFileID   string  `gorm:"column:rom_file_id;not null;size:67"`
	Attempt     uint8   `gorm:"column:attempt;not null"`
	CreatedAt   string  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
	ClaimedAt   *string `gorm:"column:claimed_at"`
	AvailableAt string  `gorm:"column:available_at;not null"`
}

func (JobQueueModel) TableName() string { return "job_queue" }

type JobDeadLetterModel struct {
	ID             uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	Kind           string `gorm:"column:kind;not null;size:64"`
	UploadID       string `gorm:"column:upload_id;not null;size:64"`
	ROMFileID      string `gorm:"column:rom_file_id;not null;size:67"`
	Attempt        uint8  `gorm:"column:attempt;not null"`
	Reason         string `gorm:"column:reason;not null;type:text"`
	DeadLetteredAt string `gorm:"column:dead_lettered_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
}

func (JobDeadLetterModel) TableName() string { return "job_dead_letter" }

type LibraryModel struct {
	ID        string `gorm:"column:id;primaryKey;size:64"`
	UserID    string `gorm:"column:user_id;not null;default:system;size:64;index:idx_library_user_id"`
	Name      string `gorm:"column:name;not null;size:255"`
	CreatedAt string `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
}

func (LibraryModel) TableName() string { return "library" }

type LibraryItemModel struct {
	LibraryID string `gorm:"column:library_id;primaryKey;size:64"`
	ROMID     string `gorm:"column:rom_id;primaryKey;size:64;index:idx_library_item_rom_id"`
}

func (LibraryItemModel) TableName() string { return "library_item" }

type UserModel struct {
	ID           string  `gorm:"column:id;primaryKey;size:64"`
	Email        string  `gorm:"column:email;not null;uniqueIndex;size:255"`
	Username     *string `gorm:"column:username;size:32"`
	PasswordHash string  `gorm:"column:password_hash;not null;size:255"`
	Role         string  `gorm:"column:role;not null;size:32"`
	TokenVersion uint64  `gorm:"column:token_version;not null;default:0"`
	// Locale is the user's chosen interface language; nil follows the browser.
	Locale *string `gorm:"column:locale;size:16"`
	// Font is the user's chosen interface font; nil uses the application default.
	Font *string `gorm:"column:font;size:16"`
}

func (UserModel) TableName() string { return "user" }

type ClientModel struct {
	ID        string `gorm:"column:id;primaryKey;size:64"`
	UserID    string `gorm:"column:user_id;not null;size:64;index:idx_client_user_id"`
	Name      string `gorm:"column:name;not null;size:255"`
	TokenHash string `gorm:"column:token_hash;not null;uniqueIndex;size:64"`
}

func (ClientModel) TableName() string { return "client" }

type RefreshTokenModel struct {
	JTI       string `gorm:"column:jti;primaryKey;size:64"`
	UserID    string `gorm:"column:user_id;not null;size:64;index:idx_refresh_tokens_user_id"`
	TokenHash string `gorm:"column:token_hash;not null;uniqueIndex;size:64"`
	ExpiresAt string `gorm:"column:expires_at;not null;autoCreateTime:false"`
	Revoked   bool   `gorm:"column:revoked;not null;default:false"`
	CreatedAt string `gorm:"column:created_at;not null;autoCreateTime:false"`
}

func (RefreshTokenModel) TableName() string { return "refresh_tokens" }

type SchemaMigrationModel struct {
	Version   uint64 `gorm:"column:version;primaryKey"`
	Name      string `gorm:"column:name;not null;size:255"`
	AppliedAt string `gorm:"column:applied_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
}

func (SchemaMigrationModel) TableName() string { return "schema_migration" }

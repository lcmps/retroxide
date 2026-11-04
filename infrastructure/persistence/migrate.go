package persistence

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/lcmps/retroxide/application/fault"
	"gorm.io/gorm"
)

type migration struct {
	version uint64
	name    string
	sql     string
}

var sqliteMigrations = []migration{
	{1, "create_core_tables", `CREATE TABLE IF NOT EXISTS upload (id TEXT PRIMARY KEY, uploader_id TEXT NOT NULL, original_filename TEXT NOT NULL, status TEXT NOT NULL, error_message TEXT);
CREATE TABLE IF NOT EXISTS rom_file (id TEXT PRIMARY KEY, sha256 TEXT NOT NULL UNIQUE, sha1 TEXT NOT NULL, md5 TEXT NOT NULL, size INTEGER NOT NULL, storage_path TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS rom (id TEXT PRIMARY KEY, rom_file_id TEXT NOT NULL, title TEXT NOT NULL, platform TEXT NOT NULL, region TEXT, version TEXT, identified_by TEXT NOT NULL, confidence INTEGER NOT NULL, FOREIGN KEY (rom_file_id) REFERENCES rom_file(id));`},
	{2, "add_core_indexes", `CREATE INDEX IF NOT EXISTS idx_upload_status ON upload(status);
CREATE INDEX IF NOT EXISTS idx_upload_uploader_id ON upload(uploader_id);
CREATE INDEX IF NOT EXISTS idx_rom_platform ON rom(platform);
CREATE INDEX IF NOT EXISTS idx_rom_rom_file_id ON rom(rom_file_id);`},
	{3, "add_job_queue_tables", `CREATE TABLE IF NOT EXISTS job_queue (id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, upload_id TEXT NOT NULL, rom_file_id TEXT NOT NULL, attempt INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_job_queue_created_at ON job_queue(created_at);
CREATE TABLE IF NOT EXISTS job_dead_letter (id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, upload_id TEXT NOT NULL, rom_file_id TEXT NOT NULL, attempt INTEGER NOT NULL, reason TEXT NOT NULL, dead_lettered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);`},
	{4, "add_library_tables", `CREATE TABLE IF NOT EXISTS library (id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS library_item (library_id TEXT NOT NULL, rom_id TEXT NOT NULL, PRIMARY KEY (library_id, rom_id), FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE, FOREIGN KEY (rom_id) REFERENCES rom(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_library_item_rom_id ON library_item(rom_id);`},
	{5, "add_auth_tables", `CREATE TABLE IF NOT EXISTS user (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_user_email ON user(email);
CREATE TABLE IF NOT EXISTS client (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_client_token_hash ON client(token_hash);
CREATE INDEX IF NOT EXISTS idx_client_user_id ON client(user_id);`},
	{6, "add_crc32_to_rom_file", `ALTER TABLE rom_file ADD COLUMN crc32 TEXT NOT NULL DEFAULT '00000000';`},
	{7, "add_refresh_tokens_and_job_scheduling", `ALTER TABLE upload ADD COLUMN rom_id TEXT;
CREATE TABLE IF NOT EXISTS refresh_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, revoked INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
ALTER TABLE job_queue ADD COLUMN claimed_at TEXT;
ALTER TABLE job_queue ADD COLUMN available_at TEXT NOT NULL DEFAULT '1970-01-01 00:00:00';`},
	{8, "add_ownership_columns", `ALTER TABLE rom ADD COLUMN uploader_id TEXT NOT NULL DEFAULT 'system';
ALTER TABLE library ADD COLUMN user_id TEXT NOT NULL DEFAULT 'system';
CREATE INDEX IF NOT EXISTS idx_rom_uploader_id ON rom(uploader_id);
CREATE INDEX IF NOT EXISTS idx_library_user_id ON library(user_id);`},
	{9, "add_rom_cover_art", `ALTER TABLE rom ADD COLUMN cover_art_path TEXT;`},
	{10, "add_rom_scraped_metadata", `ALTER TABLE rom ADD COLUMN publisher TEXT;
ALTER TABLE rom ADD COLUMN release_year TEXT;
ALTER TABLE rom ADD COLUMN description TEXT;
ALTER TABLE rom ADD COLUMN title_screen_path TEXT;
ALTER TABLE rom ADD COLUMN screenshot_path TEXT;`},
	{11, "widen_rom_file_identifiers", `SELECT 1;`},
	{12, "add_user_token_version", `ALTER TABLE user ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0;`},
	{13, "index_case_insensitive_platform", `ALTER TABLE rom ADD COLUMN platform_key TEXT GENERATED ALWAYS AS (LOWER(platform)) VIRTUAL;
CREATE INDEX IF NOT EXISTS idx_rom_platform_key ON rom(platform_key);`},
	{14, "index_job_queue_availability", `CREATE INDEX IF NOT EXISTS idx_job_queue_available_at ON job_queue(available_at);`},
	{15, "index_upload_rom_id", `CREATE INDEX IF NOT EXISTS idx_upload_rom_id ON upload(rom_id);`},
	{16, "add_user_locale", `ALTER TABLE user ADD COLUMN locale TEXT;`},
	{17, "add_user_font", `ALTER TABLE user ADD COLUMN font TEXT;`},
	// 18: composite indexes matching the listing queries (filter, then ID order), indexes for the
	// blob reference checks on delete, no more redundant indexes, and a trigram full-text index
	// for title/platform search. rom_search_key gives each ROM a stable integer FTS rowid: an
	// explicit INTEGER PRIMARY KEY survives VACUUM, unlike the implicit rowid of rom.
	{18, "optimize_rom_indexes_and_search", `DROP INDEX IF EXISTS idx_rom_uploader_id;
DROP INDEX IF EXISTS idx_rom_platform_key;
DROP INDEX IF EXISTS idx_rom_platform;
DROP INDEX IF EXISTS idx_user_email;
DROP INDEX IF EXISTS idx_client_token_hash;
DROP INDEX IF EXISTS idx_job_queue_created_at;
DROP INDEX IF EXISTS idx_upload_status;
CREATE INDEX IF NOT EXISTS idx_rom_uploader_listing ON rom(uploader_id, id);
CREATE INDEX IF NOT EXISTS idx_rom_uploader_platform ON rom(uploader_id, platform_key, id, platform);
CREATE INDEX IF NOT EXISTS idx_rom_platform_listing ON rom(platform_key, id, platform);
CREATE INDEX IF NOT EXISTS idx_rom_cover_art_path ON rom(cover_art_path);
CREATE INDEX IF NOT EXISTS idx_rom_title_screen_path ON rom(title_screen_path);
CREATE INDEX IF NOT EXISTS idx_rom_screenshot_path ON rom(screenshot_path);
CREATE INDEX IF NOT EXISTS idx_rom_file_storage_path ON rom_file(storage_path);
CREATE TABLE IF NOT EXISTS rom_search_key (id INTEGER PRIMARY KEY, rom_id TEXT NOT NULL UNIQUE);
CREATE VIRTUAL TABLE IF NOT EXISTS rom_search USING fts5(title, platform, tokenize='trigram');
INSERT INTO rom_search_key (rom_id) SELECT id FROM rom;
INSERT INTO rom_search (rowid, title, platform) SELECT k.id, r.title, r.platform FROM rom_search_key AS k JOIN rom AS r ON r.id = k.rom_id;
CREATE TRIGGER IF NOT EXISTS rom_search_after_insert AFTER INSERT ON rom BEGIN
	INSERT INTO rom_search_key (rom_id) VALUES (new.id);
	INSERT INTO rom_search (rowid, title, platform) SELECT id, new.title, new.platform FROM rom_search_key WHERE rom_id = new.id;
END;
CREATE TRIGGER IF NOT EXISTS rom_search_after_update AFTER UPDATE OF title, platform ON rom BEGIN
	DELETE FROM rom_search WHERE rowid = (SELECT id FROM rom_search_key WHERE rom_id = old.id);
	INSERT INTO rom_search (rowid, title, platform) SELECT id, new.title, new.platform FROM rom_search_key WHERE rom_id = old.id;
END;
CREATE TRIGGER IF NOT EXISTS rom_search_after_delete AFTER DELETE ON rom BEGIN
	DELETE FROM rom_search WHERE rowid = (SELECT id FROM rom_search_key WHERE rom_id = old.id);
	DELETE FROM rom_search_key WHERE rom_id = old.id;
END;`},
	// 19: an optional sign-in username, unique case-insensitively (it is stored lowercase).
	{19, "add_user_username", `ALTER TABLE user ADD COLUMN username TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_username ON user(username);`},
	// 20: a scraped or edited genre, and indexes that let the library be read in title, release,
	// publisher, or genre order one page at a time, for everyone or one uploader. Text sorts are
	// case-insensitive, so SQLite indexes them with NOCASE, matching the listing's ORDER BY.
	{20, "add_rom_genre_and_sort_indexes", `ALTER TABLE rom ADD COLUMN genre TEXT;
CREATE INDEX IF NOT EXISTS idx_rom_sort_title ON rom(title COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_sort_release ON rom(release_year COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_sort_publisher ON rom(publisher COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_sort_genre ON rom(genre COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_uploader_sort_title ON rom(uploader_id, title COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_uploader_sort_release ON rom(uploader_id, release_year COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_uploader_sort_publisher ON rom(uploader_id, publisher COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS idx_rom_uploader_sort_genre ON rom(uploader_id, genre COLLATE NOCASE, id);`},
	// 21: a small copy of the cover for library grids, indexed like the other image paths so
	// deleting a ROM can check whether another still references it.
	{21, "add_rom_cover_thumbnail", `ALTER TABLE rom ADD COLUMN cover_thumb_path TEXT;
CREATE INDEX IF NOT EXISTS idx_rom_cover_thumb_path ON rom(cover_thumb_path);`},
	// 22: uploads sent in parts, which resume after a dropped connection. updated_at is in Unix
	// seconds and indexed, so idle sessions are found without reading the table.
	{22, "add_upload_sessions", `CREATE TABLE IF NOT EXISTS upload_session (id TEXT PRIMARY KEY, uploader_id TEXT NOT NULL, filename TEXT NOT NULL, mime_type TEXT, size INTEGER NOT NULL, received INTEGER NOT NULL, hash_state BLOB NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_upload_session_updated_at ON upload_session(updated_at);`},
	// 23: discs. rom_file.format says how a file is stored (files already stored are marked
	// pending, to be inspected for CHD images); disc_identity fingerprints a disc's tracks, so the
	// same disc is recognized as a CUE/BIN set or a CHD; disc_track lists each track's layout and
	// hashes, and its file while the disc is kept as separate tracks. sheet_path keeps the sheet a
	// disc was uploaded with, also once compressed; compress_requested asks for compression to CHD.
	// Upload sessions can carry
	// one file of a disc (a part), kept in upload_part until the disc is assembled.
	{23, "add_discs", `ALTER TABLE rom_file ADD COLUMN format TEXT NOT NULL DEFAULT 'file';
UPDATE rom_file SET format = 'pending';
ALTER TABLE rom_file ADD COLUMN disc_identity TEXT;
ALTER TABLE rom_file ADD COLUMN sheet_path TEXT;
ALTER TABLE rom_file ADD COLUMN compress_requested INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_rom_file_disc_identity ON rom_file(disc_identity);
CREATE INDEX IF NOT EXISTS idx_rom_file_sheet_path ON rom_file(sheet_path);
CREATE INDEX IF NOT EXISTS idx_rom_file_format ON rom_file(format);
CREATE TABLE IF NOT EXISTS disc_track (rom_file_id TEXT NOT NULL, number INTEGER NOT NULL, mode TEXT NOT NULL, sector_bytes INTEGER NOT NULL, frames INTEGER NOT NULL, pregap INTEGER NOT NULL, pregap_stored INTEGER NOT NULL, postgap INTEGER NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL, sha1 TEXT NOT NULL, md5 TEXT NOT NULL, crc32 TEXT NOT NULL, filename TEXT NOT NULL, storage_path TEXT, PRIMARY KEY (rom_file_id, number), FOREIGN KEY (rom_file_id) REFERENCES rom_file(id));
CREATE INDEX IF NOT EXISTS idx_disc_track_sha1 ON disc_track(sha1);
CREATE INDEX IF NOT EXISTS idx_disc_track_storage_path ON disc_track(storage_path);
ALTER TABLE upload_session ADD COLUMN part INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS upload_part (id TEXT PRIMARY KEY, uploader_id TEXT NOT NULL, filename TEXT NOT NULL, storage_path TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL, sha1 TEXT NOT NULL, md5 TEXT NOT NULL, crc32 TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_upload_part_created_at ON upload_part(created_at);
CREATE INDEX IF NOT EXISTS idx_upload_part_storage_path ON upload_part(storage_path);`},
}

var mysqlMigrations = []migration{
	{1, "create_core_tables", `CREATE TABLE IF NOT EXISTS upload (id VARCHAR(64) PRIMARY KEY, uploader_id VARCHAR(255) NOT NULL, original_filename VARCHAR(255) NOT NULL, status VARCHAR(32) NOT NULL, error_message TEXT NULL);
CREATE TABLE IF NOT EXISTS rom_file (id VARCHAR(64) PRIMARY KEY, sha256 CHAR(64) NOT NULL UNIQUE, sha1 CHAR(40) NOT NULL, md5 CHAR(32) NOT NULL, size BIGINT UNSIGNED NOT NULL, storage_path TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS rom (id VARCHAR(64) PRIMARY KEY, rom_file_id VARCHAR(64) NOT NULL, title VARCHAR(255) NOT NULL, platform VARCHAR(64) NOT NULL, region VARCHAR(64) NULL, version VARCHAR(64) NULL, identified_by VARCHAR(128) NOT NULL, confidence TINYINT UNSIGNED NOT NULL, CONSTRAINT fk_rom_file FOREIGN KEY (rom_file_id) REFERENCES rom_file(id));`},
	{2, "add_core_indexes", `CREATE INDEX idx_upload_status ON upload(status);
CREATE INDEX idx_upload_uploader_id ON upload(uploader_id);
CREATE INDEX idx_rom_platform ON rom(platform);
CREATE INDEX idx_rom_rom_file_id ON rom(rom_file_id);`},
	{3, "add_job_queue_tables", `CREATE TABLE IF NOT EXISTS job_queue (id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT, kind VARCHAR(64) NOT NULL, upload_id VARCHAR(64) NOT NULL, rom_file_id VARCHAR(64) NOT NULL, attempt TINYINT UNSIGNED NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX idx_job_queue_created_at ON job_queue(created_at);
CREATE TABLE IF NOT EXISTS job_dead_letter (id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT, kind VARCHAR(64) NOT NULL, upload_id VARCHAR(64) NOT NULL, rom_file_id VARCHAR(64) NOT NULL, attempt TINYINT UNSIGNED NOT NULL, reason TEXT NOT NULL, dead_lettered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);`},
	{4, "add_library_tables", `CREATE TABLE IF NOT EXISTS library (id VARCHAR(64) PRIMARY KEY, name VARCHAR(255) NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS library_item (library_id VARCHAR(64) NOT NULL, rom_id VARCHAR(64) NOT NULL, PRIMARY KEY (library_id, rom_id), CONSTRAINT fk_library_item_library FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE, CONSTRAINT fk_library_item_rom FOREIGN KEY (rom_id) REFERENCES rom(id) ON DELETE CASCADE);
CREATE INDEX idx_library_item_rom_id ON library_item(rom_id);`},
	{5, "add_auth_tables", `CREATE TABLE IF NOT EXISTS user (id VARCHAR(64) PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE, password_hash VARCHAR(255) NOT NULL, role VARCHAR(32) NOT NULL);
CREATE INDEX idx_user_email ON user(email);
CREATE TABLE IF NOT EXISTS client (id VARCHAR(64) PRIMARY KEY, user_id VARCHAR(64) NOT NULL, name VARCHAR(255) NOT NULL, token_hash CHAR(64) NOT NULL UNIQUE, CONSTRAINT fk_client_user FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE);
CREATE INDEX idx_client_token_hash ON client(token_hash);
CREATE INDEX idx_client_user_id ON client(user_id);`},
	{6, "add_crc32_to_rom_file", `ALTER TABLE rom_file ADD COLUMN crc32 CHAR(8) NOT NULL DEFAULT '00000000';`},
	{7, "add_refresh_tokens_and_job_scheduling", `ALTER TABLE upload ADD COLUMN rom_id VARCHAR(64) NULL;
CREATE TABLE IF NOT EXISTS refresh_tokens (jti VARCHAR(64) PRIMARY KEY, user_id VARCHAR(64) NOT NULL, token_hash CHAR(64) NOT NULL UNIQUE, expires_at TIMESTAMP NOT NULL, revoked BOOLEAN NOT NULL DEFAULT FALSE, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, CONSTRAINT fk_refresh_tokens_user FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
ALTER TABLE job_queue ADD COLUMN claimed_at TIMESTAMP NULL;
ALTER TABLE job_queue ADD COLUMN available_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;`},
	{8, "add_ownership_columns", `ALTER TABLE rom ADD COLUMN uploader_id VARCHAR(64) NOT NULL DEFAULT 'system';
ALTER TABLE library ADD COLUMN user_id VARCHAR(64) NOT NULL DEFAULT 'system';
CREATE INDEX idx_rom_uploader_id ON rom(uploader_id);
CREATE INDEX idx_library_user_id ON library(user_id);`},
	{9, "add_rom_cover_art", `ALTER TABLE rom ADD COLUMN cover_art_path TEXT NULL;`},
	{10, "add_rom_scraped_metadata", `ALTER TABLE rom ADD COLUMN publisher VARCHAR(255) NULL;
ALTER TABLE rom ADD COLUMN release_year VARCHAR(16) NULL;
ALTER TABLE rom ADD COLUMN description TEXT NULL;
ALTER TABLE rom ADD COLUMN title_screen_path TEXT NULL;
ALTER TABLE rom ADD COLUMN screenshot_path TEXT NULL;`},
	{11, "widen_rom_file_identifiers", `ALTER TABLE rom DROP FOREIGN KEY fk_rom_file;
ALTER TABLE rom_file MODIFY COLUMN id VARCHAR(67) NOT NULL;
ALTER TABLE rom MODIFY COLUMN rom_file_id VARCHAR(67) NOT NULL;
ALTER TABLE job_queue MODIFY COLUMN rom_file_id VARCHAR(67) NOT NULL;
ALTER TABLE job_dead_letter MODIFY COLUMN rom_file_id VARCHAR(67) NOT NULL;
ALTER TABLE rom ADD CONSTRAINT fk_rom_file FOREIGN KEY (rom_file_id) REFERENCES rom_file(id);`},
	{12, "add_user_token_version", `ALTER TABLE user ADD COLUMN token_version BIGINT UNSIGNED NOT NULL DEFAULT 0;`},
	{13, "index_case_insensitive_platform", `ALTER TABLE rom ADD COLUMN platform_key VARCHAR(64) GENERATED ALWAYS AS (LOWER(platform)) STORED;
CREATE INDEX idx_rom_platform_key ON rom(platform_key);`},
	{14, "index_job_queue_availability", `CREATE INDEX idx_job_queue_available_at ON job_queue(available_at);`},
	{15, "index_upload_rom_id", `CREATE INDEX idx_upload_rom_id ON upload(rom_id);`},
	{16, "add_user_locale", `ALTER TABLE user ADD COLUMN locale VARCHAR(16) NULL;`},
	{17, "add_user_font", `ALTER TABLE user ADD COLUMN font VARCHAR(16) NULL;`},
	// 18: see the SQLite migration. Path columns are TEXT, so they get prefix indexes (paths are
	// far shorter than 512 characters). Search uses a word FULLTEXT index, built without the
	// stopword list so words such as "the" stay searchable; word tokens are far more selective
	// than ngrams, which measured slower than a scan for typical searches.
	{18, "optimize_rom_indexes_and_search", `DROP INDEX idx_rom_uploader_id ON rom;
DROP INDEX idx_rom_platform_key ON rom;
DROP INDEX idx_rom_platform ON rom;
DROP INDEX idx_user_email ON user;
DROP INDEX idx_client_token_hash ON client;
DROP INDEX idx_job_queue_created_at ON job_queue;
DROP INDEX idx_upload_status ON upload;
CREATE INDEX idx_rom_uploader_listing ON rom(uploader_id, id);
CREATE INDEX idx_rom_uploader_platform ON rom(uploader_id, platform_key, id, platform);
CREATE INDEX idx_rom_platform_listing ON rom(platform_key, id, platform);
CREATE INDEX idx_rom_cover_art_path ON rom(cover_art_path(512));
CREATE INDEX idx_rom_title_screen_path ON rom(title_screen_path(512));
CREATE INDEX idx_rom_screenshot_path ON rom(screenshot_path(512));
CREATE INDEX idx_rom_file_storage_path ON rom_file(storage_path(512));
SET SESSION innodb_ft_enable_stopword = OFF;
CREATE FULLTEXT INDEX ft_rom_search ON rom(title, platform);
SET SESSION innodb_ft_enable_stopword = ON;`},
	{19, "add_user_username", `ALTER TABLE user ADD COLUMN username VARCHAR(32) NULL;
CREATE UNIQUE INDEX idx_user_username ON user(username);`},
	// 20: see the SQLite migration; MySQL's column collations already compare case-insensitively.
	{20, "add_rom_genre_and_sort_indexes", `ALTER TABLE rom ADD COLUMN genre VARCHAR(64) NULL;
CREATE INDEX idx_rom_sort_title ON rom(title, id);
CREATE INDEX idx_rom_sort_release ON rom(release_year, id);
CREATE INDEX idx_rom_sort_publisher ON rom(publisher, id);
CREATE INDEX idx_rom_sort_genre ON rom(genre, id);
CREATE INDEX idx_rom_uploader_sort_title ON rom(uploader_id, title, id);
CREATE INDEX idx_rom_uploader_sort_release ON rom(uploader_id, release_year, id);
CREATE INDEX idx_rom_uploader_sort_publisher ON rom(uploader_id, publisher, id);
CREATE INDEX idx_rom_uploader_sort_genre ON rom(uploader_id, genre, id);`},
	{21, "add_rom_cover_thumbnail", `ALTER TABLE rom ADD COLUMN cover_thumb_path TEXT NULL;
CREATE INDEX idx_rom_cover_thumb_path ON rom(cover_thumb_path(512));`},
	{22, "add_upload_sessions", `CREATE TABLE IF NOT EXISTS upload_session (id VARCHAR(64) PRIMARY KEY, uploader_id VARCHAR(64) NOT NULL, filename VARCHAR(255) NOT NULL, mime_type VARCHAR(255) NULL, size BIGINT UNSIGNED NOT NULL, received BIGINT UNSIGNED NOT NULL, hash_state BLOB NOT NULL, updated_at BIGINT NOT NULL);
CREATE INDEX idx_upload_session_updated_at ON upload_session(updated_at);`},
	{23, "add_discs", `ALTER TABLE rom_file ADD COLUMN format VARCHAR(16) NOT NULL DEFAULT 'file';
UPDATE rom_file SET format = 'pending';
ALTER TABLE rom_file ADD COLUMN disc_identity CHAR(64) NULL;
ALTER TABLE rom_file ADD COLUMN sheet_path TEXT NULL;
ALTER TABLE rom_file ADD COLUMN compress_requested BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX idx_rom_file_disc_identity ON rom_file(disc_identity);
CREATE INDEX idx_rom_file_sheet_path ON rom_file(sheet_path(512));
CREATE INDEX idx_rom_file_format ON rom_file(format);
CREATE TABLE IF NOT EXISTS disc_track (rom_file_id VARCHAR(67) NOT NULL, number SMALLINT UNSIGNED NOT NULL, mode VARCHAR(16) NOT NULL, sector_bytes SMALLINT UNSIGNED NOT NULL, frames INT UNSIGNED NOT NULL, pregap INT UNSIGNED NOT NULL, pregap_stored BOOLEAN NOT NULL, postgap INT UNSIGNED NOT NULL, size BIGINT UNSIGNED NOT NULL, sha256 CHAR(64) NOT NULL, sha1 CHAR(40) NOT NULL, md5 CHAR(32) NOT NULL, crc32 CHAR(8) NOT NULL, filename VARCHAR(255) NOT NULL, storage_path TEXT NULL, PRIMARY KEY (rom_file_id, number), CONSTRAINT fk_disc_track_rom_file FOREIGN KEY (rom_file_id) REFERENCES rom_file(id));
CREATE INDEX idx_disc_track_sha1 ON disc_track(sha1);
CREATE INDEX idx_disc_track_storage_path ON disc_track(storage_path(512));
ALTER TABLE upload_session ADD COLUMN part BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE IF NOT EXISTS upload_part (id VARCHAR(64) PRIMARY KEY, uploader_id VARCHAR(64) NOT NULL, filename VARCHAR(255) NOT NULL, storage_path TEXT NOT NULL, size BIGINT UNSIGNED NOT NULL, sha256 CHAR(64) NOT NULL, sha1 CHAR(40) NOT NULL, md5 CHAR(32) NOT NULL, crc32 CHAR(8) NOT NULL, created_at BIGINT NOT NULL);
CREATE INDEX idx_upload_part_created_at ON upload_part(created_at);
CREATE INDEX idx_upload_part_storage_path ON upload_part(storage_path(512));`},
}

func RunMigrations(ctx context.Context, db *gorm.DB) error {
	dialect := db.Dialector.Name()
	var migrations []migration
	switch dialect {
	case "sqlite":
		migrations = sqliteMigrations
		if err := db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS schema_migration (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`).Error; err != nil {
			return databaseError("creating migration table", err)
		}
	case "mysql":
		migrations = mysqlMigrations
		if err := db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS schema_migration (version INT UNSIGNED PRIMARY KEY, name VARCHAR(255) NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`).Error; err != nil {
			return databaseError("creating migration table", err)
		}
	default:
		return fault.Validation("unsupported database dialect " + dialect)
	}

	var applied []SchemaMigrationModel
	if err := db.WithContext(ctx).Order("version ASC").Find(&applied).Error; err != nil {
		return databaseError("reading migration history", err)
	}
	if err := validateMigrationHistory(applied, migrations); err != nil {
		return fault.Internal(err)
	}
	appliedByVersion := make(map[uint64]string, len(applied))
	for _, record := range applied {
		appliedByVersion[record.Version] = record.Name
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for _, item := range migrations {
		if name, ok := appliedByVersion[item.version]; ok {
			if name != item.name {
				return fault.Internal(fmt.Errorf("migration version %d has name %q, expected %q", item.version, name, item.name))
			}
			continue
		}
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, statement := range splitMigrationStatements(item.sql) {
				if err := tx.Exec(statement).Error; err != nil {
					return fmt.Errorf("applying migration %d (%s): %w", item.version, item.name, err)
				}
			}
			return tx.Exec("INSERT INTO schema_migration (version, name) VALUES (?, ?)", item.version, item.name).Error
		})
		if err != nil {
			return databaseError("applying migration", err)
		}
	}
	return nil
}

// splitMigrationStatements splits a migration on semicolons, keeping each BEGIN ... END block
// (a trigger body, whose statements end in semicolons too) inside its statement.
func splitMigrationStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	depth := 0
	for _, line := range strings.SplitAfter(sql, "\n") {
		for _, word := range strings.FieldsFunc(strings.ToUpper(line), func(r rune) bool {
			return !unicode.IsLetter(r) && r != '_'
		}) {
			switch word {
			case "BEGIN":
				depth++
			case "END":
				if depth > 0 {
					depth--
				}
			}
		}
		for {
			index := strings.IndexByte(line, ';')
			if index < 0 || depth > 0 {
				current.WriteString(line)
				break
			}
			current.WriteString(line[:index])
			if statement := strings.TrimSpace(current.String()); statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
			line = line[index+1:]
		}
	}
	if statement := strings.TrimSpace(current.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}

func validateMigrationHistory(records []SchemaMigrationModel, migrations []migration) error {
	expected := make(map[uint64]string, len(migrations))
	for _, item := range migrations {
		expected[item.version] = item.name
	}
	for _, record := range records {
		name, ok := expected[record.Version]
		if !ok || name != record.Name {
			return fmt.Errorf("unknown migration record %d:%s", record.Version, record.Name)
		}
	}
	return nil
}

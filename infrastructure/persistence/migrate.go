package persistence

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

func splitMigrationStatements(sql string) []string {
	parts := strings.Split(sql, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
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

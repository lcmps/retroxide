package persistence

import (
	"sync"
	"testing"

	"github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"gorm.io/gorm/schema"
)

func TestModelsMapToRustTableAndColumnNames(t *testing.T) {
	tests := []struct {
		name          string
		model         any
		table         string
		requiredNames []string
	}{
		{name: "upload", model: UploadModel{}, table: "upload", requiredNames: []string{"uploader_id", "original_filename", "error_message", "rom_id"}},
		{name: "rom file", model: ROMFileModel{}, table: "rom_file", requiredNames: []string{"sha256", "sha1", "md5", "crc32", "storage_path"}},
		{name: "rom", model: ROMModel{}, table: "rom", requiredNames: []string{"rom_file_id", "uploader_id", "identified_by", "cover_art_path"}},
		{name: "job queue", model: JobQueueModel{}, table: "job_queue", requiredNames: []string{"upload_id", "rom_file_id", "claimed_at", "available_at"}},
		{name: "library item", model: LibraryItemModel{}, table: "library_item", requiredNames: []string{"library_id", "rom_id"}},
		{name: "refresh token", model: RefreshTokenModel{}, table: "refresh_tokens", requiredNames: []string{"token_hash", "expires_at", "revoked"}},
		{name: "schema migration", model: SchemaMigrationModel{}, table: "schema_migration", requiredNames: []string{"version", "name", "applied_at"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Table != test.table {
				t.Fatalf("table = %q, want %q", parsed.Table, test.table)
			}
			for _, column := range test.requiredNames {
				if parsed.FieldsByDBName[column] == nil {
					t.Errorf("missing database column %q", column)
				}
			}
		})
	}
}

func TestDatabaseOwnedTimestampDefaultsArePreserved(t *testing.T) {
	tests := []struct {
		name  string
		model any
		field string
	}{
		{name: "library creation", model: LibraryModel{}, field: "CreatedAt"},
		{name: "migration application", model: SchemaMigrationModel{}, field: "AppliedAt"},
		{name: "queue creation", model: JobQueueModel{}, field: "CreatedAt"},
		{name: "dead-letter creation", model: JobDeadLetterModel{}, field: "DeadLetteredAt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatal(err)
			}
			field := parsed.FieldsByName[test.field]
			if field == nil {
				t.Fatalf("field %q not parsed", test.field)
			}
			if !field.HasDefaultValue || field.DefaultValue != "CURRENT_TIMESTAMP" {
				t.Fatalf("default metadata = hasDefault:%t value:%q, want CURRENT_TIMESTAMP", field.HasDefaultValue, field.DefaultValue)
			}
		})
	}
}

func TestROMFileConflictRequiresAllHashesToMatch(t *testing.T) {
	storedHash, err := rom.NewROMHash(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"deadbeef",
	)
	if err != nil {
		t.Fatal(err)
	}
	incomingHash := storedHash
	incomingHash.MD5 = "dddddddddddddddddddddddddddddddd"
	tests := []struct {
		name     string
		stored   rom.ROMHash
		incoming rom.ROMHash
		wantDup  bool
	}{
		{name: "all hashes match", stored: storedHash, incoming: storedHash, wantDup: true},
		{name: "secondary hash differs", stored: storedHash, incoming: incomingHash},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := classifyROMFileConflict("rf-existing", test.stored, test.incoming)
			if fault.IsCode(err, fault.CodeROMAlreadyExists) != test.wantDup {
				t.Fatalf("conflict error = %v, want duplicate %t", err, test.wantDup)
			}
			if err == nil {
				t.Fatal("conflict classification returned nil error")
			}
		})
	}
}

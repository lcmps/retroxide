package persistence

import (
	"context"
	"github.com/go-sql-driver/mysql"
	"testing"
)

func TestPoolSettings(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		wantOpen int
		wantIdle int
		wantErr  bool
	}{
		{name: "sqlite serializes writes like rust connection mutex", driver: "sqlite", wantOpen: 1, wantIdle: 1},
		{name: "mysql pool is bounded", driver: "mysql", wantOpen: 10, wantIdle: 5},
		{name: "unknown driver rejected", driver: "postgres", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			maxOpen, maxIdle, err := poolSettings(test.driver)
			if (err != nil) != test.wantErr {
				t.Fatalf("poolSettings(%q) error = %v, wantErr %t", test.driver, err, test.wantErr)
			}
			if err == nil && (maxOpen != test.wantOpen || maxIdle != test.wantIdle) {
				t.Fatalf("poolSettings(%q) = %d/%d, want %d/%d", test.driver, maxOpen, maxIdle, test.wantOpen, test.wantIdle)
			}
		})
	}
}

func TestOpenAndInitializeSQLiteDatabase(t *testing.T) {
	db, err := OpenDatabase(context.Background(), "sqlite", t.TempDir()+"/pragma.sqlite3", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		connection, err := db.DB()
		if err != nil {
			t.Error(err)
			return
		}
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	}()
	var count int64
	if err := db.Model(&SchemaMigrationModel{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(len(sqliteMigrations)) {
		t.Fatalf("migration count = %d, want %d", count, len(sqliteMigrations))
	}
	if !db.Migrator().HasColumn(&ROMModel{}, "cover_art_path") {
		t.Fatal("v9 cover_art_path column is missing")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	var foreignKeys int
	if err := sqlDB.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys pragma = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := sqlDB.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func TestMySQLURLConvertsToDriverDSN(t *testing.T) {
	dsn, err := mysqlDSN("mysql://retro:secret@db.example.test:3307/library?charset=utf8mb4")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.User != "retro" || parsed.Passwd != "secret" || parsed.Addr != "db.example.test:3307" || parsed.DBName != "library" || !parsed.ParseTime {
		t.Fatalf("parsed DSN = %+v", parsed)
	}
}

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lcmps/retroxide/application/fault"
	"gorm.io/gorm"
)

func TestPoolSettings(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		wantOpen int
		wantIdle int
		wantErr  bool
	}{
		{name: "sqlite pools concurrent WAL readers", driver: "sqlite", wantOpen: 4, wantIdle: 4},
		{name: "mysql pool is bounded", driver: "mysql", wantOpen: 10, wantIdle: 10},
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
	if !parsed.InterpolateParams {
		t.Fatal("queries should be interpolated client-side to save round trips")
	}
	optOut, err := mysqlDSN("mysql://retro:secret@db.example.test:3307/library?interpolateParams=false")
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := mysql.ParseDSN(optOut); err != nil || parsed.InterpolateParams {
		t.Fatalf("interpolateParams=false should opt out: %+v, %v", parsed, err)
	}
	if _, err := mysqlDSN("mysql://retro@db.example.test/library?interpolateParams=maybe"); err == nil {
		t.Fatal("an invalid interpolateParams value should be rejected")
	}
}

func TestSQLiteConfiguresEveryPooledConnectionAndQueuesConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDatabase(ctx, "sqlite", t.TempDir()+"/pool.sqlite3", "")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Hold several connections at once so the pool must open distinct ones.
	connections := make([]*sql.Conn, sqliteMaxOpenConns)
	for i := range connections {
		if connections[i], err = sqlDB.Conn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i, connection := range connections {
		var foreignKeys, synchronous int
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || synchronous != 1 {
			t.Fatalf("connection %d: foreign_keys=%d synchronous=%d, want 1 and 1 (NORMAL)", i, foreignKeys, synchronous)
		}
		var tempStore, cacheSize int
		if err := connection.QueryRowContext(ctx, "PRAGMA temp_store").Scan(&tempStore); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize); err != nil {
			t.Fatal(err)
		}
		if tempStore != 2 || cacheSize != -sqliteCacheKiB {
			t.Fatalf("connection %d: temp_store=%d cache_size=%d, want 2 (MEMORY) and -%d", i, tempStore, cacheSize, sqliteCacheKiB)
		}
		_ = connection.Close()
	}

	// Concurrent read-then-write transactions would hit "database is locked" when upgrading
	// deferred locks; IMMEDIATE transactions queue on busy_timeout instead.
	repositories := NewRepositories(db)
	var group sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var count int64
				if err := tx.Model(&LibraryModel{}).Count(&count).Error; err != nil {
					return err
				}
				return tx.Exec("INSERT INTO library (id, user_id, name) VALUES (?, ?, ?)", fmt.Sprintf("lib-%d", i), "user", "name").Error
			})
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent transaction failed: %v", err)
		}
	}
}

func TestSQLiteDSNRejectsQueryCharacterInPath(t *testing.T) {
	if _, err := sqliteDSN("/data/odd?.sqlite3"); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("sqliteDSN error = %v, want validation error", err)
	}
}

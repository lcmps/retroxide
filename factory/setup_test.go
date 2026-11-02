package factory

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/setup"
	"github.com/lcmps/retroxide/infrastructure/config"
)

func mapLookup(values map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func sqliteSetupRequest(t *testing.T) setup.Request {
	t.Helper()
	directory := t.TempDir()
	return setup.Request{
		DBDriver: setup.DriverSQLite, SQLitePath: filepath.Join(directory, "setup.sqlite3"),
		StorageRoot: filepath.Join(directory, "storage"),
		AdminEmail:  "admin@example.com", AdminPassword: "correct horse battery staple",
	}
}

func TestSetupCompleteWritesConfigAndHandsOffApplication(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "retroxide.env")
	var handedOff *Application
	service := NewSetupService(configPath, mapLookup(nil), slog.New(slog.NewTextHandler(io.Discard, nil)), func(application *Application) {
		handedOff = application
	})
	if !service.Status().Required {
		t.Fatal("setup should be required before completion")
	}
	request := sqliteSetupRequest(t)
	if err := service.Complete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if handedOff == nil {
		t.Fatal("completed application was not handed off")
	}
	closeApplication(t, handedOff)

	values, err := config.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(values["RETROXIDE_JWT_SECRET"]) < 32 || values["RETROXIDE_DB_DRIVER"] != "sqlite" || values["RETROXIDE_SQLITE_PATH"] != request.SQLitePath {
		t.Fatalf("written config = %v", values)
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), request.AdminPassword) || strings.Contains(string(contents), "ADMIN") {
		t.Fatal("admin credentials must not be persisted")
	}
	login(t, handedOff.Router, request.AdminPassword)

	if service.Status().Required {
		t.Fatal("setup should not be required after completion")
	}
	if err := service.Complete(context.Background(), request); !fault.IsCode(err, fault.CodeConflict) {
		t.Fatalf("second Complete() = %v, want conflict", err)
	}
	reloaded, err := config.LoadEnvironment(configPath, mapLookup(nil))
	if err != nil {
		t.Fatalf("written config does not load: %v", err)
	}
	if reloaded.SQLitePath != request.SQLitePath {
		t.Fatalf("reloaded sqlite path = %q", reloaded.SQLitePath)
	}
}

func TestSetupCompleteRejectsInvalidInputWithoutWriting(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "retroxide.env")
	service := NewSetupService(configPath, mapLookup(nil), slog.New(slog.NewTextHandler(io.Discard, nil)), func(*Application) {
		t.Fatal("onComplete must not run")
	})
	cases := map[string]func(*setup.Request){
		"short password":  func(request *setup.Request) { request.AdminPassword = "short" },
		"bad email":       func(request *setup.Request) { request.AdminEmail = "admin" },
		"unknown driver":  func(request *setup.Request) { request.DBDriver = "postgres" },
		"missing storage": func(request *setup.Request) { request.StorageRoot = " " },
		"unreachable mysql": func(request *setup.Request) {
			request.DBDriver = setup.DriverMySQL
			request.MySQL = setup.MySQLConnection{Host: "127.0.0.1", Port: 1, User: "root", Database: "retroxide"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			request := sqliteSetupRequest(t)
			mutate(&request)
			if err := service.Complete(context.Background(), request); !fault.IsCode(err, fault.CodeValidation) {
				t.Fatalf("Complete() = %v, want validation error", err)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("config file written after failed setup: %v", err)
			}
		})
	}
}

func TestSetupStatusReportsEnvironmentLockedFields(t *testing.T) {
	service := NewSetupService(filepath.Join(t.TempDir(), "missing.env"), mapLookup(map[string]string{
		"RETROXIDE_DB_DRIVER": "mysql",
		"RETROXIDE_MYSQL_URL": "mysql://bob:hidden@db.example:3307/roms",
	}), nil, func(*Application) {})
	status := service.Status()
	if !slices.Equal(status.Locked, []string{"db_driver", "mysql"}) {
		t.Fatalf("locked = %v", status.Locked)
	}
	want := setup.MySQLConnection{Host: "db.example", Port: 3307, User: "bob", Database: "roms"}
	if status.Defaults == nil || status.Defaults.DBDriver != "mysql" || status.Defaults.MySQL != want {
		t.Fatalf("defaults = %+v", status.Defaults)
	}
}

func TestSetupTestMySQLReportsValidationAndConnectionFailures(t *testing.T) {
	service := NewSetupService(filepath.Join(t.TempDir(), "missing.env"), mapLookup(nil), nil, func(*Application) {})
	if err := service.TestMySQL(context.Background(), setup.MySQLConnection{Port: 3306}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("missing host error = %v", err)
	}
	err := service.TestMySQL(context.Background(), setup.MySQLConnection{Host: "127.0.0.1", Port: 1, User: "root", Database: "retroxide"})
	if !fault.IsCode(err, fault.CodeValidation) || !strings.Contains(err.Error(), "MySQL connection failed") {
		t.Fatalf("unreachable error = %v", err)
	}
}

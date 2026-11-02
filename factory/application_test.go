package factory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	authfeature "github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/domain/identity"
	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/persistence"
)

func TestNewApplicationAssemblesDependencies(t *testing.T) {
	settings := testConfig(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	app, err := NewApplication(context.Background(), settings, logger)
	if err != nil {
		t.Fatal(err)
	}
	closeApplication(t, app)
	if !reflect.DeepEqual(app.Config, settings) || app.Logger != logger {
		t.Fatal("configuration or configured logger was not retained")
	}
	if app.Database == nil || app.Router == nil || app.JobQueue == nil || app.Identifier == nil {
		t.Fatal("application dependencies are incomplete")
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", settings.CORSAllowedOrigins[0])
	app.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != settings.CORSAllowedOrigins[0] {
		t.Fatal("configured CORS origin was not retained")
	}
	if !strings.Contains(logs.String(), "http request handled") {
		t.Fatal("router did not use configured logger")
	}
	session := login(t, app.Router, settings.AdminPassword)
	for _, path := range []string{"/api/v1/rom", "/api/v1/library", "/api/v1/client", "/api/v1/i18n/en"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer "+session.AccessToken)
			response := httptest.NewRecorder()
			app.Router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
			}
		})
	}
}

func TestNewApplicationDefaultsLogger(t *testing.T) {
	settings := testConfig(t)
	settings.AdminEmail = ""
	settings.AdminPassword = ""
	app, err := NewApplication(context.Background(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeApplication(t, app)
	if app.Logger == nil {
		t.Fatal("nil logger was not defaulted")
	}
	if _, ok := app.Logger.Handler().(*slog.JSONHandler); !ok {
		t.Fatalf("default handler = %T, want JSON handler", app.Logger.Handler())
	}
	response := httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health = %d", response.Code)
	}
}

func TestNewApplicationRejectsInvalidDatabaseConfig(t *testing.T) {
	for _, driver := range []string{"unsupported", "sqlite", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			settings := testConfig(t)
			settings.DBDriver = driver
			settings.SQLitePath = ""
			settings.MySQLURL = "invalid"
			app, err := NewApplication(context.Background(), settings, nil)
			if err == nil || app != nil {
				t.Fatalf("NewApplication() = %v, %v; want nil application and error", app, err)
			}
		})
	}
}

func TestAssembleApplicationClosesDatabaseOnSeedFailure(t *testing.T) {
	for _, failure := range []string{"incomplete credentials", "seed lookup"} {
		t.Run(failure, func(t *testing.T) {
			settings := testConfig(t)
			database, err := persistence.OpenDatabase(context.Background(), settings.DBDriver, settings.SQLitePath, settings.MySQLURL)
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := database.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			ctx := context.Background()
			if failure == "incomplete credentials" {
				settings.AdminPassword = " "
			} else {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			app, err := assembleApplication(ctx, settings, slog.New(slog.NewTextHandler(io.Discard, nil)), database, persistence.NewRepositories(database))
			if err == nil || app != nil {
				t.Fatalf("assembly = %v, %v; want seed failure", app, err)
			}
			if failure == "seed lookup" && !errors.Is(err, context.Canceled) {
				t.Fatalf("seed error = %v, want context cancellation", err)
			}
			if err := sqlDB.Ping(); !errors.Is(err, sql.ErrConnDone) && (err == nil || !strings.Contains(err.Error(), "database is closed")) {
				t.Fatalf("Ping() after failed assembly = %v, want closed database", err)
			}
		})
	}
}

func TestNewApplicationSeedsAdminIdempotently(t *testing.T) {
	settings := testConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := NewApplication(context.Background(), settings, logger)
	if err != nil {
		t.Fatal(err)
	}
	closeApplication(t, first)
	before, err := persistence.NewRepositories(first.Database).FindByEmail(context.Background(), settings.AdminEmail)
	if err != nil || before == nil {
		t.Fatalf("seeded admin = %v, %v", before, err)
	}
	if before.Role != identity.RoleAdmin {
		t.Fatalf("seeded role = %q", before.Role)
	}
	sqlDB, err := first.Database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	settings.AdminPassword = "replacement password must not overwrite"
	second, err := NewApplication(context.Background(), settings, logger)
	if err != nil {
		t.Fatal(err)
	}
	closeApplication(t, second)
	after, err := persistence.NewRepositories(second.Database).FindByEmail(context.Background(), settings.AdminEmail)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("admin changed on rebuild: before=%+v after=%+v err=%v", before, after, err)
	}
	login(t, second.Router, "correct horse battery staple")
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		RuntimeEnv: "test", Host: "127.0.0.1", DBDriver: "sqlite",
		SQLitePath: filepath.Join(t.TempDir(), "factory.sqlite3"), StorageRoot: filepath.Join(t.TempDir(), "storage"),
		MaxUploadBytes: 1024, AllowedUploadMIMETypes: []string{"application/octet-stream"},
		AllowedUploadExtensions: []string{"sfc"}, CORSAllowedOrigins: []string{"https://example.com"},
		JWTSecret: strings.Repeat("s", 32), JWTAccessExpirySecs: 900, JWTRefreshExpirySecs: 604800,
		WorkerCount: 1, AdminEmail: "admin@example.com", AdminPassword: "correct horse battery staple",
	}
}

func closeApplication(t *testing.T, app *Application) {
	t.Helper()
	t.Cleanup(func() {
		sqlDB, err := app.Database.DB()
		if err != nil {
			t.Error(err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
}

func login(t *testing.T, router http.Handler, password string) authfeature.SessionResponse {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": "admin@example.com", "password": password})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}
	var session authfeature.SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("session tokens missing")
	}
	return session
}

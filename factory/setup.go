package factory

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/setup"
	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	httpadapter "github.com/lcmps/retroxide/interface/http"
	"github.com/lcmps/retroxide/interface/http/i18n"
	"gorm.io/gorm"
)

const mysqlTestTimeout = 5 * time.Second

var setupLockableKeys = []struct{ field, key string }{
	{field: "db_driver", key: "RETROXIDE_DB_DRIVER"},
	{field: "sqlite_path", key: "RETROXIDE_SQLITE_PATH"},
	{field: "mysql", key: "RETROXIDE_MYSQL_URL"},
	{field: "storage_root", key: "RETROXIDE_STORAGE_ROOT"},
}

type SetupService struct {
	configPath string
	process    config.Lookup
	logger     *slog.Logger
	onComplete func(*Application)

	mu        sync.Mutex
	completed bool
}

var _ setup.Service = (*SetupService)(nil)

// NewSetupService writes completed configuration to configPath; onComplete receives ownership of the built application.
func NewSetupService(configPath string, process config.Lookup, logger *slog.Logger, onComplete func(*Application)) *SetupService {
	if process == nil {
		process = func(string) (string, bool) { return "", false }
	}
	return &SetupService{configPath: configPath, process: process, logger: logger, onComplete: onComplete}
}

func NewSetupRouter(service setup.Service, logger *slog.Logger) (http.Handler, error) {
	locales, err := i18n.Catalogs()
	if err != nil {
		return nil, fault.Internal(fmt.Errorf("loading locale catalogs: %w", err))
	}
	return httpadapter.NewSetupRouter(service, locales, logger), nil
}

func (service *SetupService) Status() setup.Status {
	service.mu.Lock()
	completed := service.completed
	service.mu.Unlock()
	if completed {
		return setup.Status{Required: false}
	}
	fileValues, err := config.ReadFile(service.configPath)
	if err != nil {
		fileValues = nil
	}
	lookup := config.Layered(service.process, fileValues)
	value := func(key, fallback string) string {
		if current, ok := lookup(key); ok && current != "" {
			return current
		}
		return fallback
	}
	locked := []string{}
	for _, lockable := range setupLockableKeys {
		if _, ok := service.environmentValue(lockable.key); ok {
			locked = append(locked, lockable.field)
		}
	}
	return setup.Status{
		Required: true,
		Defaults: &setup.Defaults{
			DBDriver:    value("RETROXIDE_DB_DRIVER", setup.DriverSQLite),
			SQLitePath:  value("RETROXIDE_SQLITE_PATH", "./retroxide.sqlite3"),
			MySQL:       mysqlDefaults(value("RETROXIDE_MYSQL_URL", "")),
			StorageRoot: value("RETROXIDE_STORAGE_ROOT", "./storage"),
		},
		Locked: locked,
	}
}

func (service *SetupService) TestMySQL(ctx context.Context, connection setup.MySQLConnection) error {
	target, locked := service.environmentValue("RETROXIDE_MYSQL_URL")
	if !locked {
		if err := connection.Validate(); err != nil {
			return err
		}
		target = connection.URL()
	}
	ctx, cancel := context.WithTimeout(ctx, mysqlTestTimeout)
	defer cancel()
	if err := persistence.PingMySQL(ctx, target); err != nil {
		return setupFailure("MySQL connection failed", err)
	}
	return nil
}

func (service *SetupService) Complete(ctx context.Context, request setup.Request) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.completed {
		return fault.Conflict()
	}
	if err := request.Validate(); err != nil {
		return err
	}
	secret, err := newJWTSecret()
	if err != nil {
		return fault.Internal(err)
	}
	values := map[string]string{
		"RETROXIDE_JWT_SECRET":   secret,
		"RETROXIDE_DB_DRIVER":    request.DBDriver,
		"RETROXIDE_STORAGE_ROOT": strings.TrimSpace(request.StorageRoot),
	}
	if request.DBDriver == setup.DriverMySQL {
		values["RETROXIDE_MYSQL_URL"] = request.MySQL.URL()
	} else {
		values["RETROXIDE_SQLITE_PATH"] = strings.TrimSpace(request.SQLitePath)
	}

	fileValues, err := config.ReadFile(service.configPath)
	if err != nil {
		return fault.Internal(err)
	}
	candidate := maps.Clone(fileValues)
	maps.Copy(candidate, values)
	settings, err := config.Load(config.Layered(service.process, candidate))
	if err != nil {
		return fault.Validation("configuration is invalid: " + err.Error())
	}
	settings.AdminEmail = strings.TrimSpace(request.AdminEmail)
	settings.AdminPassword = request.AdminPassword

	application, err := NewApplication(ctx, settings, service.logger)
	if err != nil {
		return setupFailure("could not initialize the application", err)
	}
	if err := config.WriteFile(service.configPath, values); err != nil {
		closeDatabase(application.Database)
		return fault.Internal(fmt.Errorf("saving configuration: %w", err))
	}
	service.completed = true
	service.onComplete(application)
	return nil
}

func (service *SetupService) environmentValue(key string) (string, bool) {
	value, ok := service.process(key)
	return value, ok && value != ""
}

func mysqlDefaults(rawURL string) setup.MySQLConnection {
	defaults := setup.MySQLConnection{Host: "127.0.0.1", Port: 3306, User: "root", Database: "retroxide"}
	parsed, err := url.Parse(rawURL)
	if rawURL == "" || err != nil || parsed.Scheme != setup.DriverMySQL {
		return defaults
	}
	if host, port, err := net.SplitHostPort(parsed.Host); err == nil {
		defaults.Host = host
		if value, err := strconv.ParseUint(port, 10, 16); err == nil && value > 0 {
			defaults.Port = uint16(value)
		}
	} else if parsed.Host != "" {
		defaults.Host = parsed.Host
	}
	if parsed.User != nil && parsed.User.Username() != "" {
		defaults.User = parsed.User.Username()
	}
	if database := strings.Trim(parsed.Path, "/"); database != "" {
		defaults.Database = database
	}
	return defaults
}

func newJWTSecret() (string, error) {
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generating jwt secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

// setupFailure surfaces the underlying cause, since the installer needs it to fix connection settings.
func setupFailure(prefix string, err error) error {
	detail := err.Error()
	var appError *fault.Error
	if errors.As(err, &appError) {
		detail = appError.Message
		if appError.Cause != nil {
			detail = appError.Cause.Error()
		}
	}
	return fault.Validation(prefix + ": " + detail)
}

func closeDatabase(database *gorm.DB) {
	if sqlDB, err := database.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

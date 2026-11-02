package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Lookup func(key string) (string, bool)

const insecureProductionJWTSecret = "change-me-in-production-use-a-long-random-string"

var ErrSetupRequired = errors.New("configuration setup required")

type Config struct {
	RuntimeEnv              string
	Host                    string
	Port                    uint16
	DBDriver                string
	SQLitePath              string
	MySQLURL                string
	StorageRoot             string
	MaxUploadBytes          int64
	AllowedUploadMIMETypes  []string
	AllowedUploadExtensions []string
	JWTSecret               string
	JWTAccessExpirySecs     uint64
	JWTRefreshExpirySecs    uint64
	CORSAllowedOrigins      []string
	WorkerCount             int
	AdminEmail              string
	AdminPassword           string
}

func Load(lookup Lookup) (Config, error) {
	runtimeEnv := strings.ToLower(get(lookup, "RETROXIDE_ENV", "dev"))
	if runtimeEnv != "dev" && runtimeEnv != "development" && runtimeEnv != "prod" && runtimeEnv != "production" {
		return Config{}, invalid("RETROXIDE_ENV", "expected dev/development or prod/production")
	}
	host, port, err := ListenAddress(lookup)
	if err != nil {
		return Config{}, err
	}
	dbDriver := get(lookup, "RETROXIDE_DB_DRIVER", "sqlite")
	if dbDriver != "sqlite" && dbDriver != "mysql" {
		return Config{}, invalid("RETROXIDE_DB_DRIVER", "expected sqlite or mysql")
	}
	maxUploadBytes, err := unsigned(lookup, "RETROXIDE_MAX_UPLOAD_BYTES", 64*1024*1024, 63)
	if err != nil {
		return Config{}, err
	}
	if maxUploadBytes == 0 {
		return Config{}, invalid("RETROXIDE_MAX_UPLOAD_BYTES", "must be greater than zero")
	}
	accessExpiry, err := unsigned(lookup, "RETROXIDE_JWT_ACCESS_EXPIRY_SECS", 900, 64)
	if err != nil {
		return Config{}, err
	}
	if accessExpiry == 0 {
		return Config{}, invalid("RETROXIDE_JWT_ACCESS_EXPIRY_SECS", "must be greater than zero")
	}
	refreshExpiry, err := unsigned(lookup, "RETROXIDE_JWT_REFRESH_EXPIRY_SECS", 604800, 64)
	if err != nil {
		return Config{}, err
	}
	if refreshExpiry == 0 {
		return Config{}, invalid("RETROXIDE_JWT_REFRESH_EXPIRY_SECS", "must be greater than zero")
	}
	workerCount, err := unsigned(lookup, "RETROXIDE_WORKER_COUNT", 1, 8)
	if err != nil {
		return Config{}, err
	}
	if workerCount < 1 || workerCount > 64 {
		return Config{}, invalid("RETROXIDE_WORKER_COUNT", "must be between 1 and 64")
	}
	secret, ok := lookup("RETROXIDE_JWT_SECRET")
	if !ok || secret == "" {
		return Config{}, fmt.Errorf("%w: RETROXIDE_JWT_SECRET is not set", ErrSetupRequired)
	}
	if len(secret) < 32 {
		return Config{}, invalid("RETROXIDE_JWT_SECRET", "must be at least 32 characters long")
	}
	if (runtimeEnv == "prod" || runtimeEnv == "production") && secret == insecureProductionJWTSecret {
		return Config{}, invalid("RETROXIDE_JWT_SECRET", "must not use the built-in placeholder in production")
	}
	mimeTypes := parseList(get(lookup, "RETROXIDE_ALLOWED_UPLOAD_MIME_TYPES", "application/octet-stream"), true)
	if len(mimeTypes) == 0 {
		return Config{}, invalid("RETROXIDE_ALLOWED_UPLOAD_MIME_TYPES", "must contain at least one MIME type")
	}
	extensions := parseList(get(lookup, "RETROXIDE_ALLOWED_UPLOAD_EXTENSIONS", "rom,bin,nes,sfc,smc,gba,gb,gbc,n64,z64,v64,iso,cue,chd"), true)
	if len(extensions) == 0 {
		return Config{}, invalid("RETROXIDE_ALLOWED_UPLOAD_EXTENSIONS", "must contain at least one extension")
	}
	origins := parseList(get(lookup, "RETROXIDE_CORS_ALLOWED_ORIGINS", ""), false)
	adminEmail, hasEmail := lookup("RETROXIDE_ADMIN_EMAIL")
	adminPassword, hasPassword := lookup("RETROXIDE_ADMIN_PASSWORD")
	if hasEmail != hasPassword {
		return Config{}, invalid("RETROXIDE_ADMIN_EMAIL/RETROXIDE_ADMIN_PASSWORD", "must both be set or both unset")
	}
	if hasEmail && (strings.TrimSpace(adminEmail) == "" || strings.TrimSpace(adminPassword) == "") {
		return Config{}, invalid("RETROXIDE_ADMIN_EMAIL/RETROXIDE_ADMIN_PASSWORD", "must be non-empty")
	}
	return Config{
		RuntimeEnv: runtimeEnv, Host: host, Port: port,
		DBDriver:       dbDriver,
		SQLitePath:     get(lookup, "RETROXIDE_SQLITE_PATH", "./retroxide.sqlite3"),
		MySQLURL:       get(lookup, "RETROXIDE_MYSQL_URL", "mysql://root:root@127.0.0.1:3306/retroxide"),
		StorageRoot:    get(lookup, "RETROXIDE_STORAGE_ROOT", "./storage"),
		MaxUploadBytes: int64(maxUploadBytes), AllowedUploadMIMETypes: mimeTypes,
		AllowedUploadExtensions: extensions, JWTSecret: secret,
		JWTAccessExpirySecs: accessExpiry, JWTRefreshExpirySecs: refreshExpiry,
		CORSAllowedOrigins: origins, WorkerCount: int(workerCount),
		AdminEmail: adminEmail, AdminPassword: adminPassword,
	}, nil
}

func ListenAddress(lookup Lookup) (string, uint16, error) {
	port, err := unsigned(lookup, "RETROXIDE_PORT", 3000, 16)
	if err != nil {
		return "", 0, err
	}
	if port == 0 {
		return "", 0, invalid("RETROXIDE_PORT", "must be between 1 and 65535")
	}
	return get(lookup, "RETROXIDE_HOST", "0.0.0.0"), uint16(port), nil
}

func get(lookup Lookup, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

func unsigned(lookup Lookup, key string, fallback uint64, bits int) (uint64, error) {
	raw, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, bits)
	if err != nil {
		return 0, invalid(key, fmt.Sprintf("has invalid value %q", raw))
	}
	return value, nil
}

func parseList(raw string, lowercase bool) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if lowercase {
			value = strings.ToLower(value)
		}
		values = append(values, value)
	}
	return values
}

func invalid(key, message string) error {
	return fmt.Errorf("%s is invalid: %s", key, message)
}

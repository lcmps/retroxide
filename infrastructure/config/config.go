package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"github.com/lcmps/retroxide/application/settings"
	"github.com/lcmps/retroxide/domain/appearance"
	romdomain "github.com/lcmps/retroxide/domain/rom"
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
	TrustedProxies          []string
	WorkerCount             int
	// ScrapeWorkerCount workers fetch metadata for new ROMs, separately from WorkerCount.
	ScrapeWorkerCount int
	// MetricsToken, when set, serves Prometheus metrics at /metrics to requests bearing it.
	MetricsToken string
	// ChdmanPath locates MAME's chdman (a path, or a name looked up in PATH; default chdman).
	ChdmanPath string
	// CompressDiscs is the initial choice, on the upload page, to compress discs uploaded as
	// CUE/BIN or GDI to CHD.
	CompressDiscs bool
	// UIFont is the interface font: pixel, ubuntu, or system.
	UIFont        string
	AdminEmail    string
	AdminPassword string
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
	maxUploadBytes, err := maxUploadSize(lookup)
	if err != nil {
		return Config{}, err
	}
	accessExpiry, err := unsigned(lookup, "RETROXIDE_JWT_ACCESS_EXPIRY_SECS", 3600, 64)
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
	uiFont := strings.ToLower(strings.TrimSpace(get(lookup, settings.UIFontKey, settings.UIFontPixel)))
	if !appearance.IsFont(uiFont) {
		return Config{}, invalid(settings.UIFontKey, "expected pixel, ubuntu, or system")
	}
	scrapeWorkerCount, err := unsigned(lookup, "RETROXIDE_SCRAPE_WORKER_COUNT", 2, 8)
	if err != nil {
		return Config{}, err
	}
	if scrapeWorkerCount < 1 || scrapeWorkerCount > 64 {
		return Config{}, invalid("RETROXIDE_SCRAPE_WORKER_COUNT", "must be between 1 and 64")
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
	extensions := parseList(get(lookup, "RETROXIDE_ALLOWED_UPLOAD_EXTENSIONS", defaultUploadExtensions()), true)
	if len(extensions) == 0 {
		return Config{}, invalid("RETROXIDE_ALLOWED_UPLOAD_EXTENSIONS", "must contain at least one extension")
	}
	origins := parseList(get(lookup, "RETROXIDE_CORS_ALLOWED_ORIGINS", ""), false)
	trustedProxies := parseList(get(lookup, "RETROXIDE_TRUSTED_PROXIES", ""), false)
	for _, proxy := range trustedProxies {
		if !validProxyAddress(proxy) {
			return Config{}, invalid("RETROXIDE_TRUSTED_PROXIES", fmt.Sprintf("%q is not an IP address or CIDR range", proxy))
		}
	}
	metricsToken := strings.TrimSpace(get(lookup, MetricsTokenKey, ""))
	if metricsToken != "" && len(metricsToken) < minMetricsTokenLength {
		return Config{}, invalid(MetricsTokenKey, fmt.Sprintf("must be at least %d characters long", minMetricsTokenLength))
	}
	compressDiscs, err := boolean(lookup, CompressDiscsKey, false)
	if err != nil {
		return Config{}, err
	}
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
		MaxUploadBytes: maxUploadBytes, AllowedUploadMIMETypes: mimeTypes,
		AllowedUploadExtensions: extensions, JWTSecret: secret,
		JWTAccessExpirySecs: accessExpiry, JWTRefreshExpirySecs: refreshExpiry,
		CORSAllowedOrigins: origins, TrustedProxies: trustedProxies, WorkerCount: int(workerCount),
		ScrapeWorkerCount: int(scrapeWorkerCount), UIFont: uiFont, MetricsToken: metricsToken,
		ChdmanPath: strings.TrimSpace(get(lookup, ChdmanPathKey, "")), CompressDiscs: compressDiscs,
		AdminEmail: adminEmail, AdminPassword: adminPassword,
	}, nil
}

// Keys of the disc settings.
const (
	ChdmanPathKey    = "RETROXIDE_CHDMAN_PATH"
	CompressDiscsKey = "RETROXIDE_COMPRESS_DISCS"
)

// boolean reads a true/false setting.
func boolean(lookup Lookup, key string, fallback bool) (bool, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, invalid(key, "must be true or false")
	}
	return value, nil
}

// MetricsTokenKey configures the bearer token that unlocks /metrics.
const MetricsTokenKey = "RETROXIDE_METRICS_TOKEN"

// minMetricsTokenLength keeps the token from being guessed.
const minMetricsTokenLength = 16

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

func validProxyAddress(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(value)
	return err == nil
}

func invalid(key, message string) error {
	return fmt.Errorf("%s is invalid: %s", key, message)
}

// defaultUploadExtensions allows generic ROM containers plus every extension that identifies a
// known platform.
func defaultUploadExtensions() string {
	return strings.Join(append([]string{"rom", "bin"}, romdomain.PlatformExtensions()...), ",")
}

const (
	// MaxUploadSizeKey sets the largest accepted upload in megabytes, e.g. "1024MB" (1 MB is
	// 1024 × 1024 bytes). A bare number means megabytes; a "GB" suffix is accepted too.
	MaxUploadSizeKey = "RETROXIDE_MAX_UPLOAD_SIZE"
	// legacyMaxUploadBytesKey is the former byte-count setting, honoured only when
	// MaxUploadSizeKey is unset so existing configurations keep working.
	legacyMaxUploadBytesKey = "RETROXIDE_MAX_UPLOAD_BYTES"
	defaultMaxUploadMB      = 64
	bytesPerMB              = 1 << 20
)

func maxUploadSize(lookup Lookup) (int64, error) {
	if raw, ok := lookup(MaxUploadSizeKey); ok {
		size, err := ParseMegabytes(raw)
		if err != nil {
			return 0, invalid(MaxUploadSizeKey, err.Error())
		}
		return size, nil
	}
	if _, ok := lookup(legacyMaxUploadBytesKey); ok {
		size, err := unsigned(lookup, legacyMaxUploadBytesKey, 0, 63)
		if err != nil {
			return 0, err
		}
		if size == 0 {
			return 0, invalid(legacyMaxUploadBytesKey, "must be greater than zero")
		}
		return int64(size), nil
	}
	return defaultMaxUploadMB * bytesPerMB, nil
}

// ParseMegabytes reads a size such as "1024MB", "1024", or "4GB" and returns it in bytes.
func ParseMegabytes(raw string) (int64, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	multiplier := int64(bytesPerMB)
	switch {
	case strings.HasSuffix(value, "GB"):
		value, multiplier = strings.TrimSuffix(value, "GB"), 1<<30
	case strings.HasSuffix(value, "MB"):
		value = strings.TrimSuffix(value, "MB")
	}
	amount, err := strconv.ParseUint(strings.TrimSpace(value), 10, 63)
	if err != nil || amount == 0 {
		return 0, fmt.Errorf("has invalid value %q: expected a positive size in megabytes, such as 1024MB", raw)
	}
	if amount > uint64(math.MaxInt64/multiplier) {
		return 0, fmt.Errorf("has invalid value %q: size is too large", raw)
	}
	return int64(amount) * multiplier, nil
}

// FormatMegabytes renders a byte count the way RETROXIDE_MAX_UPLOAD_SIZE is written, rounding
// up to whole megabytes.
func FormatMegabytes(bytes int64) string {
	return strconv.FormatInt((bytes+bytesPerMB-1)/bytesPerMB, 10) + "MB"
}

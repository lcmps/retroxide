package config

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/settings"
)

// settingDefinition describes a configuration variable shown on the settings page.
type settingDefinition struct {
	key, label, description, group string
	// editable settings can be changed from the web interface; the rest affect where data
	// lives or how the server is reached, and are changed in the configuration file.
	editable bool
	// public settings are shown to every user; the rest only to administrators.
	public bool
	// live settings apply as soon as they are saved, since the server re-reads them.
	live    bool
	options []settings.Option
	value   func(Config) string
	// aliases are older keys that configure the same setting.
	aliases []string
}

var settingDefinitions = []settingDefinition{
	{key: settings.UIFontKey, label: "Default interface font", group: "appearance", editable: true, public: true, live: true,
		description: "Font for everyone who has not picked their own in their profile. Applies immediately.",
		options: []settings.Option{
			{Value: settings.UIFontPixel, Label: "Press Start 2P (pixel)"},
			{Value: settings.UIFontUbuntu, Label: "Ubuntu"},
			{Value: settings.UIFontSystem, Label: "System font"},
		},
		value: func(c Config) string { return c.UIFont }},
	{key: "RETROXIDE_ENV", label: "Environment", group: "server", public: true,
		description: "dev or prod. Production refuses the development JWT secret.",
		value:       func(c Config) string { return c.RuntimeEnv }},
	{key: "RETROXIDE_HOST", label: "Listen host", group: "server",
		description: "Address the HTTP server binds to.",
		value:       func(c Config) string { return c.Host }},
	{key: "RETROXIDE_PORT", label: "Listen port", group: "server",
		description: "Port the HTTP server listens on.",
		value:       func(c Config) string { return strconv.Itoa(int(c.Port)) }},
	{key: "RETROXIDE_TRUSTED_PROXIES", label: "Trusted proxies", group: "server", editable: true,
		description: "Comma-separated proxy IPs or CIDR ranges whose forwarding headers are trusted.",
		value:       func(c Config) string { return strings.Join(c.TrustedProxies, ",") }},
	{key: "RETROXIDE_CORS_ALLOWED_ORIGINS", label: "CORS allowed origins", group: "server", editable: true,
		description: "Comma-separated origins allowed to call the API from a browser; * allows any.",
		value:       func(c Config) string { return strings.Join(c.CORSAllowedOrigins, ",") }},
	{key: "RETROXIDE_DB_DRIVER", label: "Database driver", group: "storage",
		description: "sqlite or mysql.",
		value:       func(c Config) string { return c.DBDriver }},
	{key: "RETROXIDE_SQLITE_PATH", label: "SQLite database file", group: "storage",
		description: "Path of the SQLite database, when the driver is sqlite.",
		value:       func(c Config) string { return c.SQLitePath }},
	{key: "RETROXIDE_MYSQL_URL", label: "MySQL connection", group: "storage",
		description: "MySQL connection URL, when the driver is mysql. The password is never shown.",
		value:       func(c Config) string { return redactURLPassword(c.MySQLURL) }},
	{key: "RETROXIDE_STORAGE_ROOT", label: "ROM storage folder", group: "storage",
		description: "Folder that holds uploaded ROMs and artwork.",
		value:       func(c Config) string { return c.StorageRoot }},
	{key: MaxUploadSizeKey, label: "Maximum upload size", group: "uploads", editable: true, public: true,
		description: "Largest ROM that can be uploaded, in megabytes, e.g. 1024MB for 1 GB.",
		value:       func(c Config) string { return FormatMegabytes(c.MaxUploadBytes) },
		aliases:     []string{legacyMaxUploadBytesKey}},
	{key: "RETROXIDE_ALLOWED_UPLOAD_EXTENSIONS", label: "Allowed file extensions", group: "uploads", editable: true, public: true,
		description: "Comma-separated extensions accepted for upload.",
		value:       func(c Config) string { return strings.Join(c.AllowedUploadExtensions, ",") }},
	{key: "RETROXIDE_ALLOWED_UPLOAD_MIME_TYPES", label: "Allowed MIME types", group: "uploads", editable: true, public: true,
		description: "Comma-separated MIME types accepted when the extension is not listed.",
		value:       func(c Config) string { return strings.Join(c.AllowedUploadMIMETypes, ",") }},
	{key: "RETROXIDE_JWT_ACCESS_EXPIRY_SECS", label: "Access token lifetime", group: "sessions", editable: true, public: true,
		description: "Seconds an access token stays valid before it is refreshed.",
		value:       func(c Config) string { return strconv.FormatUint(c.JWTAccessExpirySecs, 10) }},
	{key: "RETROXIDE_JWT_REFRESH_EXPIRY_SECS", label: "Session lifetime", group: "sessions", editable: true, public: true,
		description: "Seconds a sign-in lasts without activity.",
		value:       func(c Config) string { return strconv.FormatUint(c.JWTRefreshExpirySecs, 10) }},
	{key: "RETROXIDE_WORKER_COUNT", label: "Identification workers", group: "workers", editable: true, public: true,
		description: "Background workers that turn uploads into ROMs (1-64).",
		value:       func(c Config) string { return strconv.Itoa(c.WorkerCount) }},
	{key: "RETROXIDE_SCRAPE_WORKER_COUNT", label: "Scrape workers", group: "workers", editable: true, public: true,
		description: "Background workers that fetch metadata and artwork for new ROMs (1-64).",
		value:       func(c Config) string { return strconv.Itoa(c.ScrapeWorkerCount) }},
}

// SettingsStore exposes the configuration of a running application and persists edits to its
// configuration file. Edits apply on the next start; the running Config never changes.
type SettingsStore struct {
	path    string
	process Lookup
	running Config
	mu      sync.Mutex
}

var _ settings.Store = (*SettingsStore)(nil)

func NewSettingsStore(path string, process Lookup, running Config) *SettingsStore {
	if process == nil {
		process = func(string) (string, bool) { return "", false }
	}
	return &SettingsStore{path: path, process: process, running: running}
}

func (store *SettingsStore) Settings() ([]settings.Setting, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	file, err := ReadFile(store.path)
	if err != nil {
		return nil, fault.Internal(err)
	}
	// An unreadable configuration would fail the next start; show the running values meanwhile.
	configured, err := Load(Layered(store.process, file))
	if err != nil {
		configured = store.running
	}
	result := make([]settings.Setting, 0, len(settingDefinitions))
	for _, definition := range settingDefinitions {
		result = append(result, settings.Setting{
			Key: definition.key, Label: definition.label, Description: definition.description, Group: definition.group,
			Value: definition.value(store.running), Configured: definition.value(configured),
			Source: store.source(definition, file), Editable: definition.editable, Public: definition.public,
			Live: definition.live, Options: definition.options,
		})
	}
	return result, nil
}

func (store *SettingsStore) Update(values map[string]string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	file, err := ReadFile(store.path)
	if err != nil {
		return fault.Internal(err)
	}
	set := make(map[string]string, len(values))
	var remove []string
	for key, value := range values {
		definition, ok := findSetting(key)
		if !ok || !definition.editable {
			return fault.Validation(key + " cannot be changed from the web interface")
		}
		// A setting replaces any older key for it, so the two cannot disagree.
		remove = append(remove, definition.aliases...)
		if value = strings.TrimSpace(value); value == "" {
			remove = append(remove, key)
		} else {
			set[key] = value
		}
	}

	next := make(map[string]string, len(file)+len(set))
	for key, value := range file {
		next[key] = value
	}
	for _, key := range remove {
		delete(next, key)
	}
	for key, value := range set {
		next[key] = value
	}
	if _, err := Load(Layered(store.process, next)); err != nil {
		if errors.Is(err, ErrSetupRequired) {
			return fault.Validation("the resulting configuration would be incomplete")
		}
		return fault.Validation(err.Error())
	}
	if err := UpdateFile(store.path, set, remove); err != nil {
		return fault.Internal(err)
	}
	return nil
}

func (store *SettingsStore) source(definition settingDefinition, file map[string]string) settings.Source {
	keys := append([]string{definition.key}, definition.aliases...)
	for _, key := range keys {
		if value, ok := store.process(key); ok && value != "" {
			// An alias in the environment loses to the setting in the file.
			if key == definition.key {
				return settings.SourceEnvironment
			}
			if _, inFile := file[definition.key]; !inFile {
				return settings.SourceEnvironment
			}
		}
	}
	for _, key := range keys {
		if _, ok := file[key]; ok {
			return settings.SourceConfigFile
		}
	}
	return settings.SourceDefault
}

func findSetting(key string) (settingDefinition, bool) {
	for _, definition := range settingDefinitions {
		if definition.key == key {
			return definition, true
		}
	}
	return settingDefinition{}, false
}

func redactURLPassword(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword(parsed.User.Username(), "redacted")
	}
	return parsed.String()
}

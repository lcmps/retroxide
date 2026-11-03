package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadUsesRustDefaultsAndSingleWorker(t *testing.T) {
	values := map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32)}
	config, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Host != "0.0.0.0" || config.Port != 3000 || config.DBDriver != "sqlite" || config.MaxUploadBytes != 64*1024*1024 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.WorkerCount != 1 || config.ScrapeWorkerCount != 2 || config.JWTAccessExpirySecs != 3600 || config.JWTRefreshExpirySecs != 604800 {
		t.Fatalf("unexpected concurrency/auth defaults: %+v", config)
	}
	if len(config.AllowedUploadExtensions) == 0 || len(config.AllowedUploadMIMETypes) == 0 {
		t.Fatalf("upload allowlists are empty: %+v", config)
	}
	if !slices.Contains(config.AllowedUploadExtensions, "sms") || !slices.Contains(config.AllowedUploadExtensions, "vpk") || !slices.Contains(config.AllowedUploadExtensions, "rom") {
		t.Fatalf("default extensions should cover the platform catalog: %v", config.AllowedUploadExtensions)
	}
	if len(config.TrustedProxies) != 0 {
		t.Fatalf("trusted proxies default = %v, want none", config.TrustedProxies)
	}
}

func TestLoadParsesTrustedProxies(t *testing.T) {
	values := map[string]string{
		"RETROXIDE_JWT_SECRET":      strings.Repeat("s", 32),
		"RETROXIDE_TRUSTED_PROXIES": " 10.0.0.0/8, 192.168.1.1 ,::1",
	}
	config, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(config.TrustedProxies, ",") != "10.0.0.0/8,192.168.1.1,::1" {
		t.Fatalf("trusted proxies = %v", config.TrustedProxies)
	}
}

func TestLoadRequiresSetupWhenSecretMissingOrEmpty(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"missing": {},
		"empty":   {"RETROXIDE_JWT_SECRET": ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(func(key string) (string, bool) {
				value, ok := values[key]
				return value, ok
			})
			if !errors.Is(err, ErrSetupRequired) {
				t.Fatalf("Load() error = %v, want ErrSetupRequired", err)
			}
		})
	}
	_, err := Load(func(key string) (string, bool) {
		if key == "RETROXIDE_JWT_SECRET" {
			return "short", true
		}
		return "", false
	})
	if err == nil || errors.Is(err, ErrSetupRequired) {
		t.Fatalf("invalid secret error = %v, want non-setup validation error", err)
	}
}

func TestWriteFileMergesQuotesAndRestrictsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "retroxide.env")
	if err := WriteFile(path, map[string]string{"KEEP": "original", "RETROXIDE_MYSQL_URL": `mysql://u:p#"ss@h:3306/db`}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, map[string]string{"RETROXIDE_JWT_SECRET": "secret value"}); err != nil {
		t.Fatal(err)
	}
	values, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["KEEP"] != "original" || values["RETROXIDE_MYSQL_URL"] != `mysql://u:p#"ss@h:3306/db` || values["RETROXIDE_JWT_SECRET"] != "secret value" {
		t.Fatalf("round-tripped values = %v", values)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestFilePathHonorsOverride(t *testing.T) {
	if got := FilePath(nil); got != ".env" {
		t.Fatalf("default path = %q", got)
	}
	got := FilePath(func(key string) (string, bool) { return "/data/retroxide.env", key == ConfigFileKey })
	if got != "/data/retroxide.env" {
		t.Fatalf("override path = %q", got)
	}
}

func TestLoadEnvironmentUsesDotEnvOnlyForUnsetValues(t *testing.T) {
	dotEnvPath := filepath.Join(t.TempDir(), ".env")
	contents := "RETROXIDE_HOST=from-file\nRETROXIDE_JWT_SECRET=" + strings.Repeat("s", 32) + "\n"
	if err := os.WriteFile(dotEnvPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadEnvironment(dotEnvPath, func(key string) (string, bool) {
		if key == "RETROXIDE_HOST" {
			return "from-process", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Host != "from-process" {
		t.Fatalf("host = %q, want process value", config.Host)
	}
	if config.JWTSecret != strings.Repeat("s", 32) {
		t.Fatalf("JWT secret = %q, want dotenv value", config.JWTSecret)
	}
}

func TestLoadRejectsInvalidExplicitValues(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "missing secret", values: map[string]string{}},
		{name: "short secret", values: map[string]string{"RETROXIDE_JWT_SECRET": "short"}},
		{name: "production placeholder secret", values: map[string]string{"RETROXIDE_ENV": "prod", "RETROXIDE_JWT_SECRET": "change-me-in-production-use-a-long-random-string"}},
		{name: "zero port", values: map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), "RETROXIDE_PORT": "0"}},
		{name: "unknown driver", values: map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), "RETROXIDE_DB_DRIVER": "postgres"}},
		{name: "worker count exceeds bound", values: map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), "RETROXIDE_WORKER_COUNT": "65"}},
		{name: "zero scrape workers", values: map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), "RETROXIDE_SCRAPE_WORKER_COUNT": "0"}},
		{name: "malformed trusted proxy", values: map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), "RETROXIDE_TRUSTED_PROXIES": "10.0.0.0/8,proxy.local"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(func(key string) (string, bool) {
				value, ok := test.values[key]
				return value, ok
			})
			if err == nil {
				t.Fatal("Load() succeeded, want validation error")
			}
		})
	}
}

func TestMaxUploadSizeIsReadInMegabytes(t *testing.T) {
	secret := strings.Repeat("s", 32)
	tests := []struct {
		name   string
		values map[string]string
		want   int64
	}{
		{name: "default", values: map[string]string{}, want: 64 << 20},
		{name: "megabytes with suffix", values: map[string]string{"RETROXIDE_MAX_UPLOAD_SIZE": "1024MB"}, want: 1 << 30},
		{name: "bare number is megabytes", values: map[string]string{"RETROXIDE_MAX_UPLOAD_SIZE": "512"}, want: 512 << 20},
		{name: "lowercase and spaced", values: map[string]string{"RETROXIDE_MAX_UPLOAD_SIZE": " 4096 mb "}, want: 4 << 30},
		{name: "gigabytes", values: map[string]string{"RETROXIDE_MAX_UPLOAD_SIZE": "4GB"}, want: 4 << 30},
		{name: "legacy bytes still honoured", values: map[string]string{"RETROXIDE_MAX_UPLOAD_BYTES": "1048576"}, want: 1 << 20},
		{name: "new setting wins over legacy", values: map[string]string{"RETROXIDE_MAX_UPLOAD_SIZE": "2MB", "RETROXIDE_MAX_UPLOAD_BYTES": "1048576"}, want: 2 << 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.values["RETROXIDE_JWT_SECRET"] = secret
			config, err := Load(func(key string) (string, bool) { value, ok := test.values[key]; return value, ok })
			if err != nil {
				t.Fatal(err)
			}
			if config.MaxUploadBytes != test.want {
				t.Fatalf("MaxUploadBytes = %d, want %d", config.MaxUploadBytes, test.want)
			}
		})
	}
	for _, raw := range []string{"0MB", "-5MB", "1.5GB", "lots", "MB", "1TB", "9999999999999999GB"} {
		_, err := Load(func(key string) (string, bool) {
			values := map[string]string{"RETROXIDE_JWT_SECRET": secret, "RETROXIDE_MAX_UPLOAD_SIZE": raw}
			value, ok := values[key]
			return value, ok
		})
		if err == nil || !strings.Contains(err.Error(), "RETROXIDE_MAX_UPLOAD_SIZE") {
			t.Errorf("RETROXIDE_MAX_UPLOAD_SIZE=%q error = %v, want a validation error naming the setting", raw, err)
		}
	}
	if got := FormatMegabytes(1 << 30); got != "1024MB" {
		t.Errorf("FormatMegabytes(1 GiB) = %q", got)
	}
}

func TestLoadAcceptsOnlyLongMetricsTokens(t *testing.T) {
	load := func(token string) (Config, error) {
		values := map[string]string{"RETROXIDE_JWT_SECRET": strings.Repeat("s", 32), MetricsTokenKey: token}
		return Load(func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		})
	}
	if config, err := load(""); err != nil || config.MetricsToken != "" {
		t.Fatalf("no token = %q, %v; want metrics off", config.MetricsToken, err)
	}
	if _, err := load("short"); err == nil {
		t.Fatal("a short metrics token was accepted")
	}
	if config, err := load(" " + strings.Repeat("m", 16) + " "); err != nil || config.MetricsToken != strings.Repeat("m", 16) {
		t.Fatalf("token = %q, %v", config.MetricsToken, err)
	}
}

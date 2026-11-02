package config

import (
	"os"
	"path/filepath"
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
	if config.WorkerCount != 1 || config.JWTAccessExpirySecs != 900 || config.JWTRefreshExpirySecs != 604800 {
		t.Fatalf("unexpected concurrency/auth defaults: %+v", config)
	}
	if len(config.AllowedUploadExtensions) == 0 || len(config.AllowedUploadMIMETypes) == 0 {
		t.Fatalf("upload allowlists are empty: %+v", config)
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

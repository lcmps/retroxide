package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/settings"
)

func settingsFixture(t *testing.T, fileContents string, environment map[string]string) (*SettingsStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "retroxide.env")
	if err := os.WriteFile(path, []byte(fileContents), 0o600); err != nil {
		t.Fatal(err)
	}
	process := func(key string) (string, bool) { value, ok := environment[key]; return value, ok }
	running, err := LoadEnvironment(path, process)
	if err != nil {
		t.Fatal(err)
	}
	return NewSettingsStore(path, process, running), path
}

func settingByKey(t *testing.T, store *SettingsStore, key string) settings.Setting {
	t.Helper()
	all, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range all {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("setting %s not listed", key)
	return settings.Setting{}
}

const settingsSecret = `RETROXIDE_JWT_SECRET="` + "0123456789abcdef0123456789abcdef" + `"` + "\n"

func TestSettingsStoreReportsValuesAndSources(t *testing.T) {
	store, _ := settingsFixture(t, settingsSecret+`RETROXIDE_MYSQL_URL="mysql://retro:hunter2@db:3306/retroxide"`+"\nRETROXIDE_WORKER_COUNT=\"3\"\n", map[string]string{"RETROXIDE_PORT": "8080"})

	if setting := settingByKey(t, store, "RETROXIDE_PORT"); setting.Value != "8080" || setting.Source != settings.SourceEnvironment {
		t.Fatalf("port = %+v", setting)
	}
	if setting := settingByKey(t, store, "RETROXIDE_WORKER_COUNT"); setting.Value != "3" || setting.Source != settings.SourceConfigFile || !setting.Editable {
		t.Fatalf("worker count = %+v", setting)
	}
	if setting := settingByKey(t, store, MaxUploadSizeKey); setting.Value != "64MB" || setting.Source != settings.SourceDefault || !setting.Public {
		t.Fatalf("upload size = %+v", setting)
	}
	if setting := settingByKey(t, store, "RETROXIDE_MYSQL_URL"); strings.Contains(setting.Value, "hunter2") || setting.Public {
		t.Fatalf("MySQL URL must be redacted and admin-only: %+v", setting)
	}
	all, _ := store.Settings()
	for _, setting := range all {
		if setting.Key == "RETROXIDE_JWT_SECRET" || strings.Contains(setting.Value, "0123456789abcdef") {
			t.Fatalf("the JWT secret must never be listed: %+v", setting)
		}
	}
}

func TestSettingsStoreUpdateValidatesPersistsAndLeavesTheRunningValue(t *testing.T) {
	store, path := settingsFixture(t, settingsSecret+"RETROXIDE_MAX_UPLOAD_BYTES=\"1048576\"\n", nil)

	err := store.Update(map[string]string{"RETROXIDE_WORKER_COUNT": "99"})
	if !fault.IsCode(err, fault.CodeValidation) || !strings.Contains(err.Error(), "RETROXIDE_WORKER_COUNT") {
		t.Fatalf("invalid worker count = %v, want a validation error naming the setting", err)
	}
	if err := store.Update(map[string]string{"RETROXIDE_STORAGE_ROOT": "/elsewhere"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("read-only setting = %v, want validation error", err)
	}

	if err := store.Update(map[string]string{MaxUploadSizeKey: "4096MB", "RETROXIDE_SCRAPE_WORKER_COUNT": "4"}); err != nil {
		t.Fatal(err)
	}
	upload := settingByKey(t, store, MaxUploadSizeKey)
	if upload.Value != "1MB" || upload.Configured != "4096MB" || upload.Source != settings.SourceConfigFile {
		t.Fatalf("upload size after update = %+v; want the running value kept and the new one pending", upload)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	written := string(contents)
	if !strings.Contains(written, `RETROXIDE_MAX_UPLOAD_SIZE="4096MB"`) || strings.Contains(written, "RETROXIDE_MAX_UPLOAD_BYTES") || !strings.Contains(written, "RETROXIDE_JWT_SECRET") {
		t.Fatalf("config file = %q; want the new size, the legacy key removed, and other values kept", written)
	}

	if err := store.Update(map[string]string{"RETROXIDE_SCRAPE_WORKER_COUNT": ""}); err != nil {
		t.Fatal(err)
	}
	if setting := settingByKey(t, store, "RETROXIDE_SCRAPE_WORKER_COUNT"); setting.Source != settings.SourceDefault || setting.Configured != "2" {
		t.Fatalf("cleared scrape workers = %+v; want the default restored", setting)
	}
}

func TestSettingsStoreUIFontIsValidatedAndLive(t *testing.T) {
	store, _ := settingsFixture(t, settingsSecret, nil)
	if err := store.Update(map[string]string{settings.UIFontKey: "comic-sans"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("unknown font = %v, want validation error", err)
	}
	if err := store.Update(map[string]string{settings.UIFontKey: "system"}); err != nil {
		t.Fatal(err)
	}
	font := settingByKey(t, store, settings.UIFontKey)
	if font.Configured != "system" || !font.Live || !font.Public || len(font.Options) != 3 {
		t.Fatalf("font setting = %+v", font)
	}
}

// RETROXIDE_CHDMAN_PATH was a setting until compression became built in; installs keep it around.
func TestStaleChdmanPathIsIgnored(t *testing.T) {
	const stale = "RETROXIDE_CHDMAN_PATH"
	store, path := settingsFixture(t, settingsSecret+stale+"=/usr/bin/chdman\n", map[string]string{stale: "/usr/bin/chdman"})
	all, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range all {
		if setting.Key == stale {
			t.Fatalf("%s is still listed", stale)
		}
	}
	if err := store.Update(map[string]string{CompressDiscsKey: "true"}); err != nil {
		t.Fatalf("saving another setting = %v", err)
	}
	if written, _ := os.ReadFile(path); !strings.Contains(string(written), CompressDiscsKey+`="true"`) {
		t.Fatalf("the other setting was not saved:\n%s", written)
	}
	if err := store.Update(map[string]string{stale: "/x"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("changing %s = %v, want a validation error", stale, err)
	}
}

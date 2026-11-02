package factory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/infrastructure/config"
)

func TestNewConfigPreservesProcessEnvironmentLoading(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("RETROXIDE_HOST=dotenv.example\nRETROXIDE_PORT=4321\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETROXIDE_JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("RETROXIDE_PORT", "5432")
	t.Setenv("RETROXIDE_HOST", "process.example")
	want, wantErr := config.LoadProcessEnvironment()
	got, err := NewConfig()
	if wantErr != nil || err != nil {
		t.Fatalf("configuration errors: direct=%v factory=%v", wantErr, err)
	}
	if !reflect.DeepEqual(got, want) || got.Port != 5432 || got.Host != "process.example" {
		t.Fatalf("NewConfig() = %+v, want %+v with process overrides", got, want)
	}
	t.Setenv("RETROXIDE_PORT", "invalid")
	_, wantErr = config.LoadProcessEnvironment()
	_, err = NewConfig()
	if err == nil || wantErr == nil || err.Error() != wantErr.Error() {
		t.Fatalf("configuration validation errors: direct=%v factory=%v", wantErr, err)
	}
}

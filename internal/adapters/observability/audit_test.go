package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestAuditLoggerWritesRustCompatibleFields(t *testing.T) {
	var output bytes.Buffer
	logger := NewAuditLogger(slog.New(slog.NewJSONHandler(&output, nil)))
	sha256 := strings.Repeat("a", 64)
	logger.UploadAccepted(context.Background(), "upl-1", "rf-1", sha256, 123)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["event"] != "audit" || record["action"] != "upload_accepted" || record["upload_id"] != "upl-1" || record["rom_file_id"] != "rf-1" {
		t.Fatalf("audit record = %#v", record)
	}
	if record["sha256_prefix"] != strings.Repeat("a", 12) || record["size_bytes"] != float64(123) {
		t.Fatalf("audit hashes/size = %#v", record)
	}
}

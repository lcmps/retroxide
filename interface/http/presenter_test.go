package http

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
)

func TestNewRequestIDUsesRustFormat(t *testing.T) {
	requestID := NewRequestID()
	if !regexp.MustCompile(`^req_[0-9]+_[0-9]+$`).MatchString(requestID) {
		t.Fatalf("request id = %q, want req_<micros>_<counter>", requestID)
	}
}

func TestPresentErrorPreservesConflictAndHidesInternalDetails(t *testing.T) {
	fileID := "rf-existing"
	sha256 := "abc123"
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
		fileID  *string
		sha256  *string
	}{
		{name: "duplicate rom", err: fault.ROMAlreadyExists(fileID, sha256), status: 409, code: "rom_already_exists", message: "rom already exists", fileID: &fileID, sha256: &sha256},
		{name: "internal error hides cause", err: fault.Internal(assertionError{}), status: 500, code: "internal_error", message: "internal error"},
		{name: "not found", err: fault.NotFound(), status: 404, code: "not_found", message: "not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := PresentError(test.err, "req_1_2")
			if status != test.status || response.Code != test.code || response.Message != test.message || response.RequestID != "req_1_2" {
				t.Fatalf("presentation = (%d, %+v)", status, response)
			}
			if !sameStringPointer(response.ExistingROMFileID, test.fileID) || !sameStringPointer(response.SHA256, test.sha256) {
				t.Fatalf("conflict fields = %v/%v, want %v/%v", response.ExistingROMFileID, response.SHA256, test.fileID, test.sha256)
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if test.fileID == nil && (regexp.MustCompile(`existing_rom_file_id|"sha256"`).Match(encoded)) {
				t.Fatalf("non-conflict response includes optional fields: %s", encoded)
			}
		})
	}
}

func TestHTTPStatusForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "validation", err: fault.Validation("invalid field"), want: http.StatusBadRequest},
		{name: "unauthorized", err: fault.Unauthorized(), want: http.StatusUnauthorized},
		{name: "forbidden", err: fault.Forbidden(), want: http.StatusForbidden},
		{name: "not found", err: fault.NotFound(), want: http.StatusNotFound},
		{name: "conflict", err: fault.Conflict(), want: http.StatusConflict},
		{name: "duplicate rom", err: fault.ROMAlreadyExists("rf-1", "sha-1"), want: http.StatusConflict},
		{name: "upload too large", err: fault.UploadTooLarge(20, 10), want: http.StatusRequestEntityTooLarge},
		{name: "unsupported upload", err: fault.UnsupportedUploadType("blocked"), want: http.StatusUnsupportedMediaType},
		{name: "internal", err: fault.Internal(nil), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, _ := PresentError(test.err, "req_1_2")
			if status != test.want {
				t.Fatalf("status = %d, want %d", status, test.want)
			}
		})
	}
}

func TestErrorResponseOmitsConflictFields(t *testing.T) {
	response := ErrorResponse{Code: "not_found", Message: "not found", RequestID: "req-1"}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"code":"not_found","message":"not found","request_id":"req-1"}`; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func sameStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

type assertionError struct{}

func (assertionError) Error() string { return "database password leaked if exposed" }

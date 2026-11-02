package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalErrorLogsCauseWithoutExposingIt(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(Dependencies{Logger: logger}, Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"email":"test@example.com","password":"test"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	if !strings.Contains(logs.String(), "service is unavailable") || !strings.Contains(logs.String(), response.Header().Get("x-request-id")) {
		t.Fatalf("missing correlated internal error: %s", logs.String())
	}
	if strings.Contains(response.Body.String(), "service is unavailable") {
		t.Fatal("internal cause exposed in HTTP response")
	}
}

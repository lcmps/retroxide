package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/domain/identity"
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

// A frontend handler and image service for the logging test: both answer 200 for known paths.
type loggingTestFrontend struct{}

func (loggingTestFrontend) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/_app/immutable/x.js" {
		writer.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(writer, request)
}

func TestRequestLoggingSkipsSuccessfulAssetAndImageRequests(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(Dependencies{
		Logger:        logger,
		ROMs:          &imageTestROMs{},
		ImageLinks:    imageTestLinks{},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}},
	}, Options{Frontend: loggingTestFrontend{}})
	serve := func(path, authorization string) int {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	for _, quiet := range []string{"/_app/immutable/x.js", "/api/v1/rom/rom-1/cover?token=img:user-7:rom-1"} {
		logs.Reset()
		if status := serve(quiet, ""); status != http.StatusOK {
			t.Fatalf("%s = %d, want 200", quiet, status)
		}
		if strings.Contains(logs.String(), "http request handled") {
			t.Fatalf("successful %s was logged at info:\n%s", quiet, logs.String())
		}
	}
	for _, loud := range []struct{ path, authorization string }{
		{"/_app/missing.js", ""},
		{"/api/v1/rom/rom-9/cover?token=img:user-7:rom-9", ""}, // 404 from the service
		{"/api/v1/platforms/catalog", "Bearer access"},
	} {
		logs.Reset()
		serve(loud.path, loud.authorization)
		if !strings.Contains(logs.String(), "http request handled") {
			t.Fatalf("%s was not logged:\n%s", loud.path, logs.String())
		}
	}
}

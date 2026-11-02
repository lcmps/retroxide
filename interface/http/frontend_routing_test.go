package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/application/setup"
)

var frontendStub = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
	_, _ = writer.Write([]byte("frontend"))
})

type setupStub struct{}

func (setupStub) Status() setup.Status                                   { return setup.Status{Required: true} }
func (setupStub) TestMySQL(context.Context, setup.MySQLConnection) error { return nil }
func (setupStub) Complete(context.Context, setup.Request) error          { return nil }

func request(router http.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, target, nil))
	return response
}

func TestRouterServesFrontendOutsideAPIWithoutAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Authenticator: rejectingAuthenticator}, Options{Frontend: frontendStub})

	for _, target := range []string{"/", "/library", "/rom/rom_1", "/_app/immutable/app.js"} {
		if response := request(router, http.MethodGet, target); response.Code != http.StatusOK || response.Body.String() != "frontend" {
			t.Fatalf("GET %s = %d %q, want frontend", target, response.Code, response.Body.String())
		}
	}
	if response := request(router, http.MethodGet, "/api/v1/rom"); response.Code != http.StatusUnauthorized {
		t.Fatalf("protected API = %d, want 401", response.Code)
	}
	for _, target := range []string{"/api/v1/does-not-exist", "/api"} {
		response := request(router, http.MethodGet, target)
		if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "frontend") {
			t.Fatalf("GET %s fell through to the frontend: %d %q", target, response.Code, response.Body.String())
		}
	}
	if response := request(router, http.MethodGet, "/health"); response.Body.String() != "ok" {
		t.Fatalf("health = %q", response.Body.String())
	}
}

func TestSetupRouterServesFrontendAndBlocksAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewSetupRouter(setupStub{}, map[string][]byte{}, nil, frontendStub)

	if response := request(router, http.MethodGet, "/setup"); response.Body.String() != "frontend" {
		t.Fatalf("setup page = %d %q", response.Code, response.Body.String())
	}
	response := request(router, http.MethodGet, "/api/v1/rom")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "setup_required") {
		t.Fatalf("API during setup = %d %q", response.Code, response.Body.String())
	}

	withoutFrontend := NewSetupRouter(setupStub{}, map[string][]byte{}, nil, nil)
	if response := request(withoutFrontend, http.MethodGet, "/setup"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("setup page without frontend = %d, want 503", response.Code)
	}
}

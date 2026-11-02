package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterSetsSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, Options{})
	for path, wantCSP := range map[string]string{
		"/api/v1/rom": apiContentSecurityPolicy,
		"/health":     frontendContentSecurityPolicy,
	} {
		response := request(router, http.MethodGet, path)
		header := response.Header()
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("X-Frame-Options") != "DENY" || header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s hardening headers = %v", path, header)
		}
		if got := header.Get("Content-Security-Policy"); got != wantCSP {
			t.Fatalf("%s CSP = %q, want %q", path, got, wantCSP)
		}
	}
}

func TestRouterRejectsOversizedJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{})
	body := `{"email":"user@example.com","password":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "request body is too large") {
		t.Fatalf("oversized body response = %d %s", response.Code, response.Body.String())
	}
}

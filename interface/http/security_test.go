package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func postLogin(router http.Handler, remoteAddr, forwardedFor string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"email":"user@example.com","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

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

// Web workers do not inherit the page's <meta> policy, so worker scripts get their own header:
// WebAssembly (the upload hash) may compile there, and the worker may load nothing else.
func TestRouterSetsWorkerScriptPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, Options{})
	got := request(router, http.MethodGet, "/_app/immutable/workers/hash.worker-abc123.js").Header().Get("Content-Security-Policy")
	want := "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; frame-ancestors 'none'"
	if got != want {
		t.Fatalf("worker CSP = %q, want %q", got, want)
	}
	// Only workers may compile WebAssembly; pages keep their stricter policy.
	if page := request(router, http.MethodGet, "/_app/immutable/chunks/app.js").Header().Get("Content-Security-Policy"); strings.Contains(page, "wasm") {
		t.Fatalf("non-worker asset CSP = %q allows WebAssembly", page)
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

func TestRouterThrottlesLoginPerClientAndIgnoresUntrustedForwardingHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{})
	for attempt := 1; attempt <= credentialBurst; attempt++ {
		// Rotating X-Forwarded-For must not mint fresh buckets when no proxy is trusted.
		response := postLogin(router, "192.0.2.10:1234", "198.51.100."+string(rune('0'+attempt%10)))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s, want 401", attempt, response.Code, response.Body.String())
		}
	}
	throttled := postLogin(router, "192.0.2.10:1234", "203.0.113.99")
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("throttled response = %d %s, want 429 rate_limited", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("throttled response is missing Retry-After")
	}
	if other := postLogin(router, "192.0.2.20:1234", ""); other.Code != http.StatusUnauthorized {
		t.Fatalf("separate client = %d, want its own bucket", other.Code)
	}
}

func TestRouterUsesForwardedClientIPFromTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{TrustedProxies: []string{"10.0.0.0/8"}})
	for attempt := 0; attempt < credentialBurst; attempt++ {
		postLogin(router, "10.0.0.5:1234", "198.51.100.1")
	}
	if response := postLogin(router, "10.0.0.5:1234", "198.51.100.1"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("same forwarded client = %d, want 429", response.Code)
	}
	if response := postLogin(router, "10.0.0.5:1234", "198.51.100.2"); response.Code != http.StatusUnauthorized {
		t.Fatalf("different forwarded client behind trusted proxy = %d, want 401", response.Code)
	}
}

func TestAttemptLimiterRefillsAndForgetsIdleClients(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := newAttemptLimiter(time.Second, 1)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("first attempt was rejected")
	}
	allowed, retryAfter := limiter.allow("client")
	if allowed || retryAfter <= 0 || retryAfter > time.Second {
		t.Fatalf("second attempt allowed=%v retryAfter=%v, want rejection within one second", allowed, retryAfter)
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("attempt after refill was rejected")
	}

	now = now.Add(limiterIdleTTL)
	limiter.allow("other")
	if _, tracked := limiter.entries["client"]; tracked {
		t.Fatal("idle client entry was not evicted")
	}
}

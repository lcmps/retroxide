package http

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/internal/features/auth"
	"github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type routerTestTokens struct{}

func (routerTestTokens) Issue(string, string) (auth.TokenPair, error) {
	return auth.TokenPair{}, errors.New("unexpected issue")
}
func (routerTestTokens) VerifyAccess(string) (auth.AccessClaims, error) {
	return auth.AccessClaims{}, fault.Unauthorized()
}
func (routerTestTokens) VerifyRefresh(string) (auth.AccessClaims, error) {
	return auth.AccessClaims{}, fault.Unauthorized()
}

type routerTestClients struct{}

func (routerTestClients) FindByTokenHash(context.Context, string) (*auth.Client, error) {
	return nil, nil
}
func (routerTestClients) FindByUserID(context.Context, string) ([]auth.Client, error) {
	return nil, nil
}
func (routerTestClients) SaveClient(context.Context, auth.Client) error { return nil }
func (routerTestClients) DeleteClient(context.Context, string) error      { return nil }

type routerTestSessions struct{}

func (routerTestSessions) Login(_ context.Context, input auth.LoginRequest) (auth.SessionResponse, error) {
	if input.Password == "wrong" {
		return auth.SessionResponse{}, fault.Unauthorized()
	}
	if input.Email == "" || input.Password == "" {
		return auth.SessionResponse{}, fault.Validation("email and password are required")
	}
	return auth.SessionResponse{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 900}, nil
}
func (routerTestSessions) Refresh(context.Context, string) (auth.SessionResponse, error) {
	return auth.SessionResponse{}, fault.Unauthorized()
}
func (routerTestSessions) Logout(context.Context, identity.Requester) error { return nil }

func TestRouterHealthAndRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health response = %d %q", response.Code, response.Body.String())
	}
	if requestID := response.Header().Get("x-request-id"); requestID == "" {
		t.Fatal("health response is missing x-request-id")
	}
}

func TestRouterProtectsROMRoutesWithStandardUnauthorizedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Tokens: routerTestTokens{}, ClientRepo: routerTestClients{}}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/rom", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
	}
	if response.Header().Get("x-request-id") == "" {
		t.Fatal("unauthorized response is missing x-request-id")
	}
	if got := response.Body.String(); got == "" || got[0] != '{' {
		t.Fatalf("unauthorized body is not JSON: %q", got)
	}
}

func TestRouterAllowsPublicSessionPathPastAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Tokens: routerTestTokens{}, ClientRepo: routerTestClients{}}, Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == http.StatusUnauthorized {
		t.Fatalf("public session path was rejected by auth middleware: %s", response.Body.String())
	}
}

func TestRouterServesPublicLocaleWithCacheHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Locales: map[string][]byte{"en": []byte(`{"message":"ok"}`)}}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/i18n/en", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"message":"ok"}` {
		t.Fatalf("locale response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "public, max-age=3600" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("locale headers = %#v", response.Header())
	}
}

func TestRouterBindsJSONWithCharsetOnPublicSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"email":"user@example.com","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"token_type":"Bearer"`) {
		t.Fatalf("session response = %d %s", response.Code, response.Body.String())
	}
}

func TestRouterUsesSessionSpecificUnauthorizedErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{})
	tests := []struct {
		name string
		path string
		body string
		code string
		msg  string
	}{
		{name: "invalid credentials", path: "/api/v1/session", body: `{"email":"user@example.com","password":"wrong"}`, code: "invalid_credentials", msg: "invalid credentials"},
		{name: "invalid refresh", path: "/api/v1/session/refresh", body: `{"refresh_token":"expired"}`, code: "invalid_refresh_token", msg: "refresh token is invalid or expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || !strings.Contains(response.Body.String(), `"message":"`+test.msg+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if response.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("session handler unexpectedly set WWW-Authenticate: %q", response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestRouterWrongJSONContentTypeReturns415Envelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Sessions: routerTestSessions{}}, Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"email":"user@example.com","password":"secret"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), `"code":"unsupported_media_type"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestCORSPreflightAllowsConfiguredMethodsAndRejectsPatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, Options{CORSOrigins: []string{"https://frontend.example"}})
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/rom", nil)
	request.Header.Set("Origin", "https://frontend.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
		t.Fatalf("GET preflight = %d %#v", response.Code, response.Header())
	}

	request = httptest.NewRequest(http.MethodOptions, "/api/v1/rom/rom-1", nil)
	request.Header.Set("Origin", "https://frontend.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("PATCH preflight status = %d, want 403", response.Code)
	}
}

func TestRouterRegistersEveryMountedRustRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, Options{})
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	want := []string{
		"GET /health",
		"POST /api/v1/session", "DELETE /api/v1/session", "POST /api/v1/session/refresh",
		"GET /api/v1/i18n/:locale",
		"POST /api/v1/upload", "GET /api/v1/upload/:upload_id",
		"GET /api/v1/rom", "GET /api/v1/rom/:rom_id", "PATCH /api/v1/rom/:rom_id", "DELETE /api/v1/rom/:rom_id",
		"GET /api/v1/rom/:rom_id/cover", "POST /api/v1/rom/:rom_id/cover", "GET /api/v1/rom/:rom_id/download",
		"GET /api/v1/library", "POST /api/v1/library", "GET /api/v1/library/:library_id",
		"POST /api/v1/library/:library_id/item", "DELETE /api/v1/library/:library_id/item/:rom_id",
		"POST /api/v1/client", "DELETE /api/v1/client/:id",
	}
	for _, route := range want {
		if !registered[route] {
			t.Errorf("missing route %q", route)
		}
	}
	if len(registered) != len(want) {
		t.Fatalf("registered %d routes, want exactly %d", len(registered), len(want))
	}
}

func TestParseByteRange(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		total     int
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{name: "closed range", header: "bytes=1-3", total: 8, wantStart: 1, wantEnd: 3, wantOK: true},
		{name: "open range", header: "bytes=4-", total: 8, wantStart: 4, wantEnd: 7, wantOK: true},
		{name: "suffix range", header: "bytes=-3", total: 8, wantStart: 5, wantEnd: 7, wantOK: true},
		{name: "suffix longer than body", header: "bytes=-30", total: 8, wantStart: 0, wantEnd: 7, wantOK: true},
		{name: "multi range rejected", header: "bytes=0-1,3-4", total: 8},
		{name: "start after end rejected", header: "bytes=4-2", total: 8},
		{name: "start beyond body rejected", header: "bytes=8-", total: 8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, ok := parseByteRange(test.header, test.total)
			if start != test.wantStart || end != test.wantEnd || ok != test.wantOK {
				t.Fatalf("parseByteRange() = %d-%d %t, want %d-%d %t", start, end, ok, test.wantStart, test.wantEnd, test.wantOK)
			}
		})
	}
}

func TestWriteDownloadRangeAndETagResponses(t *testing.T) {
	tests := []struct {
		name       string
		request    func(*http.Request)
		wantStatus int
		wantBody   string
		wantRange  string
	}{
		{
			name:       "partial range",
			request:    func(request *http.Request) { request.Header.Set("Range", "bytes=1-3") },
			wantStatus: http.StatusPartialContent, wantBody: "bcd", wantRange: "bytes 1-3/6",
		},
		{
			name:       "not modified",
			request:    func(request *http.Request) { request.Header.Set("If-None-Match", `"abc"`) },
			wantStatus: http.StatusNotModified, wantBody: "",
		},
		{
			name:       "unsatisfiable range",
			request:    func(request *http.Request) { request.Header.Set("Range", "bytes=9-") },
			wantStatus: http.StatusRequestedRangeNotSatisfiable, wantBody: "", wantRange: "bytes */6",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodGet, "/download", nil)
			test.request(request)
			context.Request = request
			writeDownload(context, rom.DownloadOutput{Filename: "game.rom", MIMEType: "application/octet-stream", Bytes: []byte("abcdef"), SHA256: "abc"})
			if recorder.Code != test.wantStatus || recorder.Body.String() != test.wantBody {
				t.Fatalf("response = %d %q, want %d %q", recorder.Code, recorder.Body.String(), test.wantStatus, test.wantBody)
			}
			if test.wantRange != "" && recorder.Header().Get("Content-Range") != test.wantRange {
				t.Fatalf("Content-Range = %q, want %q", recorder.Header().Get("Content-Range"), test.wantRange)
			}
			if recorder.Header().Get("ETag") != `"abc"` {
				t.Fatalf("ETag = %q", recorder.Header().Get("ETag"))
			}
		})
	}
}

func TestRequestBodyLimitSaturates(t *testing.T) {
	if got := requestBodyLimit(1024); got != 1024+(1<<20) {
		t.Fatalf("normal body limit = %d", got)
	}
	if got := requestBodyLimit(math.MaxInt64); got != math.MaxInt64 {
		t.Fatalf("saturated body limit = %d, want MaxInt64", got)
	}
}

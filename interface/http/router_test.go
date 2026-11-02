package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	uploadfeature "github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
)

// routerTestAuthenticator resolves every token to requester, or fails with err.
type routerTestAuthenticator struct {
	requester identity.Requester
	err       error
}

func (authenticator routerTestAuthenticator) Authenticate(context.Context, string) (identity.Requester, error) {
	return authenticator.requester, authenticator.err
}

var rejectingAuthenticator = routerTestAuthenticator{err: fault.Unauthorized()}

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
	router := NewRouter(Dependencies{Authenticator: rejectingAuthenticator}, Options{})
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
	router := NewRouter(Dependencies{Authenticator: rejectingAuthenticator}, Options{})
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

func TestCORSPreflightAllowsPatchAndRejectsUnsupportedMethod(t *testing.T) {
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
	if response.Header().Get("Access-Control-Expose-Headers") != "Accept-Ranges, Content-Disposition, Content-Range, ETag, Last-Modified, X-Request-ID" {
		t.Fatalf("exposed headers = %q", response.Header().Get("Access-Control-Expose-Headers"))
	}

	request = httptest.NewRequest(http.MethodOptions, "/api/v1/rom/rom-1", nil)
	request.Header.Set("Origin", "https://frontend.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("PATCH preflight status = %d, headers = %#v, want 204 with allow methods", response.Code, response.Header())
	}

	request = httptest.NewRequest(http.MethodOptions, "/api/v1/rom/rom-1", nil)
	request.Header.Set("Origin", "https://frontend.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("PUT preflight status = %d, want 403", response.Code)
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
		"GET /health", "GET /api/v1/setup",
		"POST /api/v1/session", "DELETE /api/v1/session", "POST /api/v1/session/refresh",
		"GET /api/v1/i18n/:locale",
		"POST /api/v1/upload", "GET /api/v1/upload/:upload_id",
		"GET /api/v1/rom", "GET /api/v1/rom/:rom_id", "PATCH /api/v1/rom/:rom_id", "DELETE /api/v1/rom/:rom_id",
		"GET /api/v1/rom/:rom_id/cover", "POST /api/v1/rom/:rom_id/cover", "GET /api/v1/rom/:rom_id/download",
		"POST /api/v1/rom/:rom_id/scrape", "GET /api/v1/rom/:rom_id/media/:kind",
		"GET /api/v1/library", "POST /api/v1/library", "GET /api/v1/library/:library_id",
		"POST /api/v1/library/:library_id/item", "DELETE /api/v1/library/:library_id/item/:rom_id",
		"GET /api/v1/client", "POST /api/v1/client", "DELETE /api/v1/client/:id",
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

type nopSeekCloser struct{ *strings.Reader }

func (nopSeekCloser) Close() error { return nil }

func TestWriteDownloadStreamsRangesAndConditionalResponses(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		value      string
		wantStatus int
		wantBody   string
		wantRange  string
		wantServed bool
	}{
		{name: "full body", wantStatus: http.StatusOK, wantBody: "abcdef", wantServed: true},
		{name: "partial range", header: "Range", value: "bytes=1-3", wantStatus: http.StatusPartialContent, wantBody: "bcd", wantRange: "bytes 1-3/6", wantServed: true},
		{name: "suffix range", header: "Range", value: "bytes=-2", wantStatus: http.StatusPartialContent, wantBody: "ef", wantRange: "bytes 4-5/6", wantServed: true},
		{name: "not modified", header: "If-None-Match", value: `"abc"`, wantStatus: http.StatusNotModified},
		{name: "unsatisfiable range", header: "Range", value: "bytes=9-", wantStatus: http.StatusRequestedRangeNotSatisfiable, wantRange: "bytes */6"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
			if test.header != "" {
				c.Request.Header.Set(test.header, test.value)
			}
			writeDownload(c, rom.DownloadOutput{
				Filename: "game.rom", MIMEType: "application/octet-stream", SHA256: "abc",
				Content: rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader("abcdef")}, Size: 6},
			})
			c.Writer.WriteHeaderNow() // gin flushes body-less statuses when the handler returns
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if test.wantBody != "" && recorder.Body.String() != test.wantBody {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), test.wantBody)
			}
			if got := recorder.Header().Get("Content-Range"); got != test.wantRange {
				t.Fatalf("Content-Range = %q, want %q", got, test.wantRange)
			}
			// net/http strips validators from 416 error responses.
			if test.wantStatus != http.StatusRequestedRangeNotSatisfiable && recorder.Header().Get("ETag") != `"abc"` {
				t.Fatalf("ETag = %q", recorder.Header().Get("ETag"))
			}
			if served := bodyWasServed(c); served != test.wantServed {
				t.Fatalf("bodyWasServed = %t, want %t", served, test.wantServed)
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

func TestRouterAuthenticatesBearerTokenForProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{
		Sessions:      routerTestSessions{},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}},
	}, Options{})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout with valid token = %d %s, want 204", response.Code, response.Body.String())
	}
}

func TestRouterReportsAuthenticatorFailuresAsServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{Authenticator: routerTestAuthenticator{err: fault.Internal(errors.New("database offline"))}}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/rom", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("authenticator failure = %d %s, want 500 rather than a misleading 401", response.Code, response.Body.String())
	}
}

// routerTestUploads records the streamed upload it receives.
type routerTestUploads struct {
	received *string
}

func (uploads routerTestUploads) Create(_ context.Context, _ identity.Requester, input uploadfeature.UploadInput) (uploadfeature.UploadAcceptedDTO, error) {
	content, err := io.ReadAll(input.Content)
	if err != nil {
		return uploadfeature.UploadAcceptedDTO{}, err
	}
	*uploads.received = input.Filename + ":" + string(content)
	return uploadfeature.UploadAcceptedDTO{UploadID: "upl_1", Status: "processing"}, nil
}

func (routerTestUploads) Status(context.Context, identity.Requester, string) (uploadfeature.UploadStatusDTO, error) {
	return uploadfeature.UploadStatusDTO{}, nil
}

func multipartUpload(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("note", "ignored"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func TestRouterStreamsMultipartUploads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticated := routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}}
	send := func(maxBytes int64, field, content string) (*httptest.ResponseRecorder, string) {
		var received string
		router := NewRouter(Dependencies{Uploads: routerTestUploads{received: &received}, Authenticator: authenticated}, Options{MaxUploadBytes: maxBytes})
		body, contentType := multipartUpload(t, field, "game.sfc", content)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response, received
	}

	if response, received := send(1024, "file", "rom bytes"); response.Code != http.StatusAccepted || received != "game.sfc:rom bytes" {
		t.Fatalf("upload = %d %s, service received %q", response.Code, response.Body.String(), received)
	}
	if response, _ := send(1024, "attachment", "rom bytes"); response.Code != http.StatusBadRequest {
		t.Fatalf("upload without file field = %d, want 400", response.Code)
	}
	// The request limit is the file limit plus multipart overhead, so exceed both.
	oversized := strings.Repeat("x", 16+multipartOverhead)
	if response, _ := send(16, "file", oversized); !strings.Contains(response.Body.String(), `"code":"upload_too_large"`) {
		t.Fatalf("oversized upload = %d %s, want upload_too_large", response.Code, response.Body.String())
	}
}

func TestServeImageCachesVersionedURLsAndRevalidatesOthers(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("p", 16))
	digest := sha256.Sum256(png)
	contentHash := hex.EncodeToString(digest[:])
	serve := func(target, ifNoneMatch string) *httptest.ResponseRecorder {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		if ifNoneMatch != "" {
			c.Request.Header.Set("If-None-Match", ifNoneMatch)
		}
		serveImage(c, png, nil)
		c.Writer.WriteHeaderNow()
		return recorder
	}

	versioned := serve("/cover?v="+contentHash[:16], "")
	if versioned.Code != http.StatusOK || versioned.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("versioned = %d %q", versioned.Code, versioned.Header().Get("Cache-Control"))
	}
	if stale := serve("/cover?v=0000000000000000", ""); stale.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("mismatched version Cache-Control = %q, want revalidation", stale.Header().Get("Cache-Control"))
	}
	if unchanged := serve("/cover", `"`+contentHash+`"`); unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("revalidation = %d with %d body bytes, want empty 304", unchanged.Code, unchanged.Body.Len())
	}
}

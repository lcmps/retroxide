package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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
		"POST /api/v1/upload/session", "GET /api/v1/upload/session/:session_id", "PATCH /api/v1/upload/session/:session_id", "DELETE /api/v1/upload/session/:session_id", "POST /api/v1/upload/session/:session_id/check", "POST /api/v1/upload/disc", "GET /api/v1/upload/options", "POST /api/v1/rom/:rom_id/compress",
		"GET /api/v1/platforms", "GET /api/v1/platforms/catalog",
		"GET /api/v1/rom", "GET /api/v1/rom/:rom_id", "PATCH /api/v1/rom/:rom_id", "DELETE /api/v1/rom/:rom_id",
		"GET /api/v1/rom/:rom_id/cover", "GET /api/v1/rom/:rom_id/cover/thumb", "POST /api/v1/rom/:rom_id/cover", "GET /api/v1/rom/:rom_id/download",
		"POST /api/v1/rom/:rom_id/download-link", "GET /api/v1/download",
		"POST /api/v1/rom/:rom_id/merge", "PUT /api/v1/rom/:rom_id/discs/order", "POST /api/v1/rom/:rom_id/discs/:number/split", "DELETE /api/v1/rom/:rom_id/discs/:number",
		"POST /api/v1/rom/:rom_id/scrape", "GET /api/v1/rom/:rom_id/media/:kind",
		"GET /api/v1/library", "POST /api/v1/library", "GET /api/v1/library/:library_id",
		"POST /api/v1/library/:library_id/item", "DELETE /api/v1/library/:library_id/item/:rom_id",
		"GET /api/v1/client", "POST /api/v1/client", "DELETE /api/v1/client/:id",
		"GET /api/v1/users", "POST /api/v1/users", "PATCH /api/v1/users/:user_id", "DELETE /api/v1/users/:user_id", "DELETE /api/v1/users/:user_id/sessions",
		"GET /api/v1/me", "PATCH /api/v1/me", "GET /api/v1/settings", "PATCH /api/v1/settings", "GET /api/v1/appearance",
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

func (routerTestUploads) CreateSession(context.Context, identity.Requester, uploadfeature.CreateSessionInput) (uploadfeature.SessionDTO, error) {
	return uploadfeature.SessionDTO{SessionID: "ups_1"}, nil
}

func (routerTestUploads) SessionStatus(context.Context, identity.Requester, string) (uploadfeature.SessionDTO, error) {
	return uploadfeature.SessionDTO{SessionID: "ups_1"}, nil
}

func (uploads routerTestUploads) AppendToSession(_ context.Context, _ identity.Requester, sessionID string, offset int64, content io.Reader) (uploadfeature.SessionDTO, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return uploadfeature.SessionDTO{}, err
	}
	*uploads.received = fmt.Sprintf("%s@%d:%s", sessionID, offset, data)
	return uploadfeature.SessionDTO{SessionID: sessionID, Received: offset + int64(len(data))}, nil
}

func (routerTestUploads) CancelSession(context.Context, identity.Requester, string) error { return nil }

func (routerTestUploads) Options() uploadfeature.UploadOptionsDTO {
	return uploadfeature.UploadOptionsDTO{}
}

func (routerTestUploads) AssembleDisc(context.Context, identity.Requester, uploadfeature.AssembleDiscInput) (uploadfeature.UploadAcceptedDTO, error) {
	return uploadfeature.UploadAcceptedDTO{UploadID: "upl_disc", Status: "processing"}, nil
}

func (routerTestUploads) CheckSessionDuplicate(context.Context, identity.Requester, string, string) error {
	return nil
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

	// A body cut off mid-file (the client disconnected) is rejected, not stored truncated.
	var received string
	router := NewRouter(Dependencies{Uploads: routerTestUploads{received: &received}, Authenticator: authenticated}, Options{MaxUploadBytes: 1024})
	body, contentType := multipartUpload(t, "file", "game.sfc", strings.Repeat("r", 512))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload", bytes.NewReader(body.Bytes()[:body.Len()/2]))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || received != "" {
		t.Fatalf("cut-off upload = %d %s, service received %q; want 400", response.Code, response.Body.String(), received)
	}
}

func TestRouterTakesSessionUploadPartsWithTheirOffset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received string
	router := NewRouter(Dependencies{
		Uploads:       routerTestUploads{received: &received},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}},
	}, Options{MaxUploadBytes: 1024})
	send := func(offset string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/upload/session/ups_1", strings.NewReader("part"))
		request.Header.Set("Authorization", "Bearer token")
		request.Header.Set("Content-Type", "application/offset+octet-stream")
		if offset != "" {
			request.Header.Set("Upload-Offset", offset)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := send("8"); response.Code != http.StatusOK || received != "ups_1@8:part" || !strings.Contains(response.Body.String(), `"received":12`) {
		t.Fatalf("part = %d %s, service received %q", response.Code, response.Body.String(), received)
	}
	for _, offset := range []string{"", "-1", "eight"} {
		if response := send(offset); response.Code != http.StatusBadRequest {
			t.Fatalf("Upload-Offset %q = %d, want 400", offset, response.Code)
		}
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
		serveImage(c, rom.Image{StoredContent: memoryContent(png), Version: contentHash}, nil)
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
	if versioned.Body.String() != string(png) || versioned.Header().Get("Content-Type") != "image/png" || versioned.Header().Get("ETag") != `"`+contentHash+`"` {
		t.Fatalf("versioned body %q type %q etag %q", versioned.Body.String(), versioned.Header().Get("Content-Type"), versioned.Header().Get("ETag"))
	}

	// Content that is not an image is treated as missing.
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/cover", nil)
	serveImage(c, rom.Image{StoredContent: memoryContent([]byte("plain text"))}, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("non-image = %d, want 404", recorder.Code)
	}
}

// memoryContent is stored content held in memory, for tests.
func memoryContent(content []byte) rom.StoredContent {
	return rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader(string(content))}, Size: int64(len(content)), ModifiedAt: time.Unix(1, 0)}
}

// linkTestROMs implements only what the download-link flow uses; other rom.API methods panic.
type linkTestROMs struct {
	rom.API
	owner string
}

func (roms linkTestROMs) Get(_ context.Context, requester identity.Requester, romID string) (rom.ROMDetailDTO, error) {
	if !requester.CanAccess(roms.owner) {
		return rom.ROMDetailDTO{}, fault.NotFound()
	}
	return rom.ROMDetailDTO{ROMID: romID}, nil
}

func (roms linkTestROMs) DownloadFilename(ctx context.Context, requester identity.Requester, romID string, _ rom.DownloadFormat, disc int) (string, error) {
	if _, err := roms.Get(ctx, requester, romID); err != nil {
		return "", err
	}
	if disc != 0 {
		return fmt.Sprintf("Title of %s (Disc %d).nes", romID, disc), nil
	}
	return "Title of " + romID + ".nes", nil
}

func (roms linkTestROMs) DownloadAs(ctx context.Context, requester identity.Requester, romID string, _ rom.DownloadFormat, disc int) (rom.DownloadOutput, error) {
	output, err := roms.Download(ctx, requester, romID)
	if disc != 0 {
		output.Filename = fmt.Sprintf("Title of %s (Disc %d).nes", romID, disc)
	}
	return output, err
}

func (roms linkTestROMs) Download(ctx context.Context, requester identity.Requester, romID string) (rom.DownloadOutput, error) {
	if _, err := roms.Get(ctx, requester, romID); err != nil {
		return rom.DownloadOutput{}, err
	}
	return rom.DownloadOutput{
		Filename: "Title of " + romID + ".nes", MIMEType: "application/octet-stream", SHA256: "abc",
		Content: rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader("rom bytes")}, Size: 9},
	}, nil
}

// linkTestSigner encodes the requester and ROM into the token without cryptography.
type linkTestSigner struct{}

func (linkTestSigner) Sign(requester identity.Requester, romID string) (string, time.Time, error) {
	return requester.UserID + ":" + romID, time.Now().Add(time.Minute), nil
}

func (linkTestSigner) Verify(token string) (identity.Requester, string, error) {
	userID, romID, ok := strings.Cut(token, ":")
	if !ok {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	return identity.Requester{UserID: userID, Role: identity.RoleUser}, romID, nil
}

func TestRouterDownloadLinksStreamWithoutBearerTokenAndCheckAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	asUser := func(userID string) Dependencies {
		return Dependencies{
			ROMs:          linkTestROMs{owner: "user-1"},
			DownloadLinks: linkTestSigner{},
			Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: userID, Role: identity.RoleUser}},
		}
	}
	createLink := func(userID string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/rom/rom-1/download-link", nil)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		NewRouter(asUser(userID), Options{}).ServeHTTP(response, request)
		return response
	}

	if response := createLink("user-2"); response.Code != http.StatusNotFound {
		t.Fatalf("link for inaccessible ROM = %d, want 404", response.Code)
	}
	response := createLink("user-1")
	var link downloadLinkResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &link) != nil || link.ExpiresIn <= 0 || link.Filename != "Title of rom-1.nes" {
		t.Fatalf("create link = %d %s", response.Code, response.Body.String())
	}

	// The link works without any Authorization header.
	download := httptest.NewRecorder()
	NewRouter(Dependencies{ROMs: linkTestROMs{owner: "user-1"}, DownloadLinks: linkTestSigner{}}, Options{}).
		ServeHTTP(download, httptest.NewRequest(http.MethodGet, link.URL, nil))
	if download.Code != http.StatusOK || download.Body.String() != "rom bytes" {
		t.Fatalf("download via link = %d %q", download.Code, download.Body.String())
	}
	if got := download.Header().Get("Content-Disposition"); got != `attachment; filename="Title of rom-1.nes"` {
		t.Fatalf("Content-Disposition = %q", download.Header().Get("Content-Disposition"))
	}

	invalid := httptest.NewRecorder()
	NewRouter(Dependencies{ROMs: linkTestROMs{owner: "user-1"}, DownloadLinks: linkTestSigner{}}, Options{}).
		ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/download?token=garbage", nil))
	if invalid.Code != http.StatusForbidden || !strings.Contains(invalid.Body.String(), "invalid_download_link") {
		t.Fatalf("invalid link = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestDownloadLinkCarriesTheDisc(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dependencies := Dependencies{
		ROMs:          linkTestROMs{owner: "user-1"},
		DownloadLinks: linkTestSigner{},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}},
	}
	router := NewRouter(dependencies, Options{})
	post := func(query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/rom/rom-1/download-link?"+query, nil)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	response := post("disc=2&format=chd")
	var link downloadLinkResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &link) != nil {
		t.Fatalf("create link = %d %s", response.Code, response.Body.String())
	}
	parsed, err := url.Parse(link.URL)
	if err != nil || parsed.Query().Get("disc") != "2" || parsed.Query().Get("format") != "chd" || link.Filename != "Title of rom-1 (Disc 2).nes" {
		t.Fatalf("link = %+v, %v", link, err)
	}
	download := httptest.NewRecorder()
	router.ServeHTTP(download, httptest.NewRequest(http.MethodGet, link.URL, nil))
	if got := download.Header().Get("Content-Disposition"); download.Code != http.StatusOK || got != `attachment; filename="Title of rom-1 (Disc 2).nes"` {
		t.Fatalf("download via link = %d, Content-Disposition %q", download.Code, got)
	}

	for _, bad := range []string{"disc=abc", "disc=0", "disc=-1"} {
		if response := post(bad); response.Code != http.StatusBadRequest {
			t.Errorf("POST download-link?%s = %d %s, want a validation error", bad, response.Code, response.Body.String())
		}
	}
	bad := httptest.NewRecorder()
	router.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/v1/download?token=user-1:rom-1&disc=abc", nil))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("download?disc=abc = %d, want 400", bad.Code)
	}
}

func TestAttachmentDispositionEncodesUnicodeAndStripsPaths(t *testing.T) {
	tests := map[string]string{
		"Super Mario Bros. 3.nes": `attachment; filename="Super Mario Bros. 3.nes"`,
		`..\..\evil.nes`:          `attachment; filename=.._.._evil.nes`,
		"ポケットモンスター 赤.gb":          "attachment; filename*=utf-8''%E3%83%9D%E3%82%B1%E3%83%83%E3%83%88%E3%83%A2%E3%83%B3%E3%82%B9%E3%82%BF%E3%83%BC%20%E8%B5%A4.gb",
	}
	for filename, want := range tests {
		if got := attachmentDisposition(filename); got != want {
			t.Errorf("attachmentDisposition(%q) = %q, want %q", filename, got, want)
		}
	}
}

// localeTestProfiles records the profile update that reached the service.
type localeTestProfiles struct{ updated *auth.UpdateProfileRequest }

func (localeTestProfiles) Profile(context.Context, identity.Requester) (auth.ProfileResponse, error) {
	return auth.ProfileResponse{}, nil
}

func (profiles localeTestProfiles) UpdateProfile(_ context.Context, _ identity.Requester, request auth.UpdateProfileRequest) (auth.UpdateProfileResponse, error) {
	*profiles.updated = request
	return auth.UpdateProfileResponse{Profile: auth.ProfileResponse{Locale: request.Locale}}, nil
}

func TestRouterAcceptsOnlyLocalesWithACatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var updated auth.UpdateProfileRequest
	router := NewRouter(Dependencies{
		Profiles:      localeTestProfiles{updated: &updated},
		Locales:       map[string][]byte{"en": []byte("{}"), "pt-BR": []byte("{}")},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "usr-1", Role: identity.RoleUser}},
	}, Options{})
	patch := func(body string) int {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/me", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	if code := patch(`{"locale":"fr"}`); code != http.StatusBadRequest {
		t.Fatalf("unsupported locale = %d, want 400", code)
	}
	if code := patch(`{"locale":"pt-BR"}`); code != http.StatusOK || updated.Locale == nil || *updated.Locale != "pt-BR" {
		t.Fatalf("supported locale = %d, reached service with %+v", code, updated)
	}
	if code := patch(`{"locale":""}`); code != http.StatusOK {
		t.Fatalf("clearing the locale = %d, want 200", code)
	}
}

// discTestROMs records the disc operation each route asked for.
type discTestROMs struct {
	rom.API
	calls *[]string
}

func (roms discTestROMs) record(call string, romID string) (rom.ROMDetailDTO, error) {
	*roms.calls = append(*roms.calls, call)
	return rom.ROMDetailDTO{ROMID: romID}, nil
}

func (roms discTestROMs) MergeROM(_ context.Context, _ identity.Requester, targetID, sourceID string) (rom.ROMDetailDTO, error) {
	return roms.record("merge "+targetID+" "+sourceID, targetID)
}

func (roms discTestROMs) SplitDisc(_ context.Context, _ identity.Requester, romID string, number int) (rom.ROMDetailDTO, error) {
	return roms.record(fmt.Sprintf("split %s %d", romID, number), "new-rom")
}

func (roms discTestROMs) ReorderDiscs(_ context.Context, _ identity.Requester, romID string, ids []string) (rom.ROMDetailDTO, error) {
	return roms.record("reorder "+romID+" "+strings.Join(ids, ","), romID)
}

func (roms discTestROMs) RemoveDisc(_ context.Context, _ identity.Requester, romID string, number int) (rom.ROMDetailDTO, error) {
	return roms.record(fmt.Sprintf("remove %s %d", romID, number), romID)
}

func TestDiscRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls []string
	router := NewRouter(Dependencies{
		ROMs:          discTestROMs{calls: &calls},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "user-1", Role: identity.RoleUser}},
	}, Options{})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, test := range []struct{ method, path, body, call, romID string }{
		{http.MethodPost, "/api/v1/rom/a/merge", `{"source_rom_id":"b"}`, "merge a b", "a"},
		{http.MethodPost, "/api/v1/rom/a/discs/2/split", ``, "split a 2", "new-rom"},
		{http.MethodPut, "/api/v1/rom/a/discs/order", `{"rom_file_ids":["f2","f1"]}`, "reorder a f2,f1", "a"},
		{http.MethodDelete, "/api/v1/rom/a/discs/2", ``, "remove a 2", "a"},
	} {
		calls = nil
		response := send(test.method, test.path, test.body)
		if response.Code != http.StatusOK || len(calls) != 1 || calls[0] != test.call || !strings.Contains(response.Body.String(), `"`+test.romID+`"`) {
			t.Errorf("%s %s = %d %s; calls %v, want %q", test.method, test.path, response.Code, response.Body.String(), calls, test.call)
		}
	}
	calls = nil
	for _, test := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/rom/a/discs/two/split"},
		{http.MethodDelete, "/api/v1/rom/a/discs/two"},
	} {
		if response := send(test.method, test.path, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation") {
			t.Errorf("%s %s = %d %s, want a validation error", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none", calls)
	}
}

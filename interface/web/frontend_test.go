package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testFrontend() *Frontend {
	return newFrontend(fstest.MapFS{
		"index.html":                      {Data: []byte("<!doctype html>app")},
		"index.html.br":                   {Data: []byte("brotli-index")},
		"_app/immutable/entry/app.js":     {Data: []byte("console.log(1)")},
		"_app/immutable/entry/app.js.gz":  {Data: []byte("gzip-app")},
		"components/main-logo.png":        {Data: []byte("\x89PNG\r\n\x1a\nlogo")},
		".gitkeep":                        {Data: nil},
		"_app/immutable/assets/style.css": {Data: []byte("body{}")},
	})
}

func serve(t *testing.T, frontend http.Handler, method, target, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	response := httptest.NewRecorder()
	frontend.ServeHTTP(response, request)
	return response
}

func TestFrontendServesAppShellForClientRoutes(t *testing.T) {
	frontend := testFrontend()
	for _, target := range []string{"/", "/library", "/rom/rom_123", "/setup?x=1"} {
		response := serve(t, frontend, http.MethodGet, target, "")
		if response.Code != http.StatusOK || response.Body.String() != "<!doctype html>app" {
			t.Fatalf("GET %s = %d %q", target, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-cache" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("GET %s headers = %v", target, response.Header())
		}
	}
}

func TestFrontendCachesFingerprintedAssetsAndServesPrecompressed(t *testing.T) {
	frontend := testFrontend()
	response := serve(t, frontend, http.MethodGet, "/_app/immutable/entry/app.js", "gzip, br")
	if response.Body.String() != "gzip-app" || response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip variant = %q encoding=%q", response.Body.String(), response.Header().Get("Content-Encoding"))
	}
	if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || response.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("asset headers = %v", response.Header())
	}

	response = serve(t, frontend, http.MethodGet, "/", "br;q=1.0, gzip")
	if response.Body.String() != "brotli-index" || response.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("brotli index = %q encoding=%q", response.Body.String(), response.Header().Get("Content-Encoding"))
	}
	response = serve(t, frontend, http.MethodGet, "/", "br;q=0")
	if response.Body.String() != "<!doctype html>app" || response.Header().Get("Content-Encoding") != "" {
		t.Fatalf("refused brotli still served compressed: %q", response.Body.String())
	}
	if response.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary = %q", response.Header().Get("Vary"))
	}
}

func TestFrontendRejectsMissingAssetsHiddenFilesAndWrites(t *testing.T) {
	frontend := testFrontend()
	for _, target := range []string{"/_app/immutable/missing.js", "/missing.png", "/.gitkeep", "/components/../.gitkeep"} {
		if response := serve(t, frontend, http.MethodGet, target, ""); response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", target, response.Code)
		}
	}
	if response := serve(t, frontend, http.MethodPost, "/library", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", response.Code)
	}
	if response := serve(t, frontend, http.MethodHead, "/components/main-logo.png", ""); response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD = %d body=%d", response.Code, response.Body.Len())
	}
}

func TestFrontendReportsMissingBuild(t *testing.T) {
	frontend := newFrontend(fstest.MapFS{".gitkeep": {}})
	if frontend.Available() {
		t.Fatal("placeholder-only build reported as available")
	}
	if response := serve(t, frontend, http.MethodGet, "/", ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET / = %d, want 503", response.Code)
	}
}

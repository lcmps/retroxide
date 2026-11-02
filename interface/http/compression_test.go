package http

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCompressionMiddlewareGzipsOnlyLargeJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(compressionMiddleware())
	large := strings.Repeat("retroxide ", 500)
	router.GET("/large", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"payload": large}) })
	router.GET("/small", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	router.GET("/binary", func(c *gin.Context) { c.Data(http.StatusOK, "application/octet-stream", []byte(large)) })
	router.GET("/empty", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	get := func(path, acceptEncoding string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if acceptEncoding != "" {
			request.Header.Set("Accept-Encoding", acceptEncoding)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	compressed := get("/large", "br, gzip")
	if compressed.Header().Get("Content-Encoding") != "gzip" || compressed.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("large JSON headers = %v", compressed.Header())
	}
	reader, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(body), large) {
		t.Fatalf("decompressed body = %d bytes, %v", len(body), err)
	}
	if compressed.Body.Len() >= len(body) {
		t.Fatalf("compressed size %d is not smaller than %d", compressed.Body.Len(), len(body))
	}

	for name, response := range map[string]*httptest.ResponseRecorder{
		"client without gzip": get("/large", ""),
		"gzip refused":        get("/large", "gzip;q=0"),
		"small JSON":          get("/small", "gzip"),
		"binary download":     get("/binary", "gzip"),
		"no content":          get("/empty", "gzip"),
	} {
		if encoding := response.Header().Get("Content-Encoding"); encoding != "" {
			t.Errorf("%s: Content-Encoding = %q, want none", name, encoding)
		}
	}
}

package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/rom"
)

// readFromRecorder is a response writer that, like net/http's, implements ReadFrom.
type readFromRecorder struct {
	*httptest.ResponseRecorder
	readFroms int
}

func (recorder *readFromRecorder) ReadFrom(source io.Reader) (int64, error) {
	recorder.readFroms++
	return io.Copy(recorder.ResponseRecorder, source)
}

func TestServeFileHandsTheFileToTheConnectionsReadFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "rom")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	serve := func(rangeHeader string, compressed bool) (*readFromRecorder, int64) {
		recorder := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
		if rangeHeader != "" {
			c.Request.Header.Set("Range", rangeHeader)
		}
		if compressed { // behind the gzip middleware, as for every browser
			c.Writer = &gzipResponseWriter{ResponseWriter: c.Writer}
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		c.Writer.Header().Set("Content-Type", "application/octet-stream")
		sent := serveFile(c, "rom", rom.StoredContent{ReadSeekCloser: file, Size: 10, ModifiedAt: time.Unix(1, 0)})
		c.Writer.WriteHeaderNow()
		return recorder, sent
	}

	for _, compressed := range []bool{false, true} {
		full, sent := serve("", compressed)
		if full.Code != http.StatusOK || full.Body.String() != "0123456789" || sent != 10 || full.readFroms != 1 {
			t.Fatalf("compressed=%v full = %d %q sent=%d readFroms=%d", compressed, full.Code, full.Body.String(), sent, full.readFroms)
		}
		// The status line goes out before the file, so a range is not sent as a 200.
		partial, sent := serve("bytes=2-4", compressed)
		if partial.Code != http.StatusPartialContent || partial.Body.String() != "234" || sent != 3 || partial.readFroms != 1 {
			t.Fatalf("compressed=%v range = %d %q sent=%d readFroms=%d", compressed, partial.Code, partial.Body.String(), sent, partial.readFroms)
		}
	}
}

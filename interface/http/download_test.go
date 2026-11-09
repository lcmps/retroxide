package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
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

type archiveROMs struct {
	rom.API
	output rom.DownloadOutput
	served int
}

func (roms *archiveROMs) DownloadAs(context.Context, identity.Requester, string, rom.DownloadFormat, int) (rom.DownloadOutput, error) {
	output := roms.output
	output.Served = func(context.Context, int64) { roms.served++ }
	return output, nil
}

// archiveServer serves roms through the production middleware chain's abort handling.
func archiveServer(t *testing.T, roms *archiveROMs) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(abortFailedDownload(), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		writeError(c, fault.Internal(errors.New("http handler panic")))
	}))
	handler := romHandler{roms: roms}
	router.GET("/download", func(c *gin.Context) {
		handler.streamDownload(c, identity.Requester{UserID: "u", Role: identity.RoleUser}, "rom")
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func TestAnArchiveThatFailsMidwayAbortsTheDownload(t *testing.T) {
	roms := &archiveROMs{output: rom.DownloadOutput{Filename: "game.zip", MIMEType: "application/zip",
		Archive: func(w io.Writer) error {
			_, _ = w.Write([]byte("partial zip bytes"))
			return errors.New("read failed")
		}}}
	// The client must see a failure, either before any byte arrives or while reading the body,
	// never a clean EOF.
	response, err := http.Get(archiveServer(t, roms).URL + "/download")
	if err == nil {
		defer response.Body.Close()
		body, readErr := io.ReadAll(response.Body)
		if readErr == nil {
			t.Fatalf("read %q with a clean EOF; want an error reading the aborted body", body)
		}
	}
	if roms.served != 0 {
		t.Fatalf("Served called %d times for a failed archive", roms.served)
	}
}

func TestAnArchiveThatSucceedsStreamsCompletelyAndIsServedOnce(t *testing.T) {
	roms := &archiveROMs{output: rom.DownloadOutput{Filename: "game.zip", MIMEType: "application/zip",
		Archive: func(w io.Writer) error {
			_, err := w.Write([]byte("whole zip"))
			return err
		}}}
	response, err := http.Get(archiveServer(t, roms).URL + "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "whole zip" {
		t.Fatalf("body = %q, %v", body, err)
	}
	if roms.served != 1 {
		t.Fatalf("Served called %d times, want 1", roms.served)
	}
}

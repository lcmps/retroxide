package http

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/rom"
)

const fallbackContentDisposition = `attachment; filename="download.rom"`

// writeDownload streams a ROM file as an attachment and returns how many body bytes it sent.
// http.ServeContent handles byte ranges and conditional requests (If-None-Match against the
// SHA-256 ETag, If-Modified-Since), reading only the bytes it sends rather than loading the file
// into memory.
func writeDownload(c *gin.Context, output rom.DownloadOutput) int64 {
	header := c.Writer.Header()
	header.Set("ETag", `"`+output.SHA256+`"`)
	header.Set("Cache-Control", "private, max-age=0, must-revalidate")
	header.Set("Content-Disposition", attachmentDisposition(output.Filename))
	header.Set("Content-Type", output.MIMEType)
	return serveFile(c, output.Filename, output.Content)
}

// serveFile serves stored content through http.ServeContent and returns the body bytes sent.
//
// The stored file itself is handed over (not the StoredContent wrapper), and the response
// writer exposes the connection's ReadFrom, so on Linux the kernel sends the file with sendfile
// instead of copying it through a 32 KiB buffer in user space. Gin's writer offers neither, so
// without this every byte would pass through the process twice.
func serveFile(c *gin.Context, name string, content rom.StoredContent) int64 {
	writer := &fileResponseWriter{ResponseWriter: c.Writer}
	http.ServeContent(writer, c.Request, name, content.ModifiedAt, content.ReadSeekCloser)
	return writer.sent
}

// fileResponseWriter counts body bytes and lets io.Copy reach the connection's ReadFrom.
type fileResponseWriter struct {
	gin.ResponseWriter
	sent int64
}

func (writer *fileResponseWriter) Write(data []byte) (int, error) {
	written, err := writer.ResponseWriter.Write(data)
	writer.sent += int64(written)
	return written, err
}

func (writer *fileResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	// Gin sends the status line lazily on the first Write; the connection would otherwise send
	// an implicit 200 before a 206 or 416.
	writer.ResponseWriter.WriteHeaderNow()
	readerFrom, direct := connectionReaderFrom(writer.ResponseWriter)
	// An encoded response must pass through the writer that encodes it.
	if !direct || writer.Header().Get("Content-Encoding") != "" {
		sent, err := io.Copy(onlyWriter{writer.ResponseWriter}, source)
		writer.sent += sent
		return sent, err
	}
	sent, err := readerFrom.ReadFrom(source)
	writer.sent += sent
	return sent, err
}

// connectionReaderFrom unwraps response writers (gin's, the gzip middleware's) down to one that
// implements ReadFrom: net/http's, which sends files with sendfile.
func connectionReaderFrom(writer http.ResponseWriter) (io.ReaderFrom, bool) {
	for {
		if readerFrom, ok := writer.(io.ReaderFrom); ok {
			return readerFrom, true
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		writer = unwrapper.Unwrap()
	}
}

// onlyWriter hides every method but Write, so io.Copy cannot recurse into ReadFrom.
type onlyWriter struct{ io.Writer }

// bodyWasServed reports whether the response delivered file content (in full or as a range),
// as opposed to a 304, a 416, or a body-less request.
func bodyWasServed(c *gin.Context) bool {
	status := c.Writer.Status()
	return c.Request.Method != http.MethodHead && (status == http.StatusOK || status == http.StatusPartialContent)
}

// attachmentDisposition builds a Content-Disposition header that cannot smuggle a path.
func attachmentDisposition(filename string) string {
	safeName := filepath.Base(strings.ReplaceAll(filename, "\\", "_"))
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": safeName}); disposition != "" {
		return disposition
	}
	return fallbackContentDisposition
}

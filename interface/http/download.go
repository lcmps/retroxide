package http

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/rom"
)

const fallbackContentDisposition = `attachment; filename="download.rom"`

// writeDownload streams a ROM file as an attachment. http.ServeContent handles byte ranges and
// conditional requests (If-None-Match against the SHA-256 ETag, If-Modified-Since), reading only
// the bytes it sends rather than loading the file into memory.
func writeDownload(c *gin.Context, output rom.DownloadOutput) {
	header := c.Writer.Header()
	header.Set("ETag", `"`+output.SHA256+`"`)
	header.Set("Cache-Control", "private, max-age=0, must-revalidate")
	header.Set("Content-Disposition", attachmentDisposition(output.Filename))
	header.Set("Content-Type", output.MIMEType)
	http.ServeContent(c.Writer, c.Request, output.Filename, output.Content.ModifiedAt, output.Content)
}

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

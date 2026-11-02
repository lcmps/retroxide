package http

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/rom"
)

const fallbackContentDisposition = `attachment; filename="download.rom"`

// writeDownload serves a ROM file as an attachment, honouring conditional requests and single byte ranges.
func writeDownload(c *gin.Context, output rom.DownloadOutput) {
	etag := `"` + output.SHA256 + `"`
	var modified time.Time
	if output.LastModifiedUnixSeconds != nil {
		modified = time.Unix(int64(*output.LastModifiedUnixSeconds), 0).UTC()
	}

	c.Header("ETag", etag)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")
	if !modified.IsZero() {
		c.Header("Last-Modified", modified.Format(http.TimeFormat))
	}
	if notModified(c, etag, modified) {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}

	c.Header("Content-Disposition", attachmentDisposition(output.Filename))
	c.Header("Content-Type", output.MIMEType)
	content := output.Bytes
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		c.Header("Content-Length", strconv.Itoa(len(content)))
		c.Data(http.StatusOK, output.MIMEType, content)
		return
	}
	start, end, ok := parseByteRange(rangeHeader, len(content))
	if !ok {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", len(content)))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
	c.Header("Content-Length", strconv.Itoa(end-start+1))
	c.Data(http.StatusPartialContent, output.MIMEType, content[start:end+1])
}

// notModified evaluates If-None-Match, then If-Modified-Since, against the file's validators.
func notModified(c *gin.Context, etag string, modified time.Time) bool {
	if c.GetHeader("If-None-Match") == etag {
		return true
	}
	if modified.IsZero() {
		return false
	}
	since, err := http.ParseTime(c.GetHeader("If-Modified-Since"))
	return err == nil && !modified.After(since)
}

// attachmentDisposition builds a Content-Disposition header that cannot smuggle a path.
func attachmentDisposition(filename string) string {
	safeName := filepath.Base(strings.ReplaceAll(filename, "\\", "_"))
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": safeName}); disposition != "" {
		return disposition
	}
	return fallbackContentDisposition
}

// parseByteRange parses a single "bytes=" range against a body of total bytes, returning inclusive offsets.
func parseByteRange(value string, total int) (int, int, bool) {
	if total <= 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false
	}
	startPart, endPart, found := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
	if !found {
		return 0, 0, false
	}
	if startPart == "" {
		suffix, err := strconv.Atoi(endPart)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		return max(total-suffix, 0), total - 1, true
	}
	start, err := strconv.Atoi(startPart)
	if err != nil || start < 0 || start >= total {
		return 0, 0, false
	}
	end := total - 1
	if endPart != "" {
		parsedEnd, err := strconv.Atoi(endPart)
		if err != nil || parsedEnd < start {
			return 0, 0, false
		}
		end = min(parsedEnd, total-1)
	}
	return start, end, true
}

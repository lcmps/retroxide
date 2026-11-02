package http

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// minCompressedBytes skips compression for small bodies, where gzip framing outweighs the savings.
const minCompressedBytes = 1024

var gzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return writer
}}

// compressionMiddleware gzips JSON responses for clients that accept it. Other content passes
// through untouched: ROM downloads and images are binary or already compressed, and the frontend
// serves pre-compressed assets with their own Content-Encoding.
func compressionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodHead || !acceptsGzip(c.Request) {
			c.Next()
			return
		}
		writer := &gzipResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		defer writer.finish()
		c.Next()
	}
}

// gzipResponseWriter decides on the first body write whether to compress, once the handler has
// set the status and Content-Type; gin renders JSON bodies in a single write.
type gzipResponseWriter struct {
	gin.ResponseWriter
	decided bool
	gzip    *gzip.Writer
}

func (writer *gzipResponseWriter) Write(data []byte) (int, error) {
	if !writer.decided {
		writer.decide(len(data))
	}
	if writer.gzip == nil {
		return writer.ResponseWriter.Write(data)
	}
	return writer.gzip.Write(data)
}

func (writer *gzipResponseWriter) WriteString(data string) (int, error) {
	return writer.Write([]byte(data))
}

func (writer *gzipResponseWriter) Flush() {
	if writer.gzip != nil {
		_ = writer.gzip.Flush()
	}
	writer.ResponseWriter.Flush()
}

func (writer *gzipResponseWriter) decide(firstWriteSize int) {
	writer.decided = true
	header := writer.Header()
	status := writer.Status()
	if firstWriteSize < minCompressedBytes ||
		header.Get("Content-Encoding") != "" ||
		status == http.StatusNoContent || status == http.StatusNotModified ||
		!strings.HasPrefix(header.Get("Content-Type"), "application/json") {
		return
	}
	header.Set("Content-Encoding", "gzip")
	header.Add("Vary", "Accept-Encoding")
	header.Del("Content-Length")
	writer.gzip = gzipWriters.Get().(*gzip.Writer)
	writer.gzip.Reset(writer.ResponseWriter)
}

func (writer *gzipResponseWriter) finish() {
	if writer.gzip == nil {
		return
	}
	_ = writer.gzip.Close()
	gzipWriters.Put(writer.gzip)
	writer.gzip = nil
}

func acceptsGzip(request *http.Request) bool {
	for part := range strings.SplitSeq(request.Header.Get("Accept-Encoding"), ",") {
		token, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(token), "gzip") {
			return strings.ReplaceAll(parameters, " ", "") != "q=0"
		}
	}
	return false
}

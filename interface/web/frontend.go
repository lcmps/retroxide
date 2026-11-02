package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist is produced by the frontend build; only a placeholder is committed.
//
//go:embed all:dist
var embedded embed.FS

const (
	immutablePrefix = "_app/immutable/"
	indexFile       = "index.html"
)

// Frontend serves the embedded single-page app, falling back to index.html for client-side routes.
type Frontend struct {
	files fs.FS
	ready bool
}

func New() *Frontend {
	files, err := fs.Sub(embedded, "dist")
	if err != nil {
		return &Frontend{}
	}
	return newFrontend(files)
}

func newFrontend(files fs.FS) *Frontend {
	return &Frontend{files: files, ready: isFile(files, indexFile)}
}

func (frontend *Frontend) Available() bool {
	return frontend.ready
}

func (frontend *Frontend) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !frontend.ready {
		http.Error(writer, "the web interface is not included in this build; build it with `make build`", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
	if name == "" {
		name = indexFile
	}
	if hasHiddenSegment(name) {
		http.NotFound(writer, request)
		return
	}
	if isFile(frontend.files, name) {
		frontend.serveFile(writer, request, name)
		return
	}
	if path.Ext(name) != "" {
		http.NotFound(writer, request)
		return
	}
	frontend.serveFile(writer, request, indexFile)
}

func (frontend *Frontend) serveFile(writer http.ResponseWriter, request *http.Request, name string) {
	header := writer.Header()
	if strings.HasPrefix(name, immutablePrefix) {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "no-cache")
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		header.Set("Content-Type", contentType)
	}
	header.Add("Vary", "Accept-Encoding")

	served := name
	for _, variant := range []struct{ suffix, encoding string }{{".br", "br"}, {".gz", "gzip"}} {
		if acceptsEncoding(request, variant.encoding) && isFile(frontend.files, name+variant.suffix) {
			served = name + variant.suffix
			header.Set("Content-Encoding", variant.encoding)
			break
		}
	}
	file, err := frontend.files.Open(served)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	content, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(writer, "unreadable asset", http.StatusInternalServerError)
		return
	}
	http.ServeContent(writer, request, name, time.Time{}, content)
}

func acceptsEncoding(request *http.Request, encoding string) bool {
	for _, part := range strings.Split(request.Header.Get("Accept-Encoding"), ",") {
		token, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(token), encoding) {
			return strings.ReplaceAll(parameters, " ", "") != "q=0"
		}
	}
	return false
}

func hasHiddenSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && !info.IsDir()
}

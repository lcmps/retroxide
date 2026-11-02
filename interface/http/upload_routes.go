package http

import (
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/uploads"
)

const (
	// defaultMaxUploadBytes caps buffered uploads when no limit is configured.
	defaultMaxUploadBytes = 100 << 20
	// multipartOverhead allows for multipart boundaries and headers around the file itself.
	multipartOverhead     = 1 << 20
	defaultUploadFilename = "upload.rom"
)

type uploadHandler struct {
	uploads  uploads.Service
	maxBytes int64
}

func registerUploadRoutes(api *gin.RouterGroup, service uploads.Service, maxBytes int64) {
	handler := uploadHandler{uploads: service, maxBytes: maxBytes}

	group := api.Group("/upload", requireService(service != nil))
	group.POST("", handler.create)
	group.GET("/:upload_id", handler.status)
}

func (handler uploadHandler) create(c *gin.Context) {
	limit := handler.maxBytes
	if limit <= 0 {
		limit = defaultMaxUploadBytes
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestBodyLimit(limit))
	part, err := uploadFilePart(c.Request)
	if err != nil {
		writeError(c, err)
		return
	}
	defer part.Close()

	requester := requesterFromContext(c)
	input := uploads.UploadInput{
		UploaderID: requester.UserID,
		Filename:   part.FileName(),
		Content:    uploadBodyReader{part},
	}
	if input.Filename == "" {
		input.Filename = defaultUploadFilename
	}
	if mimeType := part.Header.Get("Content-Type"); mimeType != "" {
		input.MIMEType = &mimeType
	}
	accepted, err := handler.uploads.Create(c.Request.Context(), requester, input)
	respondJSON(c, http.StatusAccepted, accepted, err)
}

func (handler uploadHandler) status(c *gin.Context) {
	status, err := handler.uploads.Status(c.Request.Context(), requesterFromContext(c), c.Param("upload_id"))
	respondJSON(c, http.StatusOK, status, err)
}

// uploadFilePart advances a streaming multipart reader to the "file" field without buffering
// the upload in memory or spooling it to a temporary file.
func uploadFilePart(request *http.Request) (*multipart.Part, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, fault.Validation("upload must be a multipart/form-data request")
	}
	for {
		part, err := reader.NextPart()
		switch {
		case errors.Is(err, io.EOF):
			return nil, fault.Validation(`missing or empty "file" field`)
		case isBodyTooLarge(err):
			return nil, errUploadBodyTooLarge
		case err != nil:
			return nil, fault.Validation("invalid multipart upload")
		case part.FormName() == "file":
			return part, nil
		}
		_ = part.Close()
	}
}

var errUploadBodyTooLarge = &fault.Error{Code: fault.CodeUploadTooLarge, Message: "upload size exceeds limit while reading multipart body"}

// uploadBodyReader reports the request size limit as an upload-too-large error rather than a
// generic I/O failure when it trips mid-stream.
type uploadBodyReader struct {
	reader io.Reader
}

func (body uploadBodyReader) Read(buffer []byte) (int, error) {
	n, err := body.reader.Read(buffer)
	if isBodyTooLarge(err) {
		return n, errUploadBodyTooLarge
	}
	return n, err
}

// requestBodyLimit is the whole-request limit for a multipart body carrying a file of up to fileLimit bytes.
func requestBodyLimit(fileLimit int64) int64 {
	if fileLimit > math.MaxInt64-multipartOverhead {
		return math.MaxInt64
	}
	return fileLimit + multipartOverhead
}

package http

import (
	"io"
	"math"
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
	input, err := handler.readUpload(c)
	if err != nil {
		writeError(c, err)
		return
	}
	requester := requesterFromContext(c)
	input.UploaderID = requester.UserID
	accepted, err := handler.uploads.Create(c.Request.Context(), requester, input)
	respondJSON(c, http.StatusAccepted, accepted, err)
}

func (handler uploadHandler) status(c *gin.Context) {
	status, err := handler.uploads.Status(c.Request.Context(), requesterFromContext(c), c.Param("upload_id"))
	respondJSON(c, http.StatusOK, status, err)
}

// readUpload buffers the multipart "file" field, enforcing the upload size limit.
func (handler uploadHandler) readUpload(c *gin.Context) (uploads.UploadInput, error) {
	limit := handler.maxBytes
	if limit > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestBodyLimit(limit))
	} else {
		limit = defaultMaxUploadBytes
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) {
			return uploads.UploadInput{}, &fault.Error{Code: fault.CodeUploadTooLarge, Message: "upload size exceeds limit while reading multipart body"}
		}
		return uploads.UploadInput{}, fault.Validation(`missing or empty "file" field`)
	}
	if fileHeader.Size > limit {
		return uploads.UploadInput{}, fault.UploadTooLarge(fileHeader.Size, limit)
	}
	file, err := fileHeader.Open()
	if err != nil {
		return uploads.UploadInput{}, fault.Validation("invalid multipart file")
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return uploads.UploadInput{}, fault.Validation("invalid multipart file payload")
	}
	if int64(len(content)) > limit {
		return uploads.UploadInput{}, fault.UploadTooLarge(int64(len(content)), limit)
	}

	input := uploads.UploadInput{Filename: fileHeader.Filename, Bytes: content}
	if input.Filename == "" {
		input.Filename = defaultUploadFilename
	}
	if mimeType := fileHeader.Header.Get("Content-Type"); mimeType != "" {
		input.MIMEType = &mimeType
	}
	return input, nil
}

// requestBodyLimit is the whole-request limit for a multipart body carrying a file of up to fileLimit bytes.
func requestBodyLimit(fileLimit int64) int64 {
	if fileLimit > math.MaxInt64-multipartOverhead {
		return math.MaxInt64
	}
	return fileLimit + multipartOverhead
}

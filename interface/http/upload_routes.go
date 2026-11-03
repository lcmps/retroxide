package http

import (
	"bufio"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"

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
	// Uploads sent in parts, which resume after a dropped connection.
	group.POST("/session", handler.createSession)
	group.GET("/session/:session_id", handler.sessionStatus)
	group.PATCH("/session/:session_id", handler.appendToSession)
	group.DELETE("/session/:session_id", handler.cancelSession)
	group.POST("/session/:session_id/check", handler.checkSessionDuplicate)
}

// checkSessionDuplicate takes the file's SHA-256, computed by the client while it uploads.
func (handler uploadHandler) checkSessionDuplicate(c *gin.Context) {
	var input struct {
		SHA256 string `json:"sha256"`
	}
	if !bindJSON(c, &input) {
		return
	}
	respondNoContent(c, handler.uploads.CheckSessionDuplicate(c.Request.Context(), requesterFromContext(c), c.Param("session_id"), input.SHA256))
}

func (handler uploadHandler) createSession(c *gin.Context) {
	var input uploads.CreateSessionInput
	if !bindJSON(c, &input) {
		return
	}
	session, err := handler.uploads.CreateSession(c.Request.Context(), requesterFromContext(c), input)
	respondJSON(c, http.StatusCreated, session, err)
}

func (handler uploadHandler) sessionStatus(c *gin.Context) {
	session, err := handler.uploads.SessionStatus(c.Request.Context(), requesterFromContext(c), c.Param("session_id"))
	respondJSON(c, http.StatusOK, session, err)
}

// appendToSession takes one part of a session upload: the raw bytes, with the Upload-Offset
// header naming where in the file they start.
func (handler uploadHandler) appendToSession(c *gin.Context) {
	offset, err := strconv.ParseInt(c.GetHeader("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		writeError(c, fault.Validation("the Upload-Offset header must give the part's starting byte"))
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, uploads.MaxChunkBytes)
	content := uploadBodyReader{bufio.NewReaderSize(body, uploadReadBufferSize)}
	session, err := handler.uploads.AppendToSession(c.Request.Context(), requesterFromContext(c), c.Param("session_id"), offset, content)
	respondJSON(c, http.StatusOK, session, err)
}

func (handler uploadHandler) cancelSession(c *gin.Context) {
	respondNoContent(c, handler.uploads.CancelSession(c.Request.Context(), requesterFromContext(c), c.Param("session_id")))
}

func (handler uploadHandler) create(c *gin.Context) {
	limit := handler.maxBytes
	if limit <= 0 {
		limit = defaultMaxUploadBytes
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, requestBodyLimit(limit))
	// The multipart reader reads 4 KiB at a time, one system call each; a larger buffer in
	// front of it reads the socket in up to 1 MiB pieces instead.
	c.Request.Body = bufferedBody{Reader: bufio.NewReaderSize(body, uploadReadBufferSize), Closer: body}
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
		// The request carries the file and a little multipart framing, so its length is the
		// file's size to within a few hundred bytes.
		ExpectedSize: c.Request.ContentLength,
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

// uploadReadBufferSize is how much of an upload is read from the connection at a time.
const uploadReadBufferSize = 1 << 20

// bufferedBody reads a request body through a buffer and closes the body itself.
type bufferedBody struct {
	io.Reader
	io.Closer
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
	switch {
	case isBodyTooLarge(err):
		return n, errUploadBodyTooLarge
	case errors.Is(err, io.ErrUnexpectedEOF):
		return n, errUploadInterrupted
	}
	return n, err
}

// errUploadInterrupted reports a body that ended before the upload did, as when the client
// disconnects mid-transfer.
var errUploadInterrupted = fault.Validation("upload was interrupted before it finished")

// requestBodyLimit is the whole-request limit for a multipart body carrying a file of up to fileLimit bytes.
func requestBodyLimit(fileLimit int64) int64 {
	if fileLimit > math.MaxInt64-multipartOverhead {
		return math.MaxInt64
	}
	return fileLimit + multipartOverhead
}

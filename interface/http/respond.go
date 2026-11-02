package http

import (
	"errors"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

// respondJSON writes body with status, or the error response when err is set.
func respondJSON(c *gin.Context, status int, body any, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status, body)
}

// respondNoContent writes 204, or the error response when err is set.
func respondNoContent(c *gin.Context, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeError(c *gin.Context, err error) {
	_ = c.Error(err)
	status, response := PresentError(err, requestIDFromContext(c))
	writeErrorResponse(c, status, response)
}

func writeErrorResponse(c *gin.Context, status int, response ErrorResponse) {
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", "Bearer")
	}
	c.AbortWithStatusJSON(status, response)
}

// writeSpecificError writes an error whose code is more specific than the generic fault codes.
func writeSpecificError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorResponse{
		Code: code, Message: message, RequestID: requestIDFromContext(c),
	})
}

func abortUnauthorized(c *gin.Context) {
	status, response := PresentError(fault.Unauthorized(), requestIDFromContext(c))
	writeErrorResponse(c, status, response)
}

func requestIDFromContext(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

func requesterFromContext(c *gin.Context) identity.Requester {
	value, _ := c.Get(principalKey)
	requester, _ := value.(identity.Requester)
	return requester
}

// bindJSON decodes a size-limited application/json body into target, writing the error
// response and returning false when the request is unacceptable.
func bindJSON(c *gin.Context, target any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(c, fault.UnsupportedMediaType("request content type must be application/json"))
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBodyBytes)
	if err := c.ShouldBindJSON(target); err != nil {
		if isBodyTooLarge(err) {
			writeError(c, fault.Validation("request body is too large"))
		} else {
			writeError(c, fault.Validation("invalid JSON request body"))
		}
		return false
	}
	return true
}

func isBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

// requireService fails a route group with an internal error when its backing service is missing.
func requireService(available bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !available {
			writeError(c, fault.Internal(errors.New("service is unavailable")))
			return
		}
		c.Next()
	}
}

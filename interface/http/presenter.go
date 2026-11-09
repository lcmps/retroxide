package http

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lcmps/retroxide/application/fault"
)

type ErrorResponse struct {
	Code              string  `json:"code"`
	Message           string  `json:"message"`
	RequestID         string  `json:"request_id"`
	ExistingROMFileID *string `json:"existing_rom_file_id,omitempty"`
	ExistingBIOSID    *string `json:"existing_bios_id,omitempty"`
	SHA256            *string `json:"sha256,omitempty"`
}

var requestCounter atomic.Uint64

func NewRequestID() string {
	timestampMicros := time.Now().UnixMicro()
	if timestampMicros < 0 {
		timestampMicros = 0
	}
	counter := requestCounter.Add(1)
	return "req_" + decimal(uint64(timestampMicros)) + "_" + decimal(counter)
}

func PresentError(err error, requestID string) (int, ErrorResponse) {
	var appError *fault.Error
	if !errors.As(err, &appError) {
		return http.StatusInternalServerError, ErrorResponse{
			Code: "internal_error", Message: "internal error", RequestID: requestID,
		}
	}
	message := appError.Message
	if appError.Code == fault.CodeInternal {
		message = "internal error"
	}
	return httpStatusForError(appError.Code), ErrorResponse{
		Code:              string(appError.Code),
		Message:           message,
		RequestID:         requestID,
		ExistingROMFileID: appError.ExistingROMFileID,
		ExistingBIOSID:    appError.ExistingBIOSID,
		SHA256:            appError.SHA256,
	}
}

func httpStatusForError(code fault.Code) int {
	switch code {
	case fault.CodeValidation:
		return http.StatusBadRequest
	case fault.CodeUnauthorized:
		return http.StatusUnauthorized
	case fault.CodeForbidden:
		return http.StatusForbidden
	case fault.CodeNotFound:
		return http.StatusNotFound
	case fault.CodeConflict, fault.CodeROMAlreadyExists, fault.CodeBIOSAlreadyExists, fault.CodeAlreadyInLibrary:
		return http.StatusConflict
	case fault.CodeUploadTooLarge:
		return http.StatusRequestEntityTooLarge
	case fault.CodeUnsupportedUpload, fault.CodeUnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case fault.CodeUpstreamUnavailable:
		return http.StatusBadGateway
	case fault.CodeRateLimited:
		return http.StatusTooManyRequests
	case fault.CodeInsufficientStorage:
		return http.StatusInsufficientStorage
	default:
		return http.StatusInternalServerError
	}
}

func decimal(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}

package fault

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeValidation           Code = "validation_error"
	CodeUnauthorized         Code = "unauthorized"
	CodeForbidden            Code = "forbidden"
	CodeNotFound             Code = "not_found"
	CodeConflict             Code = "conflict"
	CodeROMAlreadyExists     Code = "rom_already_exists"
	CodeBIOSAlreadyExists    Code = "bios_already_exists"
	CodeAlreadyInLibrary     Code = "already_in_library"
	CodeUploadTooLarge       Code = "upload_too_large"
	CodeUnsupportedUpload    Code = "upload_type_not_allowed"
	CodeUnsupportedMediaType Code = "unsupported_media_type"
	CodeUpstreamUnavailable  Code = "upstream_unavailable"
	CodeRateLimited          Code = "rate_limited"
	CodeInsufficientStorage  Code = "insufficient_storage"
	CodeInternal             Code = "internal_error"
)

type Error struct {
	Code              Code
	Message           string
	ExistingROMFileID *string
	ExistingBIOSID    *string
	SHA256            *string
	Cause             error
}

func (appError *Error) Error() string {
	if appError.Cause != nil {
		return fmt.Sprintf("%s: %v", appError.Code, appError.Cause)
	}
	return appError.Message
}

func (appError *Error) Unwrap() error { return appError.Cause }

func Validation(message string) *Error { return &Error{Code: CodeValidation, Message: message} }
func Unauthorized() *Error             { return &Error{Code: CodeUnauthorized, Message: "unauthorized"} }
func Forbidden() *Error                { return &Error{Code: CodeForbidden, Message: "forbidden"} }
func NotFound() *Error                 { return &Error{Code: CodeNotFound, Message: "not found"} }
func Conflict() *Error                 { return &Error{Code: CodeConflict, Message: "conflict"} }

func ROMAlreadyExists(fileID, sha256 string) *Error {
	return &Error{Code: CodeROMAlreadyExists, Message: "rom already exists", ExistingROMFileID: &fileID, SHA256: &sha256}
}

// BIOSAlreadyExists reports that a BIOS file is already on the server, as the entry biosID.
func BIOSAlreadyExists(biosID, sha256 string) *Error {
	appError := &Error{Code: CodeBIOSAlreadyExists, Message: "bios already exists", ExistingBIOSID: &biosID}
	if sha256 != "" {
		appError.SHA256 = &sha256
	}
	return appError
}

// AlreadyInLibrary reports that the requester already has a ROM a share link offers.
func AlreadyInLibrary() *Error {
	return &Error{Code: CodeAlreadyInLibrary, Message: "this rom is already in your library"}
}

func UploadTooLarge(actualBytes, maxBytes int64) *Error {
	return &Error{Code: CodeUploadTooLarge, Message: fmt.Sprintf("upload size exceeds limit: actual_bytes=%d, max_bytes=%d", actualBytes, maxBytes)}
}

func UnsupportedUploadType(reason string) *Error {
	return &Error{Code: CodeUnsupportedUpload, Message: reason}
}

func UnsupportedMediaType(message string) *Error {
	return &Error{Code: CodeUnsupportedMediaType, Message: message}
}

func Internal(cause error) *Error {
	return &Error{Code: CodeInternal, Message: "internal error", Cause: cause}
}

func UpstreamUnavailable(message string, cause error) *Error {
	return &Error{Code: CodeUpstreamUnavailable, Message: message, Cause: cause}
}

// InsufficientStorage reports that the server's disk cannot hold the content.
func InsufficientStorage() *Error {
	return &Error{Code: CodeInsufficientStorage, Message: "the server does not have enough free disk space"}
}

func RateLimited() *Error {
	return &Error{Code: CodeRateLimited, Message: "too many attempts, try again later"}
}

func IsCode(err error, code Code) bool {
	var appError *Error
	return errors.As(err, &appError) && appError.Code == code
}

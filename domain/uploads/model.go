package uploads

import "errors"

type UploadStatus string

const (
	UploadStatusPending    UploadStatus = "pending"
	UploadStatusProcessing UploadStatus = "processing"
	UploadStatusCompleted  UploadStatus = "completed"
	UploadStatusFailed     UploadStatus = "failed"
)

var ErrInvalidUploadStatusTransition = errors.New("domain: invalid upload status transition")

type Upload struct {
	ID               string
	UploaderID       string
	OriginalFilename string
	Status           UploadStatus
	ErrorMessage     *string
	ROMID            *string
}

func (upload *Upload) TransitionTo(next UploadStatus) error {
	valid := false
	switch upload.Status {
	case UploadStatusPending:
		valid = next == UploadStatusProcessing || next == UploadStatusFailed
	case UploadStatusProcessing:
		valid = next == UploadStatusCompleted || next == UploadStatusFailed
	case UploadStatusFailed:
		valid = next == UploadStatusProcessing
	}
	if !valid {
		return ErrInvalidUploadStatusTransition
	}
	upload.Status = next
	return nil
}

package uploads

import "io"

type UploadInput struct {
	UploaderID string
	Filename   string
	MIMEType   *string
	// Content streams the upload; the service reads it once while staging and hashing.
	Content io.Reader
	// ExpectedSize is the upload's announced size in bytes, or 0 when unknown. An upload that
	// cannot fit on disk is refused before any of it is written.
	ExpectedSize int64
}

type UploadAcceptedDTO struct {
	UploadID string `json:"upload_id"`
	Status   string `json:"status"`
}

type UploadStatusDTO struct {
	UploadID string  `json:"upload_id"`
	Status   string  `json:"status"`
	ROMID    *string `json:"rom_id"`
	Error    *string `json:"error"`
}

// CreateSessionInput starts an upload sent in parts.
type CreateSessionInput struct {
	Filename string  `json:"filename"`
	MIMEType *string `json:"mime_type,omitempty"`
	// Size is the whole file's size in bytes.
	Size int64 `json:"size"`
}

// SessionDTO is the progress of an upload sent in parts.
type SessionDTO struct {
	SessionID string `json:"session_id"`
	Size      int64  `json:"size"`
	// Received is how many bytes have arrived: the offset the next part starts at.
	Received int64 `json:"received"`
	// MaxChunkSize is the largest part accepted in one request.
	MaxChunkSize int64 `json:"max_chunk_size"`
	// Upload is set once every byte has arrived and the upload was accepted.
	Upload *UploadAcceptedDTO `json:"upload,omitempty"`
}

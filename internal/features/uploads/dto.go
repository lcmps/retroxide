package uploads

type UploadInput struct {
	UploaderID string
	Filename   string
	MIMEType   *string
	Bytes      []byte
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

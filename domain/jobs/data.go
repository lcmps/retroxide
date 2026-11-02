package jobs

type Payload struct {
	Kind      string
	UploadID  string
	ROMFileID string
	Attempt   uint8
}

type Job struct {
	ID      string
	Payload Payload
}

type DeadLetterJob struct {
	Payload Payload
	Reason  string
}

type DeadLetterJobRecord struct {
	ID      string
	Payload Payload
	Reason  string
}

type DeadLetterPage struct {
	Item       []DeadLetterJobRecord
	NextCursor *string
}

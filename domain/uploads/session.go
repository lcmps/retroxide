package uploads

import "time"

// Session is an upload sent in parts, which can resume after a dropped connection. Received
// bytes of Size have arrived and are hashed; HashState saves the hashing progress so the next
// part continues it, even after a restart.
type Session struct {
	ID         string
	UploaderID string
	Filename   string
	MIMEType   *string
	Size       int64
	Received   int64
	HashState  []byte
	UpdatedAt  time.Time
}

// Complete reports whether every byte has arrived.
func (session Session) Complete() bool {
	return session.Received == session.Size
}

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

const (
	// KindIdentifyROM turns a stored upload into a ROM.
	KindIdentifyROM = "identify_rom"
	// KindScrapeROM fetches metadata and artwork for the ROM an upload created.
	KindScrapeROM = "scrape_rom"
)

// KindFilter selects which job kinds a claim may return: those listed, or with Exclude, all
// others. The zero value matches every kind.
type KindFilter struct {
	Kinds   []string
	Exclude bool
}

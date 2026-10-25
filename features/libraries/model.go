package libraries

type Library struct {
	ID     string
	UserID string
	Name   string
}

type LibraryItem struct {
	LibraryID string
	ROMID     string
}

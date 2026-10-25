package libraries

type LibraryDTO struct {
	LibraryID string `json:"library_id"`
	Name      string `json:"name"`
}

type CreateLibraryRequest struct {
	Name string `json:"name"`
}

type AddLibraryItemRequest struct {
	ROMID string `json:"rom_id"`
}

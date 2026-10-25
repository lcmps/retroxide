package libraries

type LibraryDTO struct {
	LibraryID string `json:"library_id"`
	Name      string `json:"name"`
}

type LibraryDetailDTO struct {
	LibraryID string   `json:"library_id"`
	Name      string   `json:"name"`
	ROMIDs    []string `json:"rom_ids"`
}

type CreateLibraryRequest struct {
	Name string `json:"name"`
}

type AddLibraryItemRequest struct {
	ROMID string `json:"rom_id"`
}

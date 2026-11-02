package libraries

import "github.com/lcmps/retroxide/application/rom"

type LibraryDTO struct {
	LibraryID string `json:"library_id"`
	Name      string `json:"name"`
}

type LibraryDetailDTO struct {
	LibraryID string   `json:"library_id"`
	Name      string   `json:"name"`
	ROMIDs    []string `json:"rom_ids"`
	// Items summarizes each ROM so clients need not fetch them one by one.
	Items []rom.ROMSummaryDTO `json:"items"`
}

type CreateLibraryRequest struct {
	Name string `json:"name"`
}

type AddLibraryItemRequest struct {
	ROMID string `json:"rom_id"`
}

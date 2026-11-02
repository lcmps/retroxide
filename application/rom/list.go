package rom

import (
	"context"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const (
	maxROMListLimit = 100
	// maxSearchLength bounds search terms; titles are at most 255 characters.
	maxSearchLength = 100
)

type romListReader interface {
	ListROMsWithFiles(ctx context.Context, limit int, filter ROMFilter) ([]romdomain.ROMWithFile, error)
}

type ListROMService struct {
	repository romListReader
}

func NewListROMService(repository romListReader) *ListROMService {
	return &ListROMService{repository: repository}
}

// List returns the catalog visible to requester: everything for broad-access roles, only their own uploads otherwise.
func (service *ListROMService) List(ctx context.Context, requester identity.Requester, request ListRequest) (ROMListResponseDTO, error) {
	limit := request.Limit
	if limit <= 0 {
		return ROMListResponseDTO{}, fault.Validation("limit must be greater than zero")
	}
	if limit > maxROMListLimit {
		limit = maxROMListLimit
	}
	if request.Search != nil && len([]rune(*request.Search)) > maxSearchLength {
		return ROMListResponseDTO{}, fault.Validation("search must be at most 100 characters")
	}
	if request.Identified != nil && !*request.Identified {
		return ROMListResponseDTO{Item: []ROMSummaryDTO{}}, nil
	}

	filter := ROMFilter{AfterCursor: request.Cursor, Platform: request.Platform, Search: request.Search}
	if !requester.HasBroadAccess() {
		filter.UploaderID = &requester.UserID
	}
	rows, err := service.repository.ListROMsWithFiles(ctx, limit, filter)
	if err != nil {
		return ROMListResponseDTO{}, err
	}
	var nextCursor *string
	if len(rows) > limit {
		pivot := rows[limit-1].ROM.ID
		nextCursor = &pivot
		rows = rows[:limit]
	}

	items := make([]ROMSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, Summarize(row))
	}
	return ROMListResponseDTO{Item: items, NextCursor: nextCursor}, nil
}

// Summarize maps a ROM and its content hash to the summary shape used in listings.
func Summarize(row romdomain.ROMWithFile) ROMSummaryDTO {
	return ROMSummaryDTO{
		ROMID:       row.ROM.ID,
		Title:       row.ROM.Identity.Title,
		Platform:    row.ROM.Identity.Platform,
		SHA256:      row.SHA256,
		CoverArtURL: imageURL(row.ROM.ID, "cover", row.ROM.Identity.CoverArtPath),
	}
}

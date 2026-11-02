package rom

import (
	"context"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const maxROMListLimit = 100

type romListReader interface {
	ListROMsWithFiles(ctx context.Context, limit int, afterCursor, platform *string) ([]romdomain.ROMWithFile, error)
}

type ListROMService struct {
	repository romListReader
}

func NewListROMService(repository romListReader) *ListROMService {
	return &ListROMService{repository: repository}
}

func (service *ListROMService) List(ctx context.Context, limit int, cursor, platform *string, identified *bool) (ROMListResponseDTO, error) {
	if limit <= 0 {
		return ROMListResponseDTO{}, fault.Validation("limit must be greater than zero")
	}
	if limit > maxROMListLimit {
		limit = maxROMListLimit
	}
	if identified != nil && !*identified {
		return ROMListResponseDTO{Item: []ROMSummaryDTO{}}, nil
	}

	rows, err := service.repository.ListROMsWithFiles(ctx, limit, cursor, platform)
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
		var coverArtURL *string
		if row.ROM.Identity.CoverArtPath != nil {
			url := "/api/v1/rom/" + row.ROM.ID + "/cover"
			coverArtURL = &url
		}
		items = append(items, ROMSummaryDTO{
			ROMID:       row.ROM.ID,
			Title:       row.ROM.Identity.Title,
			Platform:    row.ROM.Identity.Platform,
			SHA256:      row.SHA256,
			CoverArtURL: coverArtURL,
		})
	}
	return ROMListResponseDTO{Item: items, NextCursor: nextCursor}, nil
}

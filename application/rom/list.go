package rom

import (
	"context"
	"slices"
	"strings"

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

	sort, err := parseSort(request.Sort, request.Order)
	if err != nil {
		return ROMListResponseDTO{}, err
	}
	filter := ROMFilter{Sort: sort, Platform: request.Platform, Search: request.Search}
	if request.Cursor != nil {
		if filter.After, err = decodeCursor(*request.Cursor, sort); err != nil {
			return ROMListResponseDTO{}, err
		}
	}
	if !requester.HasBroadAccess() {
		filter.UploaderID = &requester.UserID
	}
	rows, err := service.repository.ListROMsWithFiles(ctx, limit, filter)
	if err != nil {
		return ROMListResponseDTO{}, err
	}
	var nextCursor *string
	if len(rows) > limit {
		pivot := encodeCursor(rows[limit-1].ROM, sort)
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
		ROMID:         row.ROM.ID,
		Title:         row.ROM.Identity.Title,
		Platform:      row.ROM.Identity.Platform,
		PlatformName:  romdomain.PlatformName(row.ROM.Identity.Platform),
		SHA256:        row.SHA256,
		CoverArtURL:   imageURL(row.ROM.ID, "cover", row.ROM.Identity.CoverArtPath),
		CoverThumbURL: coverThumbURL(row.ROM),
		DiscCount:     row.DiscCount,
	}
}

// Platforms lists the platforms of the ROMs the requester can see, and those with BIOS files
// (shared by everyone), ordered by display name.
func (service *Service) Platforms(ctx context.Context, requester identity.Requester) ([]PlatformDTO, error) {
	var uploaderID *string
	if !requester.HasBroadAccess() {
		uploaderID = &requester.UserID
	}
	counts, err := service.repository.ListPlatforms(ctx, uploaderID)
	if err != nil {
		return nil, err
	}
	platforms := make([]PlatformDTO, 0, len(counts))
	for _, count := range counts {
		platforms = append(platforms, PlatformDTO{Key: count.Platform, Name: romdomain.PlatformName(count.Platform), ROMCount: count.Count, BIOSCount: count.BIOSCount})
	}
	slices.SortFunc(platforms, func(a, b PlatformDTO) int {
		// Unknown sorts last; the rest alphabetically.
		if aUnknown, bUnknown := romdomain.IsUnknownPlatform(a.Key), romdomain.IsUnknownPlatform(b.Key); aUnknown != bUnknown {
			if aUnknown {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return platforms, nil
}

// PlatformCatalog lists every known platform by display name, with no ROM counts.
func (*Service) PlatformCatalog() []PlatformDTO {
	known := romdomain.Platforms()
	catalog := make([]PlatformDTO, 0, len(known))
	for _, platform := range known {
		catalog = append(catalog, PlatformDTO{Key: platform.Key, Name: platform.Name})
	}
	slices.SortFunc(catalog, func(a, b PlatformDTO) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return catalog
}

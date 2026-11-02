package libraries

import (
	"context"
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
)

type Service struct {
	libraries Repository
	roms      ROMReader
}

func NewService(libraries Repository, roms ROMReader) *Service {
	return &Service{libraries: libraries, roms: roms}
}

func (service *Service) List(ctx context.Context, requester identity.Requester) ([]LibraryDTO, error) {
	libraries, err := service.libraries.ListLibrary(ctx, ownerFilter(requester))
	if err != nil {
		return nil, err
	}
	result := make([]LibraryDTO, 0, len(libraries))
	for _, library := range libraries {
		result = append(result, LibraryDTO{LibraryID: library.ID, Name: library.Name})
	}
	return result, nil
}

func (service *Service) Create(ctx context.Context, requester identity.Requester, name string) (LibraryDTO, error) {
	if err := ValidateName(name); err != nil {
		return LibraryDTO{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return LibraryDTO{}, fault.Validation("library name cannot be empty")
	}
	libraryID, err := id.New("lib")
	if err != nil {
		return LibraryDTO{}, fault.Internal(err)
	}
	library := librariesdomain.Library{ID: libraryID, UserID: requester.UserID, Name: name}
	if err := service.libraries.SaveLibrary(ctx, library); err != nil {
		return LibraryDTO{}, err
	}
	return LibraryDTO{LibraryID: library.ID, Name: library.Name}, nil
}

func (service *Service) Get(ctx context.Context, requester identity.Requester, libraryID string) (LibraryDetailDTO, error) {
	library, err := service.libraries.FindLibraryByID(ctx, libraryID)
	if err != nil {
		return LibraryDetailDTO{}, err
	}
	if library == nil || !requester.CanAccess(library.UserID) {
		return LibraryDetailDTO{}, fault.NotFound()
	}
	roms, err := service.libraries.ListLibraryROMs(ctx, library.ID)
	if err != nil {
		return LibraryDetailDTO{}, err
	}
	detail := LibraryDetailDTO{LibraryID: library.ID, Name: library.Name, ROMIDs: []string{}, Items: []rom.ROMSummaryDTO{}}
	for _, row := range roms {
		// Items added before access checks existed may reference ROMs the requester cannot see.
		if !requester.CanAccess(row.ROM.UploaderID) {
			continue
		}
		detail.ROMIDs = append(detail.ROMIDs, row.ROM.ID)
		detail.Items = append(detail.Items, rom.Summarize(row))
	}
	return detail, nil
}

func (service *Service) AddItem(ctx context.Context, requester identity.Requester, libraryID, romID string) error {
	library, err := service.libraries.FindLibraryByID(ctx, libraryID)
	if err != nil {
		return err
	}
	if library == nil || !requester.CanAccess(library.UserID) {
		return fault.NotFound()
	}
	found, err := service.roms.FindROMByID(ctx, romID)
	if err != nil {
		return err
	}
	if found == nil || !requester.CanAccess(found.UploaderID) {
		return fault.NotFound()
	}
	return service.libraries.AddLibraryItem(ctx, libraryID, romID)
}

func (service *Service) RemoveItem(ctx context.Context, requester identity.Requester, libraryID, romID string) error {
	library, err := service.libraries.FindLibraryByID(ctx, libraryID)
	if err != nil {
		return err
	}
	if library == nil || !requester.CanAccess(library.UserID) {
		return fault.NotFound()
	}
	removed, err := service.libraries.RemoveLibraryItem(ctx, libraryID, romID)
	if err != nil {
		return err
	}
	if !removed {
		return fault.NotFound()
	}
	return nil
}

const MaxNameLength = 255

func ValidateName(name string) error {
	if name == "" {
		return fault.Validation("library name cannot be empty")
	}
	if len(name) > MaxNameLength {
		return fault.Validation(fmt.Sprintf("library name exceeds %d characters", MaxNameLength))
	}
	return nil
}

// ownerFilter limits listings to the requester's own libraries unless they have broad access.
func ownerFilter(requester identity.Requester) *string {
	if requester.HasBroadAccess() {
		return nil
	}
	return &requester.UserID
}

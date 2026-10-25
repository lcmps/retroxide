package libraries

import (
	"context"
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/internal/platform/id"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type Service struct {
	libraries Repository
	roms      ROMReader
}

func NewService(libraries Repository, roms ROMReader) *Service {
	return &Service{libraries: libraries, roms: roms}
}

func (service *Service) List(ctx context.Context, requester identity.Requester) ([]LibraryDTO, error) {
	libraries, err := service.libraries.ListLibrary(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]LibraryDTO, 0, len(libraries))
	for _, library := range libraries {
		if !requester.CanAccess(library.UserID) {
			continue
		}
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
	library := Library{ID: libraryID, UserID: requester.UserID, Name: name}
	if err := service.libraries.SaveLibrary(ctx, library); err != nil {
		return LibraryDTO{}, err
	}
	return LibraryDTO{LibraryID: library.ID, Name: library.Name}, nil
}

func (service *Service) Get(ctx context.Context, requester identity.Requester, libraryID string) (LibraryDTO, error) {
	library, err := service.libraries.FindLibraryByID(ctx, libraryID)
	if err != nil {
		return LibraryDTO{}, err
	}
	if library == nil || !requester.CanAccess(library.UserID) {
		return LibraryDTO{}, fault.NotFound()
	}
	return LibraryDTO{LibraryID: library.ID, Name: library.Name}, nil
}

func (service *Service) AddItem(ctx context.Context, requester identity.Requester, libraryID, romID string) error {
	library, err := service.libraries.FindLibraryByID(ctx, libraryID)
	if err != nil {
		return err
	}
	if library == nil || !requester.CanAccess(library.UserID) {
		return fault.NotFound()
	}
	exists, err := service.roms.ROMExists(ctx, romID)
	if err != nil {
		return err
	}
	if !exists {
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

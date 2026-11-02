package libraries

import (
	"context"

	"github.com/lcmps/retroxide/domain/identity"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Repository interface {
	SaveLibrary(context.Context, librariesdomain.Library) error
	// ListLibrary returns libraries ordered by ID; a non-nil ownerID restricts them to that user.
	ListLibrary(ctx context.Context, ownerID *string) ([]librariesdomain.Library, error)
	FindLibraryByID(context.Context, string) (*librariesdomain.Library, error)
	// ListLibraryROMs returns the library's ROMs with their content hashes, ordered by ROM ID.
	ListLibraryROMs(ctx context.Context, libraryID string) ([]romdomain.ROMWithFile, error)
	AddLibraryItem(context.Context, string, string) error
	RemoveLibraryItem(context.Context, string, string) (bool, error)
}

type ROMReader interface {
	FindROMByID(context.Context, string) (*romdomain.ROM, error)
}

type API interface {
	List(context.Context, identity.Requester) ([]LibraryDTO, error)
	Create(context.Context, identity.Requester, string) (LibraryDTO, error)
	Get(context.Context, identity.Requester, string) (LibraryDetailDTO, error)
	AddItem(context.Context, identity.Requester, string, string) error
	RemoveItem(context.Context, identity.Requester, string, string) error
}

package libraries

import (
	"context"

	"github.com/lcmps/retroxide/internal/shared/identity"
)

type Repository interface {
	SaveLibrary(context.Context, Library) error
	ListLibrary(context.Context) ([]Library, error)
	FindLibraryByID(context.Context, string) (*Library, error)
	ListLibraryROMIDs(context.Context, string) ([]string, error)
	AddLibraryItem(context.Context, string, string) error
	RemoveLibraryItem(context.Context, string, string) (bool, error)
}

type ROMReader interface {
	ROMExists(context.Context, string) (bool, error)
}

type API interface {
	List(context.Context, identity.Requester) ([]LibraryDTO, error)
	Create(context.Context, identity.Requester, string) (LibraryDTO, error)
	Get(context.Context, identity.Requester, string) (LibraryDetailDTO, error)
	AddItem(context.Context, identity.Requester, string, string) error
	RemoveItem(context.Context, identity.Requester, string, string) error
}

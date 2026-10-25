package libraries

import (
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
)

type testLibraryRepository struct {
	libraries []Library
	items     map[string]bool
}

func (repository *testLibraryRepository) SaveLibrary(_ context.Context, library Library) error {
	repository.libraries = append(repository.libraries, library)
	return nil
}

func (repository *testLibraryRepository) ListLibrary(context.Context) ([]Library, error) {
	return repository.libraries, nil
}

func (repository *testLibraryRepository) FindLibraryByID(_ context.Context, libraryID string) (*Library, error) {
	for _, library := range repository.libraries {
		if library.ID == libraryID {
			return &library, nil
		}
	}
	return nil, nil
}

func (repository *testLibraryRepository) AddLibraryItem(_ context.Context, libraryID, romID string) error {
	repository.items[libraryID+":"+romID] = true
	return nil
}

func (repository *testLibraryRepository) RemoveLibraryItem(_ context.Context, libraryID, romID string) (bool, error) {
	key := libraryID + ":" + romID
	if !repository.items[key] {
		return false, nil
	}
	delete(repository.items, key)
	return true, nil
}

type testLibraryROMRepository struct {
	roms map[string]bool
}

func (repository testLibraryROMRepository) ROMExists(_ context.Context, romID string) (bool, error) {
	return repository.roms[romID], nil
}

func TestLibraryServiceFiltersByOwnerAndAllowsBroadAccess(t *testing.T) {
	repository := &testLibraryRepository{libraries: []Library{
		{ID: "lib-1", UserID: "user-1", Name: "One"},
		{ID: "lib-2", UserID: "user-2", Name: "Two"},
	}, items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{roms: map[string]bool{}})

	userResult, err := service.List(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if len(userResult) != 1 || userResult[0].LibraryID != "lib-1" {
		t.Fatalf("user libraries = %+v, want only lib-1", userResult)
	}

	adminResult, err := service.List(context.Background(), identity.Requester{UserID: "admin", Role: identity.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(adminResult) != 2 {
		t.Fatalf("admin libraries = %+v, want both libraries", adminResult)
	}
}

func TestLibraryServiceConcealsUnauthorizedLibrary(t *testing.T) {
	repository := &testLibraryRepository{libraries: []Library{{ID: "lib-1", UserID: "user-1", Name: "One"}}, items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{roms: map[string]bool{}})
	_, err := service.Get(context.Background(), identity.Requester{UserID: "user-2", Role: identity.RoleUser}, "lib-1")
	if !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Get() error = %v, want not found", err)
	}
}

func TestLibraryServiceRejectsNameOverMaximumLength(t *testing.T) {
	repository := &testLibraryRepository{items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{roms: map[string]bool{}})
	_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, strings.Repeat("x", MaxNameLength+1))
	if !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("Create() error = %v, want validation error", err)
	}
	if len(repository.libraries) != 0 {
		t.Fatalf("created libraries = %d, want none", len(repository.libraries))
	}
}

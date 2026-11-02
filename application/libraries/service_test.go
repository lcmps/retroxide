package libraries

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type testLibraryRepository struct {
	libraries []librariesdomain.Library
	items     map[string]bool
	roms      map[string]romdomain.ROM
}

func (repository *testLibraryRepository) SaveLibrary(_ context.Context, library librariesdomain.Library) error {
	repository.libraries = append(repository.libraries, library)
	return nil
}

func (repository *testLibraryRepository) ListLibrary(_ context.Context, ownerID *string) ([]librariesdomain.Library, error) {
	var result []librariesdomain.Library
	for _, library := range repository.libraries {
		if ownerID == nil || library.UserID == *ownerID {
			result = append(result, library)
		}
	}
	return result, nil
}

func (repository *testLibraryRepository) FindLibraryByID(_ context.Context, libraryID string) (*librariesdomain.Library, error) {
	for _, library := range repository.libraries {
		if library.ID == libraryID {
			return &library, nil
		}
	}
	return nil, nil
}

func (repository *testLibraryRepository) ListLibraryROMs(_ context.Context, libraryID string) ([]romdomain.ROMWithFile, error) {
	var rows []romdomain.ROMWithFile
	for key := range repository.items {
		prefix := libraryID + ":"
		if romID, ok := strings.CutPrefix(key, prefix); ok {
			rows = append(rows, romdomain.ROMWithFile{ROM: repository.roms[romID], SHA256: "sha-" + romID})
		}
	}
	slices.SortFunc(rows, func(a, b romdomain.ROMWithFile) int { return strings.Compare(a.ROM.ID, b.ROM.ID) })
	return rows, nil
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
	roms map[string]romdomain.ROM
}

func (repository testLibraryROMRepository) FindROMByID(_ context.Context, romID string) (*romdomain.ROM, error) {
	rom, ok := repository.roms[romID]
	if !ok {
		return nil, nil
	}
	return &rom, nil
}

func ownedROM(id, uploaderID string) romdomain.ROM {
	return romdomain.ROM{ID: id, UploaderID: uploaderID, Identity: romdomain.ROMIdentity{Title: "Title " + id, Platform: "snes"}}
}

func TestLibraryServiceFiltersByOwnerAndAllowsBroadAccess(t *testing.T) {
	repository := &testLibraryRepository{libraries: []librariesdomain.Library{
		{ID: "lib-1", UserID: "user-1", Name: "One"},
		{ID: "lib-2", UserID: "user-2", Name: "Two"},
	}, items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{})

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
	repository := &testLibraryRepository{libraries: []librariesdomain.Library{{ID: "lib-1", UserID: "user-1", Name: "One"}}, items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{})
	_, err := service.Get(context.Background(), identity.Requester{UserID: "user-2", Role: identity.RoleUser}, "lib-1")
	if !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Get() error = %v, want not found", err)
	}
}

func TestLibraryServiceGetReturnsSortedROMMembership(t *testing.T) {
	repository := &testLibraryRepository{
		libraries: []librariesdomain.Library{{ID: "lib-1", UserID: "user-1", Name: "Collection"}},
		items:     map[string]bool{"lib-1:rom-z": true, "lib-1:rom-a": true},
		roms:      map[string]romdomain.ROM{"rom-a": ownedROM("rom-a", "user-1"), "rom-z": ownedROM("rom-z", "user-1")},
	}
	service := NewService(repository, testLibraryROMRepository{})

	detail, err := service.Get(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, "lib-1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(detail.ROMIDs, []string{"rom-a", "rom-z"}) {
		t.Fatalf("ROMIDs = %v, want [rom-a rom-z]", detail.ROMIDs)
	}
}

func TestLibraryServiceRejectsNameOverMaximumLength(t *testing.T) {
	repository := &testLibraryRepository{items: make(map[string]bool)}
	service := NewService(repository, testLibraryROMRepository{})
	_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, strings.Repeat("x", MaxNameLength+1))
	if !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("Create() error = %v, want validation error", err)
	}
	if len(repository.libraries) != 0 {
		t.Fatalf("created libraries = %d, want none", len(repository.libraries))
	}
}

func TestLibraryServiceGetEmbedsSummariesOfAccessibleROMsOnly(t *testing.T) {
	repository := &testLibraryRepository{
		libraries: []librariesdomain.Library{{ID: "lib-1", UserID: "user-1", Name: "Collection"}},
		items:     map[string]bool{"lib-1:rom-mine": true, "lib-1:rom-theirs": true},
		roms:      map[string]romdomain.ROM{"rom-mine": ownedROM("rom-mine", "user-1"), "rom-theirs": ownedROM("rom-theirs", "user-2")},
	}
	service := NewService(repository, testLibraryROMRepository{})

	detail, err := service.Get(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, "lib-1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(detail.ROMIDs, []string{"rom-mine"}) || len(detail.Items) != 1 {
		t.Fatalf("detail = %+v, want only the requester's ROM", detail)
	}
	if item := detail.Items[0]; item.Title != "Title rom-mine" || item.SHA256 != "sha-rom-mine" {
		t.Fatalf("summary = %+v", item)
	}
}

func TestLibraryServiceAddItemRequiresAccessToTheROM(t *testing.T) {
	roms := map[string]romdomain.ROM{"rom-mine": ownedROM("rom-mine", "user-1"), "rom-theirs": ownedROM("rom-theirs", "user-2")}
	repository := &testLibraryRepository{
		libraries: []librariesdomain.Library{{ID: "lib-1", UserID: "user-1", Name: "Collection"}},
		items:     map[string]bool{},
		roms:      roms,
	}
	service := NewService(repository, testLibraryROMRepository{roms: roms})
	requester := identity.Requester{UserID: "user-1", Role: identity.RoleUser}

	if err := service.AddItem(context.Background(), requester, "lib-1", "rom-theirs"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("AddItem(other user's ROM) error = %v, want not found", err)
	}
	if err := service.AddItem(context.Background(), requester, "lib-1", "rom-mine"); err != nil {
		t.Fatal(err)
	}
	if len(repository.items) != 1 || !repository.items["lib-1:rom-mine"] {
		t.Fatalf("library items = %v, want only rom-mine", repository.items)
	}
}

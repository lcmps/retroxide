package rom

import (
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

var adminRequester = identity.Requester{UserID: "admin-1", Role: identity.RoleAdmin}

type listROMRepository struct {
	rows       []romdomain.ROMWithFile
	limit      int
	uploaderID *string
	filter     ROMFilter
	callCount  int
}

func (repository *listROMRepository) ListROMsWithFiles(_ context.Context, limit int, filter ROMFilter) ([]romdomain.ROMWithFile, error) {
	repository.limit = limit
	repository.uploaderID = filter.UploaderID
	repository.filter = filter
	repository.callCount++
	return repository.rows, nil
}

func TestListROMServiceReturnsCursorAtLastIncludedROM(t *testing.T) {
	repository := &listROMRepository{rows: []romdomain.ROMWithFile{
		{ROM: romdomain.ROM{ID: "rom-1", Identity: romdomain.ROMIdentity{Title: "One", Platform: "snes"}}, SHA256: "sha-1"},
		{ROM: romdomain.ROM{ID: "rom-2", Identity: romdomain.ROMIdentity{Title: "Two", Platform: "snes", CoverArtPath: stringPointer("/cover")}}, SHA256: "sha-2"},
		{ROM: romdomain.ROM{ID: "rom-3", Identity: romdomain.ROMIdentity{Title: "Three", Platform: "nes"}}, SHA256: "sha-3"},
	}}
	service := NewListROMService(repository)

	result, err := service.List(context.Background(), adminRequester, ListRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Item) != 2 || result.NextCursor == nil || *result.NextCursor != "rom-2" {
		t.Fatalf("page = %+v, want two items and cursor rom-2", result)
	}
	if result.Item[1].CoverArtURL == nil || *result.Item[1].CoverArtURL != "/api/v1/rom/rom-2/cover" { // "/cover" is not content-addressed, so unversioned
		t.Fatalf("cover_art_url = %v, want cover endpoint", result.Item[1].CoverArtURL)
	}
}

func TestListROMServiceClampsAndSkipsUnidentifiedFalse(t *testing.T) {
	repository := &listROMRepository{}
	service := NewListROMService(repository)
	identified := false

	result, err := service.List(context.Background(), adminRequester, ListRequest{Limit: 500, Identified: &identified})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Item) != 0 || result.NextCursor != nil || repository.callCount != 0 {
		t.Fatalf("result = %+v calls=%d, want empty result without repository call", result, repository.callCount)
	}

	identified = true
	_, err = service.List(context.Background(), adminRequester, ListRequest{Limit: 500, Identified: &identified})
	if err != nil {
		t.Fatal(err)
	}
	if repository.limit != 100 {
		t.Fatalf("repository limit = %d, want 100", repository.limit)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestListROMServiceScopesRegularUsersToTheirOwnUploads(t *testing.T) {
	repository := &listROMRepository{}
	service := NewListROMService(repository)

	if _, err := service.List(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, ListRequest{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if repository.uploaderID == nil || *repository.uploaderID != "user-1" {
		t.Fatalf("uploader filter = %v, want user-1", repository.uploaderID)
	}

	for _, role := range []identity.RequesterRole{identity.RoleAdmin, identity.RoleClient} {
		if _, err := service.List(context.Background(), identity.Requester{UserID: "user-1", Role: role}, ListRequest{Limit: 10}); err != nil {
			t.Fatal(err)
		}
		if repository.uploaderID != nil {
			t.Fatalf("role %d uploader filter = %v, want none", role, *repository.uploaderID)
		}
	}
}

func TestListROMServicePassesSearchAndRejectsOverlongTerms(t *testing.T) {
	repository := &listROMRepository{}
	service := NewListROMService(repository)
	search := "mario"
	if _, err := service.List(context.Background(), adminRequester, ListRequest{Limit: 10, Search: &search}); err != nil {
		t.Fatal(err)
	}
	if repository.filter.Search == nil || *repository.filter.Search != "mario" {
		t.Fatalf("search filter = %v, want mario", repository.filter.Search)
	}
	overlong := strings.Repeat("x", maxSearchLength+1)
	if _, err := service.List(context.Background(), adminRequester, ListRequest{Limit: 10, Search: &overlong}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("overlong search error = %v, want validation", err)
	}
}

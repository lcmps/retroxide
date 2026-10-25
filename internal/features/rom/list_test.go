package rom

import (
	"context"
	"testing"
)

type listROMRepository struct {
	rows      []ROMWithFile
	limit     int
	callCount int
}

func (repository *listROMRepository) ListROMsWithFiles(_ context.Context, limit int, _, _ *string) ([]ROMWithFile, error) {
	repository.limit = limit
	repository.callCount++
	return repository.rows, nil
}

func TestListROMServiceReturnsCursorAtLastIncludedROM(t *testing.T) {
	repository := &listROMRepository{rows: []ROMWithFile{
		{ROM: ROM{ID: "rom-1", Identity: ROMIdentity{Title: "One", Platform: "snes"}}, SHA256: "sha-1"},
		{ROM: ROM{ID: "rom-2", Identity: ROMIdentity{Title: "Two", Platform: "snes", CoverArtPath: stringPointer("/cover")}}, SHA256: "sha-2"},
		{ROM: ROM{ID: "rom-3", Identity: ROMIdentity{Title: "Three", Platform: "nes"}}, SHA256: "sha-3"},
	}}
	service := NewListROMService(repository)

	result, err := service.List(context.Background(), 2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Item) != 2 || result.NextCursor == nil || *result.NextCursor != "rom-2" {
		t.Fatalf("page = %+v, want two items and cursor rom-2", result)
	}
	if result.Item[1].CoverArtURL == nil || *result.Item[1].CoverArtURL != "/api/v1/rom/rom-2/cover" {
		t.Fatalf("cover_art_url = %v, want cover endpoint", result.Item[1].CoverArtURL)
	}
}

func TestListROMServiceClampsAndSkipsUnidentifiedFalse(t *testing.T) {
	repository := &listROMRepository{}
	service := NewListROMService(repository)
	identified := false

	result, err := service.List(context.Background(), 500, nil, nil, &identified)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Item) != 0 || result.NextCursor != nil || repository.callCount != 0 {
		t.Fatalf("result = %+v calls=%d, want empty result without repository call", result, repository.callCount)
	}

	identified = true
	_, err = service.List(context.Background(), 500, nil, nil, &identified)
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

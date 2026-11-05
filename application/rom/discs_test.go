package rom

import (
	"context"
	"errors"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

func discTestService() (*Service, *romTestRepository) {
	region := func(value string) *string { return &value }
	repository := &romTestRepository{
		roms: map[string]romdomain.ROM{
			"a":     {ID: "a", ROMFileID: "file-a", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "A", Platform: "psx", Region: region("USA")}},
			"b":     {ID: "b", ROMFileID: "file-b", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "B", Platform: "PSX", Region: region("usa")}},
			"japan": {ID: "japan", ROMFileID: "file-j", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "J", Platform: "psx", Region: region("Japan")}},
			"free":  {ID: "free", ROMFileID: "file-f", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "F", Platform: "psx"}},
			"ps2":   {ID: "ps2", ROMFileID: "file-p", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "P", Platform: "ps2"}},
			"other": {ID: "other", ROMFileID: "file-o", UploaderID: "someone", Identity: romdomain.ROMIdentity{Title: "O", Platform: "psx"}},
		},
		romFiles: map[string]romdomain.ROMFile{},
		discs: map[string][]romdomain.ROMDisc{
			"a": {{ROMID: "a", ROMFileID: "file-a", Number: 1}, {ROMID: "a", ROMFileID: "file-a2", Number: 2}},
		},
	}
	for _, id := range []string{"file-a", "file-a2", "file-b", "file-j", "file-f", "file-p", "file-o"} {
		repository.romFiles[id] = romdomain.ROMFile{ID: id, StoragePath: "/stored/" + id}
	}
	return NewService(repository, &romTestStorage{files: map[string][]byte{}}, romTestAudit{}, nil), repository
}

func assertErrorCode(t *testing.T, err error, code fault.Code) {
	t.Helper()
	var appError *fault.Error
	if !errors.As(err, &appError) || appError.Code != code {
		t.Fatalf("error = %v, want code %v", err, code)
	}
}

func TestMergeRefusesDifferentRegions(t *testing.T) {
	service, repository := discTestService()
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	_, err := service.MergeROM(context.Background(), owner, "a", "japan")
	assertErrorCode(t, err, fault.CodeValidation)
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v", repository.calls)
	}
	// Case differs only, or either unset: allowed.
	for _, source := range []string{"b", "free"} {
		service, repository := discTestService()
		if _, err := service.MergeROM(context.Background(), owner, "a", source); err != nil {
			t.Errorf("merging %s: %v", source, err)
		}
		if len(repository.calls) != 1 {
			t.Errorf("merging %s: calls = %v", source, repository.calls)
		}
	}
	service, _ = discTestService()
	if _, err := service.MergeROM(context.Background(), owner, "free", "japan"); err != nil {
		t.Errorf("unset target region: %v", err)
	}
}

func TestMergeRefusesDifferentPlatforms(t *testing.T) {
	service, repository := discTestService()
	_, err := service.MergeROM(context.Background(), identity.Requester{UserID: "owner", Role: identity.RoleUser}, "free", "ps2")
	assertErrorCode(t, err, fault.CodeValidation)
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v", repository.calls)
	}
}

func TestMergeRefusesAROMWithItself(t *testing.T) {
	service, repository := discTestService()
	_, err := service.MergeROM(context.Background(), identity.Requester{UserID: "owner", Role: identity.RoleUser}, "a", "a")
	assertErrorCode(t, err, fault.CodeValidation)
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v", repository.calls)
	}
}

func TestMergeRefusesDifferentOwners(t *testing.T) {
	service, repository := discTestService()
	_, err := service.MergeROM(context.Background(), identity.Requester{UserID: "admin", Role: identity.RoleAdmin}, "a", "other")
	assertErrorCode(t, err, fault.CodeValidation)
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v", repository.calls)
	}
}

func TestMergeCallsTheRepository(t *testing.T) {
	service, repository := discTestService()
	detail, err := service.MergeROM(context.Background(), identity.Requester{UserID: "owner", Role: identity.RoleUser}, "a", "b")
	if err != nil || detail.ROMID != "a" || len(repository.calls) != 1 || repository.calls[0] != "merge a b" {
		t.Fatalf("MergeROM() = %+v, %v; calls %v", detail, err, repository.calls)
	}
}

func TestDiscOperationsNeedAccess(t *testing.T) {
	service, repository := discTestService()
	stranger := identity.Requester{UserID: "stranger", Role: identity.RoleUser}
	ctx := context.Background()
	_, mergeErr := service.MergeROM(ctx, stranger, "a", "b")
	_, splitErr := service.SplitDisc(ctx, stranger, "a", 2)
	_, reorderErr := service.ReorderDiscs(ctx, stranger, "a", []string{"file-a2", "file-a"})
	_, removeErr := service.RemoveDisc(ctx, stranger, "a", 2, "file-a2")
	for name, err := range map[string]error{"merge": mergeErr, "split": splitErr, "reorder": reorderErr, "remove": removeErr} {
		var appError *fault.Error
		if !errors.As(err, &appError) || appError.Code != fault.CodeNotFound {
			t.Errorf("%s error = %v, want not found", name, err)
		}
	}
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v", repository.calls)
	}
}

func TestSplitAndRemoveMapNumbersToFiles(t *testing.T) {
	service, repository := discTestService()
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	ctx := context.Background()
	split, err := service.SplitDisc(ctx, owner, "a", 2)
	if err != nil || split.ROMID == "a" || split.ROMID == "" {
		t.Fatalf("SplitDisc() = %+v, %v", split, err)
	}
	if _, err := service.RemoveDisc(ctx, owner, "a", 1, "file-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReorderDiscs(ctx, owner, "a", []string{"file-a2", "file-a"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"split a file-a2", "remove a file-a", "reorder a file-a2,file-a"}
	if len(repository.calls) != 3 || repository.calls[0] != want[0] || repository.calls[1] != want[1] || repository.calls[2] != want[2] {
		t.Errorf("calls = %v, want %v", repository.calls, want)
	}
	_, err = service.SplitDisc(ctx, owner, "a", 9)
	assertErrorCode(t, err, fault.CodeNotFound)
	_, err = service.RemoveDisc(ctx, owner, "a", 9, "file-a")
	assertErrorCode(t, err, fault.CodeNotFound)
}

func TestRemoveDiscRefusesANumberThatNowMeansAnotherFile(t *testing.T) {
	service, repository := discTestService()
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	ctx := context.Background()
	_, err := service.RemoveDisc(ctx, owner, "a", 1, "file-a2")
	assertErrorCode(t, err, fault.CodeConflict)
	_, err = service.RemoveDisc(ctx, owner, "a", 2, "")
	assertErrorCode(t, err, fault.CodeValidation)
	if len(repository.calls) != 0 {
		t.Errorf("calls = %v, want none", repository.calls)
	}
	if _, err := service.RemoveDisc(ctx, owner, "a", 2, "file-a2"); err != nil {
		t.Fatal(err)
	}
	if len(repository.calls) != 1 || repository.calls[0] != "remove a file-a2" {
		t.Errorf("calls = %v", repository.calls)
	}
}

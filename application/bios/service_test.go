package bios

import (
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type fakeRepository struct {
	entries map[string]biosdomain.BIOS
	deleted []string
}

func (repository *fakeRepository) ListBIOS(_ context.Context, platform *string) ([]biosdomain.BIOS, error) {
	var result []biosdomain.BIOS
	for _, entry := range repository.entries {
		if platform == nil || entry.Platform == *platform {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (repository *fakeRepository) FindBIOS(_ context.Context, id string) (*biosdomain.BIOS, error) {
	entry, ok := repository.entries[id]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

func (repository *fakeRepository) ListUnrecognizedBIOS(context.Context) ([]biosdomain.BIOS, error) {
	var result []biosdomain.BIOS
	for _, entry := range repository.entries {
		if !entry.Recognized {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (repository *fakeRepository) UpdateBIOS(_ context.Context, entry biosdomain.BIOS) error {
	repository.entries[entry.ID] = entry
	return nil
}

func (repository *fakeRepository) HardDeleteBIOS(_ context.Context, id string, _ func(string) error) error {
	repository.deleted = append(repository.deleted, id)
	delete(repository.entries, id)
	return nil
}

func (repository *fakeRepository) FindROMFileByID(_ context.Context, id string) (*romdomain.ROMFile, error) {
	return &romdomain.ROMFile{ID: id, StoragePath: "stored/" + id}, nil
}

type fakeKnown map[string]biosdomain.Known

func (known fakeKnown) Lookup(sha1 string, size uint64) (biosdomain.Known, bool) {
	entry, ok := known[strings.ToLower(sha1)]
	return entry, ok && entry.Size == size
}

func testHash(t *testing.T, sha1 string) romdomain.ROMHash {
	t.Helper()
	hash, err := romdomain.NewROMHash(strings.Repeat("a", 64), sha1, strings.Repeat("c", 32), "0000000d")
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

var (
	owner  = identity.Requester{UserID: "usr_owner", Role: identity.RoleUser}
	other  = identity.Requester{UserID: "usr_other", Role: identity.RoleUser}
	admin  = identity.Requester{UserID: "usr_admin", Role: identity.RoleAdmin}
	client = identity.Requester{UserID: "usr_owner", Role: identity.RoleClient}
)

func newTestService(t *testing.T, known fakeKnown) (*Service, *fakeRepository) {
	t.Helper()
	path, region := "pcsx2/bios/ps2-0160e-20010704.bin", "Europe"
	repository := &fakeRepository{entries: map[string]biosdomain.BIOS{
		"bio_known": {ID: "bio_known", ROMFileID: "rf_known", UploaderID: "usr_owner", Platform: "ps2", Filename: "mine.bin",
			ExpectedPath: &path, Recognized: true, Labels: biosdomain.Labels{Region: &region}, Hash: testHash(t, strings.Repeat("1", 40)), Size: 4},
		"bio_unknown": {ID: "bio_unknown", ROMFileID: "rf_unknown", UploaderID: "usr_owner", Platform: "ps2", Filename: "dump.bin",
			Hash: testHash(t, strings.Repeat("2", 40)), Size: 8},
	}}
	return NewService(repository, nil, known), repository
}

func TestEveryoneCanListAndGet(t *testing.T) {
	service, _ := newTestService(t, nil)
	platform := "ps2"
	for _, requester := range []identity.Requester{owner, other, admin, client} {
		listed, err := service.List(context.Background(), requester, &platform)
		if err != nil || len(listed) != 2 {
			t.Fatalf("List as %+v = %d entries, %v", requester, len(listed), err)
		}
		got, err := service.Get(context.Background(), requester, "bio_known")
		if err != nil || got.ExpectedPath != "pcsx2/bios/ps2-0160e-20010704.bin" || got.CanModify != (requester == owner || requester == admin) {
			t.Fatalf("Get as %+v = %+v, %v", requester, got, err)
		}
	}
	unknown, _ := service.Get(context.Background(), other, "bio_unknown")
	if unknown.ExpectedPath != "dump.bin" || unknown.Recognized {
		t.Fatalf("unrecognized entry reports %+v, want its uploaded filename", unknown)
	}
	if _, err := service.Get(context.Background(), owner, "bio_missing"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
}

func TestDeletePermissions(t *testing.T) {
	for _, test := range []struct {
		name      string
		requester identity.Requester
		allowed   bool
	}{{"owner", owner, true}, {"admin", admin, true}, {"other user", other, false}, {"owner's client", client, false}} {
		service, repository := newTestService(t, nil)
		err := service.Delete(context.Background(), test.requester, "bio_known")
		if test.allowed && (err != nil || len(repository.deleted) != 1) {
			t.Errorf("%s: Delete = %v, deleted %v", test.name, err, repository.deleted)
		}
		if !test.allowed && (!fault.IsCode(err, fault.CodeForbidden) || len(repository.deleted) != 0) {
			t.Errorf("%s: Delete = %v, want forbidden", test.name, err)
		}
	}
}

func TestLabel(t *testing.T) {
	service, repository := newTestService(t, nil)
	model := "SCPH-39004"
	if _, err := service.Label(context.Background(), owner, "bio_known", LabelInput{Model: &model}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("labelling a recognized entry = %v, want validation error", err)
	}
	if _, err := service.Label(context.Background(), other, "bio_unknown", LabelInput{Model: &model}); !fault.IsCode(err, fault.CodeForbidden) {
		t.Fatalf("labelling someone else's entry = %v, want forbidden", err)
	}
	tooLong := strings.Repeat("x", 65)
	if _, err := service.Label(context.Background(), owner, "bio_unknown", LabelInput{Region: &tooLong}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("a 65-character region = %v, want validation error", err)
	}
	// Dates are stored in 16 characters (MySQL rejects longer values in strict mode).
	longDate := "dumped 1995-09-09"
	if _, err := service.Label(context.Background(), owner, "bio_unknown", LabelInput{Date: &longDate}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("a 17-character date = %v, want validation error", err)
	}
	labelled, err := service.Label(context.Background(), owner, "bio_unknown", LabelInput{Model: &model})
	if err != nil || labelled.Model == nil || *labelled.Model != model || repository.entries["bio_unknown"].Labels.Model == nil {
		t.Fatalf("Label = %+v, %v", labelled, err)
	}
}

func TestRecheckUnrecognized(t *testing.T) {
	path, version := "pcsx2/bios/ps2-0170a-20030325.bin", "1.70"
	service, repository := newTestService(t, fakeKnown{strings.Repeat("2", 40): {Platform: "ps2", Path: path, Size: 8, SHA1: strings.Repeat("2", 40), Labels: biosdomain.Labels{Version: &version}}})
	recognized, err := service.RecheckUnrecognized(context.Background())
	if err != nil || recognized != 1 {
		t.Fatalf("RecheckUnrecognized = %d, %v", recognized, err)
	}
	entry := repository.entries["bio_unknown"]
	if !entry.Recognized || entry.ExpectedPath == nil || *entry.ExpectedPath != path || entry.Labels.Version == nil || *entry.Labels.Version != version {
		t.Fatalf("entry after recheck = %+v", entry)
	}
}

func TestDownloadUsesTheExpectedFilename(t *testing.T) {
	storage := &fakeStorage{}
	service, _ := newTestService(t, nil)
	service.storage = storage
	output, err := service.Download(context.Background(), other, "bio_known")
	if err != nil {
		t.Fatal(err)
	}
	if output.Filename != "ps2-0160e-20010704.bin" || output.MIMEType != "application/octet-stream" || output.SHA256 != strings.Repeat("a", 64) || storage.opened != "stored/rf_known" {
		t.Fatalf("Download = %+v, opened %q", output, storage.opened)
	}
}

type fakeStorage struct{ opened string }

func (storage *fakeStorage) Write(context.Context, string, []byte) (string, error) { return "", nil }
func (storage *fakeStorage) Read(context.Context, string) ([]byte, error)          { return nil, nil }
func (storage *fakeStorage) Delete(context.Context, string) error                  { return nil }
func (storage *fakeStorage) Open(_ context.Context, path string) (rom.StoredContent, error) {
	storage.opened = path
	return rom.StoredContent{}, nil
}

package rom

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type romTestRepository struct {
	roms     map[string]romdomain.ROM
	romFiles map[string]romdomain.ROMFile
}

func (repository *romTestRepository) UpsertROMFile(_ context.Context, romFile romdomain.ROMFile) (romdomain.ROMFile, error) {
	repository.romFiles[romFile.ID] = romFile
	return romFile, nil
}

func (repository *romTestRepository) SaveROM(_ context.Context, rom romdomain.ROM) error {
	repository.roms[rom.ID] = rom
	return nil
}

func (*romTestRepository) ListROMsWithFiles(context.Context, int, *string, *string) ([]romdomain.ROMWithFile, error) {
	return nil, nil
}

func (repository *romTestRepository) FindROMByID(_ context.Context, romID string) (*romdomain.ROM, error) {
	rom, ok := repository.roms[romID]
	if !ok {
		return nil, nil
	}
	return &rom, nil
}

func (repository *romTestRepository) FindROMFileByID(_ context.Context, romFileID string) (*romdomain.ROMFile, error) {
	romFile, ok := repository.romFiles[romFileID]
	if !ok {
		return nil, nil
	}
	return &romFile, nil
}

func (repository *romTestRepository) FindROMFileBySHA256(_ context.Context, sha256 string) (*romdomain.ROMFile, error) {
	for _, romFile := range repository.romFiles {
		if romFile.Hash.SHA256 == sha256 {
			return &romFile, nil
		}
	}
	return nil, nil
}

func (repository *romTestRepository) FindROMFileByROMID(_ context.Context, romID string) (*romdomain.ROMFile, error) {
	rom, ok := repository.roms[romID]
	if !ok {
		return nil, nil
	}
	return repository.FindROMFileByID(context.Background(), rom.ROMFileID)
}

func (repository *romTestRepository) CountROMsByROMFileID(_ context.Context, romFileID string) (uint64, error) {
	var count uint64
	for _, rom := range repository.roms {
		if rom.ROMFileID == romFileID {
			count++
		}
	}
	return count, nil
}

func (repository *romTestRepository) DeleteROM(_ context.Context, romID string) error {
	delete(repository.roms, romID)
	return nil
}

func (repository *romTestRepository) DeleteROMFile(_ context.Context, romFileID string) error {
	delete(repository.romFiles, romFileID)
	return nil
}

type romTestStorage struct {
	files map[string][]byte
}

func (storage *romTestStorage) Write(_ context.Context, key string, content []byte) (string, error) {
	path := "/storage/" + key
	storage.files[path] = append([]byte(nil), content...)
	return path, nil
}

func (storage *romTestStorage) Read(_ context.Context, path string) ([]byte, error) {
	return append([]byte(nil), storage.files[path]...), nil
}

func (storage *romTestStorage) Open(_ context.Context, path string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(storage.files[path])), nil
}

func (*romTestStorage) LastModified(context.Context, string) (*uint64, error) {
	return nil, nil
}

func (storage *romTestStorage) Delete(_ context.Context, path string) error {
	delete(storage.files, path)
	return nil
}

type romTestAudit struct{}

func (romTestAudit) UploadAccepted(context.Context, string, string, string, uint64) {}
func (romTestAudit) DuplicateUpload(context.Context, string, string)                {}
func (romTestAudit) DownloadServed(context.Context, string, string, string, uint64) {}

func TestROMServiceUpdateNormalizesRequiredMetadata(t *testing.T) {
	hash, err := romdomain.NewROMHash(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"deadbeef",
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := &romTestRepository{
		roms: map[string]romdomain.ROM{
			"rom-1": {ID: "rom-1", ROMFileID: "file-1", UploaderID: "user-1", Identity: romdomain.ROMIdentity{Title: "Old", Platform: "unknown"}},
		},
		romFiles: map[string]romdomain.ROMFile{"file-1": {ID: "file-1", Hash: hash}},
	}
	service := NewService(repository, &romTestStorage{files: make(map[string][]byte)}, romTestAudit{}, nil)
	emptyRegion := "  "
	version := "Rev A"

	result, err := service.Update(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, "rom-1", UpdateROMRequest{
		Title:    " New Title ",
		Platform: " SNES ",
		Region:   &emptyRegion,
		Version:  &version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "New Title" || result.Platform != "snes" || result.Region != nil || result.Version == nil || *result.Version != version {
		t.Fatalf("updated ROM detail = %+v", result)
	}
}

func TestROMServiceConcealsROMFromOtherUser(t *testing.T) {
	repository := &romTestRepository{
		roms:     map[string]romdomain.ROM{"rom-1": {ID: "rom-1", UploaderID: "owner"}},
		romFiles: make(map[string]romdomain.ROMFile),
	}
	service := NewService(repository, &romTestStorage{files: make(map[string][]byte)}, romTestAudit{}, nil)
	_, err := service.Get(context.Background(), identity.Requester{UserID: "other", Role: identity.RoleUser}, "rom-1")
	if !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Get() error = %v, want not found", err)
	}
}

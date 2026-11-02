package rom

import (
	"bytes"
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type romTestRepository struct {
	roms     map[string]romdomain.ROM
	romFiles map[string]romdomain.ROMFile
	// uploadedFilenames maps ROM IDs to the filename they were uploaded with.
	uploadedFilenames map[string]string
}

func (repository *romTestRepository) FindUploadedFilename(_ context.Context, romID string) (*string, error) {
	filename, ok := repository.uploadedFilenames[romID]
	if !ok {
		return nil, nil
	}
	return &filename, nil
}

func (repository *romTestRepository) UpsertROMFile(_ context.Context, romFile romdomain.ROMFile) (romdomain.ROMFile, error) {
	repository.romFiles[romFile.ID] = romFile
	return romFile, nil
}

func (repository *romTestRepository) SaveROM(_ context.Context, rom romdomain.ROM) error {
	repository.roms[rom.ID] = rom
	return nil
}

func (*romTestRepository) ListROMsWithFiles(context.Context, int, ROMFilter) ([]romdomain.ROMWithFile, error) {
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

func (repository *romTestRepository) HardDeleteROM(ctx context.Context, romID string, deletePath func(string) error) error {
	rom := repository.roms[romID]
	if err := deletePath(repository.romFiles[rom.ROMFileID].StoragePath); err != nil {
		return err
	}
	return repository.DeleteROM(ctx, romID)
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

type romTestContent struct{ *bytes.Reader }

func (romTestContent) Close() error { return nil }

func (storage *romTestStorage) Open(_ context.Context, path string) (StoredContent, error) {
	content := storage.files[path]
	return StoredContent{ReadSeekCloser: romTestContent{bytes.NewReader(content)}, Size: int64(len(content))}, nil
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

func TestROMServiceDownloadsUnderTheTitleWithTheUploadedExtension(t *testing.T) {
	repository := &romTestRepository{
		roms: map[string]romdomain.ROM{"rom-1": {
			ID: "rom-1", ROMFileID: "file-1", UploaderID: "owner",
			Identity: romdomain.ROMIdentity{Title: "Super Mario Bros. 3", Platform: "nes"},
		}},
		romFiles:          map[string]romdomain.ROMFile{"file-1": {ID: "file-1", StoragePath: "/storage/file-1"}},
		uploadedFilenames: map[string]string{"rom-1": "smb3 (USA) [!].NES"},
	}
	storage := &romTestStorage{files: map[string][]byte{"/storage/file-1": []byte("rom")}}
	service := NewService(repository, storage, romTestAudit{}, nil)
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}

	output, err := service.Download(context.Background(), owner, "rom-1")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Content.Close()
	if output.Filename != "Super Mario Bros. 3.nes" {
		t.Fatalf("Download().Filename = %q", output.Filename)
	}
	if name, err := service.DownloadFilename(context.Background(), owner, "rom-1"); err != nil || name != output.Filename {
		t.Fatalf("DownloadFilename() = %q, %v; want %q", name, err, output.Filename)
	}
	if _, err := service.DownloadFilename(context.Background(), identity.Requester{UserID: "other", Role: identity.RoleUser}, "rom-1"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("DownloadFilename() for another user error = %v, want not found", err)
	}
}

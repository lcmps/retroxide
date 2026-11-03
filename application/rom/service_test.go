package rom

import (
	"bytes"
	"context"
	"strings"
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
	// uploadROMs maps upload IDs to the ROM each created.
	uploadROMs map[string]string
}

func (repository *romTestRepository) FindROMIDByUploadID(_ context.Context, uploadID string) (*string, error) {
	romID, ok := repository.uploadROMs[uploadID]
	if !ok {
		return nil, nil
	}
	return &romID, nil
}

func (repository *romTestRepository) ListPlatforms(_ context.Context, uploaderID *string) ([]PlatformCount, error) {
	counts := map[string]uint64{}
	for _, rom := range repository.roms {
		if uploaderID == nil || rom.UploaderID == *uploaderID {
			counts[rom.Identity.Platform]++
		}
	}
	result := make([]PlatformCount, 0, len(counts))
	for platform, count := range counts {
		result = append(result, PlatformCount{Platform: platform, Count: count})
	}
	return result, nil
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

func (repository *romTestRepository) SetCoverThumbnail(_ context.Context, romID, coverPath, thumbnailPath string) error {
	rom, ok := repository.roms[romID]
	if ok && rom.Identity.CoverArtPath != nil && *rom.Identity.CoverArtPath == coverPath {
		rom.Identity.CoverThumbPath = &thumbnailPath
		repository.roms[romID] = rom
	}
	return nil
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

func TestROMServiceUpdateEditsDetailsAndMarksManualCorrections(t *testing.T) {
	publisher, description := "Old Publisher", "Old description."
	repository := &romTestRepository{
		roms: map[string]romdomain.ROM{"rom-1": {ID: "rom-1", ROMFileID: "file-1", UploaderID: "owner", Identity: romdomain.ROMIdentity{
			Title: "Alex Kidd", Platform: "unknown", Publisher: &publisher, Description: &description,
			IdentifiedBy: romdomain.IdentifiedByFilename, Confidence: 45,
		}}},
		romFiles: map[string]romdomain.ROMFile{"file-1": {ID: "file-1"}},
	}
	service := NewService(repository, &romTestStorage{files: map[string][]byte{}}, romTestAudit{}, nil)
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	text := func(value string) *string { return &value }

	// Omitted details are kept; the platform name resolves to its catalog key.
	detail, err := service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{
		Title: "Alex Kidd", Platform: "Sega Master System", ReleaseYear: text(" 1986 "), Genre: text(" Platform "),
	})
	if err != nil {
		t.Fatal(err)
	}
	saved := repository.roms["rom-1"].Identity
	if detail.Platform != "sms" || detail.PlatformName != "Master System" || *detail.ReleaseYear != "1986" || *detail.Publisher != "Old Publisher" || *detail.Genre != "Platform" {
		t.Fatalf("detail = %+v", detail)
	}
	if saved.IdentifiedBy != romdomain.IdentifiedByManual {
		t.Fatalf("a hand-corrected platform must be final, got identified by %q", saved.IdentifiedBy)
	}

	// Empty details are cleared; names outside the catalog keep their capitalization.
	detail, err = service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{
		Title: "Alex Kidd", Platform: "Sega Pico", Publisher: text(""), Genre: text(""), Description: text("  "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Platform != "Sega Pico" || detail.Publisher != nil || detail.Genre != nil || detail.Description != nil || *detail.ReleaseYear != "1986" {
		t.Fatalf("detail after clearing = %+v", detail)
	}

	long := strings.Repeat("x", MaxDescriptionLength+1)
	if _, err := service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{Title: "Alex Kidd", Platform: "sms", Description: &long}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("overlong description = %v, want validation error", err)
	}
	longGenre := strings.Repeat("x", MaxGenreLength+1)
	if _, err := service.Update(context.Background(), owner, "rom-1", UpdateROMRequest{Title: "Alex Kidd", Platform: "sms", Genre: &longGenre}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("overlong genre = %v, want validation error", err)
	}
}

func TestPlatformCatalogListsEveryKnownPlatformByName(t *testing.T) {
	catalog := (&Service{}).PlatformCatalog()
	if len(catalog) != len(romdomain.Platforms()) {
		t.Fatalf("catalog has %d platforms, want %d", len(catalog), len(romdomain.Platforms()))
	}
	for index := 1; index < len(catalog); index++ {
		if strings.ToLower(catalog[index-1].Name) > strings.ToLower(catalog[index].Name) {
			t.Fatalf("catalog not sorted by name at %q, %q", catalog[index-1].Name, catalog[index].Name)
		}
	}
}

package rom

import (
	"bytes"
	"context"
	"io"
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
	discTracks map[string][]romdomain.DiscTrack
	// discs maps ROM IDs to their discs; calls records the disc operations asked for.
	discs map[string][]romdomain.ROMDisc
	calls []string
}

func (repository *romTestRepository) FindROMDiscs(_ context.Context, romID string) ([]romdomain.ROMDisc, error) {
	return repository.discs[romID], nil
}

func (repository *romTestRepository) MergeROMs(_ context.Context, targetID, sourceID string, _ func(string) error) error {
	repository.calls = append(repository.calls, "merge "+targetID+" "+sourceID)
	delete(repository.roms, sourceID)
	return nil
}

func (repository *romTestRepository) SplitROMDisc(_ context.Context, romID, romFileID, newROMID string) error {
	repository.calls = append(repository.calls, "split "+romID+" "+romFileID)
	rom := repository.roms[romID]
	rom.ID, rom.ROMFileID = newROMID, romFileID
	repository.roms[newROMID] = rom
	return nil
}

func (repository *romTestRepository) ReorderROMDiscs(_ context.Context, romID string, romFileIDs []string) error {
	repository.calls = append(repository.calls, "reorder "+romID+" "+strings.Join(romFileIDs, ","))
	return nil
}

func (repository *romTestRepository) HardDeleteROMDisc(_ context.Context, romID, romFileID string, _ func(string) error) error {
	repository.calls = append(repository.calls, "remove "+romID+" "+romFileID)
	return nil
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

func (repository *romTestRepository) UpdateROM(_ context.Context, rom romdomain.ROM) error {
	if _, ok := repository.roms[rom.ID]; !ok {
		return fault.NotFound()
	}
	repository.roms[rom.ID] = rom
	return nil
}

func (repository *romTestRepository) SaveROM(_ context.Context, rom romdomain.ROM) error {
	repository.roms[rom.ID] = rom
	return nil
}

func (*romTestRepository) ListROMsWithFiles(context.Context, int, ROMFilter) ([]romdomain.ROMWithFile, error) {
	return nil, nil
}

func (repository *romTestRepository) FindDiscTracks(_ context.Context, romFileID string) ([]romdomain.DiscTrack, error) {
	return repository.discTracks[romFileID], nil
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
	if name, err := service.DownloadFilename(context.Background(), owner, "rom-1", ""); err != nil || name != output.Filename {
		t.Fatalf("DownloadFilename() = %q, %v; want %q", name, err, output.Filename)
	}
	if _, err := service.DownloadFilename(context.Background(), identity.Requester{UserID: "other", Role: identity.RoleUser}, "rom-1", ""); !fault.IsCode(err, fault.CodeNotFound) {
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

// fakeDiscs writes discs as fixed bytes.
type fakeDiscs struct {
	canCompress bool
	percent     int
	running     bool
	requested   []string
}

func (fakeDiscs) Archive(_ context.Context, _ romdomain.ROMFile, base string, out io.Writer) error {
	_, err := io.WriteString(out, "zip of "+base)
	return err
}

func (fakeDiscs) ISO(_ context.Context, _ romdomain.ROMFile, out io.Writer) error {
	_, err := io.WriteString(out, "iso")
	return err
}

func (discs *fakeDiscs) RequestCompression(_ context.Context, romFile romdomain.ROMFile) error {
	if romFile.Format != romdomain.FormatTracks {
		return fault.Validation("not tracks")
	}
	discs.requested = append(discs.requested, romFile.ID)
	return nil
}

func (discs *fakeDiscs) CanCompressFile(context.Context, romdomain.ROMFile) bool {
	return discs.canCompress
}

func (discs *fakeDiscs) CompressionProgress(string) (int, bool) { return discs.percent, discs.running }

func TestROMsOfferEveryFormatTheyDownloadIn(t *testing.T) {
	sheet := "/stored/sheet"
	repository := &romTestRepository{
		roms:              map[string]romdomain.ROM{"rom-1": {ID: "rom-1", ROMFileID: "file-1", UploaderID: "owner", Identity: romdomain.ROMIdentity{Title: "Other", Platform: "psx"}}},
		uploadedFilenames: map[string]string{"rom-1": "Other.cue"},
		discTracks:        map[string][]romdomain.DiscTrack{"file-1": {{Number: 1, Mode: "MODE2/2352"}, {Number: 2, Mode: "AUDIO"}}},
	}
	storage := &romTestStorage{files: map[string][]byte{"/stored/chd": []byte("MComprHD")}}
	discs := &fakeDiscs{canCompress: true}
	woken := 0
	service := NewService(repository, storage, romTestAudit{}, nil).WithDiscs(discs, func() { woken++ })
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}
	formats := func() []string {
		detail, err := service.Get(context.Background(), owner, "rom-1")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, option := range detail.Downloads {
			names = append(names, string(option.Format)+option.Extension)
		}
		return names
	}
	set := func(file romdomain.ROMFile) {
		file.ID = "file-1"
		repository.romFiles = map[string]romdomain.ROMFile{"file-1": file}
	}

	// Uploaded as CUE/BIN and kept as tracks: the zip, which compression can turn into a CHD.
	set(romdomain.ROMFile{StoragePath: sheet, SheetPath: &sheet, Format: romdomain.FormatTracks})
	if got := formats(); strings.Join(got, " ") != "tracks.zip" {
		t.Fatalf("tracks disc formats = %v", got)
	}
	detail, err := service.RequestCompression(context.Background(), owner, "rom-1")
	if err != nil || !detail.Disc.CanCompress || len(discs.requested) != 1 || woken != 1 {
		t.Fatalf("RequestCompression() = %+v, %v; requested %v, woken %d", detail.Disc, err, discs.requested, woken)
	}
	// Whether a disc offers compression is the disc service's call, such as a GDI disc's no.
	discs.canCompress = false
	if detail, err := service.RequestCompression(context.Background(), owner, "rom-1"); err != nil || detail.Disc.CanCompress {
		t.Fatalf("a disc that cannot compress: %+v, %v", detail.Disc, err)
	}
	discs.canCompress = true

	// Waiting for its turn has no percentage, which is different from having reached 0%.
	set(romdomain.ROMFile{StoragePath: sheet, SheetPath: &sheet, Format: romdomain.FormatTracks, CompressRequested: true})
	if detail, err := service.RequestCompression(context.Background(), owner, "rom-1"); err != nil || detail.Disc.CompressionPercent != nil {
		t.Fatalf("a disc not being compressed: %+v, %v", detail.Disc, err)
	}
	discs.percent, discs.running = 0, true
	if detail, err := service.RequestCompression(context.Background(), owner, "rom-1"); err != nil || detail.Disc.CompressionPercent == nil || *detail.Disc.CompressionPercent != 0 {
		t.Fatalf("a disc starting to compress: %+v, %v", detail.Disc, err)
	}
	discs.percent = 42
	if detail, err := service.RequestCompression(context.Background(), owner, "rom-1"); err != nil || detail.Disc.CompressionPercent == nil || *detail.Disc.CompressionPercent != 42 {
		t.Fatalf("a disc being compressed: %+v, %v", detail.Disc, err)
	}
	// Once the request is gone, so is any percentage.
	set(romdomain.ROMFile{StoragePath: sheet, SheetPath: &sheet, Format: romdomain.FormatTracks})
	if detail, err := service.RequestCompression(context.Background(), owner, "rom-1"); err != nil || detail.Disc.CompressionPercent != nil {
		t.Fatalf("a disc no longer requested: %+v, %v", detail.Disc, err)
	}
	discs.running = false

	// Compressed since: its upload format first, then the CHD.
	set(romdomain.ROMFile{StoragePath: "/stored/chd", SheetPath: &sheet, Format: romdomain.FormatCHD})
	if got := formats(); strings.Join(got, " ") != "tracks.zip chd.chd" {
		t.Fatalf("compressed disc formats = %v", got)
	}
	output, err := service.DownloadAs(context.Background(), owner, "rom-1", DownloadCHD)
	if err != nil || output.Filename != "Other.chd" || output.Content.ReadSeekCloser == nil {
		t.Fatalf("CHD download = %+v, %v", output, err)
	}
	output.Content.Close()
	if output, err := service.Download(context.Background(), owner, "rom-1"); err != nil || output.Filename != "Other.zip" || output.Archive == nil {
		t.Fatalf("default download = %+v, %v; want the zip it was uploaded as", output, err)
	}
	if _, err := service.DownloadAs(context.Background(), owner, "rom-1", DownloadISO); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("ISO of a mixed-mode disc = %v, want refused", err)
	}

	// Uploaded as a CHD with a single MODE1 track: CHD first, then the zip and an ISO.
	repository.uploadedFilenames["rom-1"] = "Other.chd"
	repository.discTracks["file-1"] = []romdomain.DiscTrack{{Number: 1, Mode: "MODE1/2352"}}
	set(romdomain.ROMFile{StoragePath: "/stored/chd", Format: romdomain.FormatCHD})
	if got := formats(); strings.Join(got, " ") != "chd.chd tracks.zip iso.iso" {
		t.Fatalf("single-track CHD formats = %v", got)
	}
	if name, _ := service.DownloadFilename(context.Background(), owner, "rom-1", DownloadISO); name != "Other.iso" {
		t.Fatalf("ISO link name = %q", name)
	}
	if _, err := service.RequestCompression(context.Background(), owner, "rom-1"); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("compressing a CHD = %v, want refused", err)
	}

	// Anything else downloads as it was uploaded.
	repository.uploadedFilenames["rom-1"] = "Other.sfc"
	repository.discTracks["file-1"] = nil
	set(romdomain.ROMFile{StoragePath: "/stored/chd", Format: romdomain.FormatFile})
	if got := formats(); strings.Join(got, " ") != "file.sfc" {
		t.Fatalf("cartridge formats = %v", got)
	}
}

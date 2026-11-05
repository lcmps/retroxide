package uploads

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

func TestInferIdentityFromFilename(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		wantTitle      string
		wantPlatform   string
		wantRegion     string
		wantVersion    string
		wantConfidence uint8
	}{
		{
			name:           "platform region and revision",
			filename:       "Super Mario World (USA) (Rev A).sfc",
			wantTitle:      "Super Mario World",
			wantPlatform:   "snes",
			wantRegion:     "usa",
			wantVersion:    "Rev A",
			wantConfidence: 80,
		},
		{
			name:           "unknown extension",
			filename:       "Mystery Game.rom",
			wantTitle:      "Mystery Game",
			wantPlatform:   "unknown",
			wantConfidence: 45,
		},
		{
			name:           "bracketed revision",
			filename:       "Title [Rev B].nes",
			wantTitle:      "Title",
			wantPlatform:   "nes",
			wantVersion:    "Rev B",
			wantConfidence: 80,
		},
		{
			name:           "title fallback after metadata split",
			filename:       " (USA).nes",
			wantTitle:      "(USA)",
			wantPlatform:   "nes",
			wantRegion:     "usa",
			wantConfidence: 80,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := InferIdentityFromFilename(test.filename)
			if identity.Title != test.wantTitle || identity.Platform != test.wantPlatform || identity.Confidence != test.wantConfidence {
				t.Fatalf("identity = %+v", identity)
			}
			if got := optionalValue(identity.Region); got != test.wantRegion {
				t.Fatalf("region = %q, want %q", got, test.wantRegion)
			}
			if got := optionalValue(identity.Version); got != test.wantVersion {
				t.Fatalf("version = %q, want %q", got, test.wantVersion)
			}
		})
	}
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type identifyTestUploadRepository struct {
	upload *uploadsdomain.Upload
	saved  []uploadsdomain.Upload
}

func (repository *identifyTestUploadRepository) FindUpload(context.Context, string) (*uploadsdomain.Upload, error) {
	if repository.upload == nil {
		return nil, nil
	}
	upload := *repository.upload
	return &upload, nil
}

func (repository *identifyTestUploadRepository) SaveUpload(_ context.Context, upload uploadsdomain.Upload) error {
	repository.saved = append(repository.saved, upload)
	return nil
}

type identifyTestROMRepository struct {
	romFile *romdomain.ROMFile
	// anyFile makes every file id resolve to a file.
	anyFile bool
	roms    []romdomain.ROM
	discs   []romdomain.ROMDisc
	err     error
}

func (repository *identifyTestROMRepository) FindROMFileByID(_ context.Context, id string) (*romdomain.ROMFile, error) {
	if repository.anyFile {
		return &romdomain.ROMFile{ID: id}, nil
	}
	return repository.romFile, nil
}

// SaveROM mirrors the real one: it creates or updates by id, refuses a second ROM with the same
// uploader and disc group, and gives a ROM with no discs disc 1.
func (repository *identifyTestROMRepository) SaveROM(_ context.Context, rom romdomain.ROM) error {
	if repository.err != nil {
		return repository.err
	}
	for index, existing := range repository.roms {
		if existing.ID == rom.ID {
			rom.ROMFileID, rom.UploaderID, rom.DiscGroup = existing.ROMFileID, existing.UploaderID, existing.DiscGroup
			repository.roms[index] = rom
			return nil
		}
		if rom.DiscGroup != nil && existing.DiscGroup != nil && existing.UploaderID == rom.UploaderID && *existing.DiscGroup == *rom.DiscGroup {
			return errors.New("unique constraint failed: rom.uploader_id, rom.disc_group")
		}
	}
	repository.roms = append(repository.roms, rom)
	repository.discs = append(repository.discs, romdomain.ROMDisc{ROMID: rom.ID, ROMFileID: rom.ROMFileID, Number: 1})
	return nil
}

func (repository *identifyTestROMRepository) FindROMDiscs(_ context.Context, romID string) ([]romdomain.ROMDisc, error) {
	var discs []romdomain.ROMDisc
	for _, disc := range repository.discs {
		if disc.ROMID == romID {
			discs = append(discs, disc)
		}
	}
	sort.Slice(discs, func(i, j int) bool { return discs[i].Number < discs[j].Number })
	return discs, nil
}

func (repository *identifyTestROMRepository) FindROMByDiscGroup(_ context.Context, uploaderID, group string) (*romdomain.ROM, error) {
	for _, rom := range repository.roms {
		if rom.UploaderID == uploaderID && rom.DiscGroup != nil && *rom.DiscGroup == group {
			found := rom
			return &found, nil
		}
	}
	return nil, nil
}

func (repository *identifyTestROMRepository) RecordDisc(_ context.Context, disc romdomain.ROMDisc) error {
	kept := repository.discs[:0]
	for _, existing := range repository.discs {
		if existing.ROMID != disc.ROMID || existing.ROMFileID != disc.ROMFileID {
			kept = append(kept, existing)
		}
	}
	repository.discs = append(kept, disc)
	first, _ := repository.FindROMDiscs(context.Background(), disc.ROMID)
	for index := range repository.roms {
		if repository.roms[index].ID == disc.ROMID {
			repository.roms[index].ROMFileID = first[0].ROMFileID
		}
	}
	return nil
}

func TestIdentifyServicePersistsCompletedROM(t *testing.T) {
	uploads := &identifyTestUploadRepository{upload: &uploadsdomain.Upload{
		ID:               "upl-1",
		UploaderID:       "user-1",
		OriginalFilename: "Super Mario World (USA).sfc",
		Status:           uploadsdomain.UploadStatusProcessing,
	}}
	roms := &identifyTestROMRepository{romFile: &romdomain.ROMFile{ID: "rf-1"}}
	service := NewIdentifyService(uploads, roms)

	if err := service.Identify(context.Background(), "upl-1", "rf-1"); err != nil {
		t.Fatal(err)
	}
	if len(roms.roms) != 1 || len(uploads.saved) != 1 {
		t.Fatalf("saved roms=%d uploads=%d, want one each", len(roms.roms), len(uploads.saved))
	}
	if uploads.saved[0].Status != uploadsdomain.UploadStatusCompleted || uploads.saved[0].ROMID == nil || *uploads.saved[0].ROMID != roms.roms[0].ID {
		t.Fatalf("completed upload = %+v", uploads.saved[0])
	}
	if roms.roms[0].Identity.Platform != "snes" || roms.roms[0].Identity.Region == nil || *roms.roms[0].Identity.Region != "usa" {
		t.Fatalf("identified metadata = %+v", roms.roms[0].Identity)
	}
}

func TestIdentifyServiceMarksUploadFailedWhenROMFileIsMissing(t *testing.T) {
	uploads := &identifyTestUploadRepository{upload: &uploadsdomain.Upload{
		ID:               "upl-1",
		UploaderID:       "user-1",
		OriginalFilename: "game.sfc",
		Status:           uploadsdomain.UploadStatusProcessing,
	}}
	service := NewIdentifyService(uploads, &identifyTestROMRepository{})

	err := service.Identify(context.Background(), "upl-1", "rf-missing")
	if !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Identify() error = %v, want not found", err)
	}
	if len(uploads.saved) != 1 || uploads.saved[0].Status != uploadsdomain.UploadStatusFailed || uploads.saved[0].ErrorMessage == nil {
		t.Fatalf("failed upload state = %+v", uploads.saved)
	}
}

// discUpload identifies one uploaded file and returns the ROM it ended up in.
func discUpload(t *testing.T, roms *identifyTestROMRepository, uploader, filename, fileID string) romdomain.ROM {
	t.Helper()
	uploads := &identifyTestUploadRepository{upload: &uploadsdomain.Upload{
		ID:               "upl-" + fileID,
		UploaderID:       uploader,
		OriginalFilename: filename,
		Status:           uploadsdomain.UploadStatusProcessing,
	}}
	if err := NewIdentifyService(uploads, roms).Identify(context.Background(), "upl-"+fileID, fileID); err != nil {
		t.Fatal(err)
	}
	saved := uploads.saved[len(uploads.saved)-1]
	if saved.Status != uploadsdomain.UploadStatusCompleted || saved.ROMID == nil {
		t.Fatalf("upload after %q = %+v", filename, saved)
	}
	for _, rom := range roms.roms {
		if rom.ID == *saved.ROMID {
			return rom
		}
	}
	t.Fatalf("upload points at unknown ROM %s", *saved.ROMID)
	return romdomain.ROM{}
}

func discNumbers(t *testing.T, roms *identifyTestROMRepository, romID string) map[int]string {
	t.Helper()
	discs, _ := roms.FindROMDiscs(context.Background(), romID)
	numbers := map[int]string{}
	for _, disc := range discs {
		numbers[disc.Number] = disc.Filename
	}
	return numbers
}

func TestDiscsOfOneReleaseBecomeOneTitle(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	var ids []string
	names := []string{"Final Fantasy VII (USA) (Disc 1).cue", "Final Fantasy VII (USA) (Disc 2).cue", "Final Fantasy VII (USA) (Disc 3).cue"}
	for index, name := range names {
		ids = append(ids, discUpload(t, roms, "user-1", name, "rf-"+string(rune('1'+index))).ID)
	}
	if len(roms.roms) != 1 || ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("roms = %d, upload rom ids = %v; want one title", len(roms.roms), ids)
	}
	if roms.roms[0].Identity.Title != "Final Fantasy VII" || roms.roms[0].DiscGroup == nil {
		t.Fatalf("rom = %+v", roms.roms[0])
	}
	numbers := discNumbers(t, roms, ids[0])
	if len(numbers) != 3 || numbers[1] != names[0] || numbers[2] != names[1] || numbers[3] != names[2] {
		t.Fatalf("discs = %v", numbers)
	}
}

func TestDifferentReleasesStaySeparate(t *testing.T) {
	pairs := [][2]string{
		{"Game (USA) (Disc 1).cue", "Game (Japan) (Disc 1).cue"},
		{"Game (Brazil) (Disc 1).cue", "Game (Germany) (Disc 1).cue"},
		{"Game (USA) (Disc 1).cue", "Game (USA) (Rev 1) (Disc 1).cue"},
		{"Game (Disc 1).cue", "Game (USA) (Disc 1).cue"},
	}
	for _, pair := range pairs {
		roms := &identifyTestROMRepository{anyFile: true}
		first := discUpload(t, roms, "user-1", pair[0], "rf-1")
		second := discUpload(t, roms, "user-1", pair[1], "rf-2")
		if first.ID == second.ID || len(roms.roms) != 2 {
			t.Errorf("%q and %q were grouped", pair[0], pair[1])
		}
	}
}

func TestOtherUploadersDiscsAreNotJoined(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	first := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).cue", "rf-1")
	second := discUpload(t, roms, "user-2", "Game (USA) (Disc 1).cue", "rf-2")
	if first.ID == second.ID || len(roms.roms) != 2 {
		t.Fatalf("two uploaders share a ROM: %d roms", len(roms.roms))
	}
}

func TestDiscTwoBeforeDiscOne(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	second := discUpload(t, roms, "user-1", "Game (USA) (Disc 2).cue", "rf-2")
	if numbers := discNumbers(t, roms, second.ID); len(numbers) != 1 || numbers[2] == "" {
		t.Fatalf("discs = %v, want only disc 2", numbers)
	}
	first := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).cue", "rf-1")
	if first.ID != second.ID || len(roms.roms) != 1 {
		t.Fatalf("disc 1 made its own title")
	}
	if first.ROMFileID != "rf-1" {
		t.Fatalf("primary file = %s, want disc 1's rf-1", first.ROMFileID)
	}
}

func TestLaterDiscJoinsAfterRenameAndScrape(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	first := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).iso", "rf-1")
	roms.roms[0].Identity.Title = "Renamed Game"
	roms.roms[0].Identity.Platform = "ps2"
	second := discUpload(t, roms, "user-1", "Game (USA) (Disc 2).iso", "rf-2")
	if second.ID != first.ID || len(roms.roms) != 1 {
		t.Fatalf("disc 2 did not join, roms = %d", len(roms.roms))
	}
	if second.Identity.Title != "Renamed Game" || second.Identity.Platform != "ps2" {
		t.Fatalf("identity changed to %+v", second.Identity)
	}
}

func TestATakenDiscNumberBecomesItsOwnROM(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	first := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).cue", "rf-1")
	second := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).cue", "rf-2")
	if second.ID == first.ID || second.DiscGroup != nil {
		t.Fatalf("second = %+v, want its own ROM without a group", second)
	}
	if numbers := discNumbers(t, roms, second.ID); len(numbers) != 1 || numbers[1] == "" {
		t.Fatalf("discs = %v, want disc 1", numbers)
	}
}

func TestReprocessingAnAttachedDiscChangesNothing(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	first := discUpload(t, roms, "user-1", "Game (USA) (Disc 1).cue", "rf-1")
	discUpload(t, roms, "user-1", "Game (USA) (Disc 2).cue", "rf-2")
	again := discUpload(t, roms, "user-1", "Game (USA) (Disc 2).cue", "rf-2")
	if again.ID != first.ID || len(roms.roms) != 1 || len(discNumbers(t, roms, first.ID)) != 2 || len(roms.discs) != 2 {
		t.Fatalf("roms = %d, discs = %v", len(roms.roms), roms.discs)
	}
}

func TestFilesWithoutAMarkerBehaveAsBefore(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	rom := discUpload(t, roms, "user-1", "Chrono Trigger (USA).sfc", "rf-1")
	if rom.DiscGroup != nil || rom.Identity.Title != "Chrono Trigger" || rom.Identity.Platform != "snes" {
		t.Fatalf("rom = %+v", rom)
	}
	if numbers := discNumbers(t, roms, rom.ID); len(numbers) != 1 || numbers[1] != "Chrono Trigger (USA).sfc" {
		t.Fatalf("discs = %v", numbers)
	}
}

func TestBareMarkersLeaveTheTitle(t *testing.T) {
	roms := &identifyTestROMRepository{anyFile: true}
	rom := discUpload(t, roms, "user-1", "Game - Disc 2.iso", "rf-1")
	if rom.Identity.Title != "Game" {
		t.Fatalf("title = %q, want Game", rom.Identity.Title)
	}
}

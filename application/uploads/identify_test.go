package uploads

import (
	"context"
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
	roms    []romdomain.ROM
	err     error
}

func (repository *identifyTestROMRepository) FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error) {
	return repository.romFile, nil
}

func (repository *identifyTestROMRepository) SaveROM(_ context.Context, rom romdomain.ROM) error {
	if repository.err != nil {
		return repository.err
	}
	repository.roms = append(repository.roms, rom)
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

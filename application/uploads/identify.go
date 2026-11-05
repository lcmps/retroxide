package uploads

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

type identifyUploadRepository interface {
	FindUpload(context.Context, string) (*uploadsdomain.Upload, error)
	SaveUpload(context.Context, uploadsdomain.Upload) error
}

type identifyROMRepository interface {
	FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error)
	SaveROM(context.Context, romdomain.ROM) error
	FindROMDiscs(context.Context, string) ([]romdomain.ROMDisc, error)
	FindROMByDiscGroup(context.Context, string, string) (*romdomain.ROM, error)
	RecordDisc(context.Context, romdomain.ROMDisc) error
}

type IdentifyService struct {
	uploadRepository identifyUploadRepository
	romRepository    identifyROMRepository
}

func NewIdentifyService(uploadRepository identifyUploadRepository, romRepository identifyROMRepository) *IdentifyService {
	return &IdentifyService{uploadRepository: uploadRepository, romRepository: romRepository}
}

func (service *IdentifyService) Identify(ctx context.Context, uploadID, romFileID string) error {
	upload, err := service.uploadRepository.FindUpload(ctx, uploadID)
	if err != nil {
		return err
	}
	if upload == nil {
		return fault.NotFound()
	}
	romFile, err := service.romRepository.FindROMFileByID(ctx, romFileID)
	if err != nil {
		return err
	}
	if romFile == nil {
		if err := upload.TransitionTo(uploadsdomain.UploadStatusFailed); err != nil {
			return fault.Internal(fmt.Errorf("transitioning upload to failed: %w", err))
		}
		message := "rom file not found for identification"
		upload.ErrorMessage = &message
		if err := service.uploadRepository.SaveUpload(ctx, *upload); err != nil {
			return err
		}
		return fault.NotFound()
	}

	romID, err := service.attachOrCreate(ctx, upload, romFileID)
	if err != nil {
		if transitionErr := upload.TransitionTo(uploadsdomain.UploadStatusFailed); transitionErr == nil {
			message := err.Error()
			upload.ErrorMessage = &message
			if saveErr := service.uploadRepository.SaveUpload(ctx, *upload); saveErr != nil {
				slog.ErrorContext(ctx, "failed to persist failed upload status", "upload_id", uploadID, "error", saveErr)
			}
		}
		return err
	}
	if err := upload.TransitionTo(uploadsdomain.UploadStatusCompleted); err != nil {
		return fault.Internal(fmt.Errorf("transitioning upload to completed: %w", err))
	}
	upload.ErrorMessage = nil
	upload.ROMID = &romID
	return service.uploadRepository.SaveUpload(ctx, *upload)
}

// attachOrCreate puts the upload's file in a ROM and returns that ROM's id. A file whose name
// marks it as a disc joins the uploader's ROM for the same release when one exists and the disc
// number is free; otherwise it becomes a new ROM. A unique-index conflict from a concurrent
// upload of another disc is returned, so the job's retry finds the ROM that won.
func (service *IdentifyService) attachOrCreate(ctx context.Context, upload *uploadsdomain.Upload, romFileID string) (string, error) {
	identity := InferIdentityFromFilename(upload.OriginalFilename)
	stem, _ := splitFilename(upload.OriginalFilename)
	disc, marked := romdomain.ParseDiscName(stem)
	number := 1
	var group *string
	if marked {
		number = disc.Number
		key := romdomain.DiscGroupKey(identity.Platform, disc.Base)
		group = &key
		existing, err := service.romRepository.FindROMByDiscGroup(ctx, upload.UploaderID, key)
		if err != nil {
			return "", err
		}
		if existing != nil {
			discs, err := service.romRepository.FindROMDiscs(ctx, existing.ID)
			if err != nil {
				return "", err
			}
			attached, taken := false, false
			for _, existingDisc := range discs {
				attached = attached || existingDisc.ROMFileID == romFileID
				taken = taken || existingDisc.Number == number
			}
			if attached {
				return existing.ID, nil
			}
			if !taken {
				return existing.ID, service.romRepository.RecordDisc(ctx, romdomain.ROMDisc{ROMID: existing.ID, ROMFileID: romFileID, Number: number, Filename: upload.OriginalFilename})
			}
			group = nil
		}
	}
	romID, err := id.New("rom")
	if err != nil {
		return "", fault.Internal(fmt.Errorf("generating rom id: %w", err))
	}
	rom := romdomain.ROM{ID: romID, ROMFileID: romFileID, UploaderID: upload.UploaderID, DiscGroup: group, Identity: identity}
	if err := service.romRepository.SaveROM(ctx, rom); err != nil {
		return "", err
	}
	return romID, service.romRepository.RecordDisc(ctx, romdomain.ROMDisc{ROMID: romID, ROMFileID: romFileID, Number: number, Filename: upload.OriginalFilename})
}

func InferIdentityFromFilename(filename string) romdomain.ROMIdentity {
	name, extension := splitFilename(filename)
	if disc, ok := romdomain.ParseDiscName(name); ok {
		name = disc.Base
	}
	lowerName := strings.ToLower(name)
	platform := inferPlatform(extension)
	region := inferRegion(lowerName)
	version := inferVersion(name, lowerName)
	confidence := uint8(70)
	if platform == romdomain.PlatformUnknown {
		confidence = 45
	} else if region != nil || version != nil {
		confidence = 80
	}
	return romdomain.ROMIdentity{
		Title:        inferTitle(name),
		Platform:     platform,
		Region:       region,
		Version:      version,
		IdentifiedBy: romdomain.IdentifiedByFilename,
		Confidence:   confidence,
	}
}

func splitFilename(filename string) (string, string) {
	index := strings.LastIndexByte(filename, '.')
	if index < 0 {
		return filename, ""
	}
	return filename[:index], filename[index+1:]
}

func inferPlatform(extension string) string {
	return romdomain.PlatformForExtension(extension)
}

func inferRegion(lowerName string) *string {
	regions := []struct {
		markers []string
		name    string
	}{
		{markers: []string{"(usa)", "[usa]", "(us)", "[us]"}, name: "usa"},
		{markers: []string{"(europe)", "[europe]", "(eu)", "[eu]"}, name: "europe"},
		{markers: []string{"(japan)", "[japan]", "(jp)", "[jp]"}, name: "japan"},
		{markers: []string{"(world)", "[world]"}, name: "world"},
	}
	for _, region := range regions {
		for _, marker := range region.markers {
			if strings.Contains(lowerName, marker) {
				value := region.name
				return &value
			}
		}
	}
	return nil
}

func inferVersion(name, lowerName string) *string {
	for _, marker := range []string{"rev ", "version "} {
		if index := strings.Index(lowerName, marker); index >= 0 {
			return extractVersion(name[index:])
		}
	}
	if index := strings.IndexByte(lowerName, 'v'); index >= 0 && index+1 < len(lowerName) && lowerName[index+1] >= '0' && lowerName[index+1] <= '9' {
		return extractVersion(name[index:])
	}
	return nil
}

func extractVersion(fragment string) *string {
	if index := strings.IndexAny(fragment, ")]("); index >= 0 {
		fragment = fragment[:index]
	}
	version := strings.TrimSpace(fragment)
	if version == "" {
		return nil
	}
	return &version
}

func inferTitle(name string) string {
	originalName := name
	if index := strings.Index(name, " ("); index >= 0 {
		name = name[:index]
	}
	if index := strings.IndexByte(name, '['); index >= 0 {
		name = name[:index]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return strings.TrimSpace(originalName)
	}
	return name
}

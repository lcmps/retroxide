package rom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Service struct {
	repository Repository
	storage    Storage
	audit      Audit
	scraper    Scraper
}

const scrapedConfidence = 95

func NewService(repository Repository, storage Storage, audit Audit, scraper Scraper) *Service {
	return &Service{repository: repository, storage: storage, audit: audit, scraper: scraper}
}

func (service *Service) List(ctx context.Context, requester identity.Requester, request ListRequest) (ROMListResponseDTO, error) {
	return NewListROMService(service.repository).List(ctx, requester, request)
}

func (service *Service) Get(ctx context.Context, requester identity.Requester, romID string) (ROMDetailDTO, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
}

func (service *Service) Update(ctx context.Context, requester identity.Requester, romID string, input UpdateROMRequest) (ROMDetailDTO, error) {
	if err := ValidateUpdate(input.Title, input.Platform, input.Region, input.Version); err != nil {
		return ROMDetailDTO{}, err
	}
	if err := ValidateDetails(input.Publisher, input.ReleaseYear, input.Description); err != nil {
		return ROMDetailDTO{}, err
	}
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	title := strings.TrimSpace(input.Title)
	platform := normalizePlatform(input.Platform)
	// A hand-corrected title or platform is final: scrapes only fill in inferred values.
	if title != rom.Identity.Title || !strings.EqualFold(platform, rom.Identity.Platform) {
		rom.Identity.IdentifiedBy = romdomain.IdentifiedByManual
		rom.Identity.Confidence = 100
	}
	rom.Identity.Title = title
	rom.Identity.Platform = platform
	rom.Identity.Region = optionalUnlessWhitespace(input.Region)
	rom.Identity.Version = optionalUnlessWhitespace(input.Version)
	for _, field := range []struct {
		input  *string
		target **string
	}{
		{input.Publisher, &rom.Identity.Publisher},
		{input.ReleaseYear, &rom.Identity.ReleaseYear},
		{input.Description, &rom.Identity.Description},
	} {
		if field.input != nil {
			*field.target = optionalUnlessWhitespace(field.input)
		}
	}
	if err := service.repository.SaveROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
}

func (service *Service) Delete(ctx context.Context, requester identity.Requester, romID string) error {
	if _, err := service.findAccessibleROM(ctx, requester, romID); err != nil {
		return err
	}
	return service.repository.HardDeleteROM(ctx, romID, func(path string) error {
		if err := service.storage.Delete(ctx, path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fault.Internal(fmt.Errorf("deleting ROM content: %w", err))
		}
		return nil
	})
}

func (service *Service) Download(ctx context.Context, requester identity.Requester, romID string) (DownloadOutput, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return DownloadOutput{}, err
	}
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return DownloadOutput{}, err
	}
	if romFile == nil {
		return DownloadOutput{}, fault.Internal(fmt.Errorf("rom_file %s not found", rom.ROMFileID))
	}
	filename, err := service.downloadFilename(ctx, *rom)
	if err != nil {
		return DownloadOutput{}, err
	}
	content, err := service.storage.Open(ctx, romFile.StoragePath)
	if err != nil {
		return DownloadOutput{}, err
	}
	return DownloadOutput{
		Filename: filename,
		MIMEType: "application/octet-stream",
		SHA256:   romFile.Hash.SHA256,
		Content:  content,
		Served: func(ctx context.Context, bytesSent int64) {
			if service.audit != nil {
				service.audit.DownloadServed(ctx, romID, romFile.ID, romFile.Hash.SHA256, uint64(max(bytesSent, 0)))
			}
		},
	}, nil
}

func (service *Service) DownloadFilename(ctx context.Context, requester identity.Requester, romID string) (string, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return "", err
	}
	return service.downloadFilename(ctx, *rom)
}

func (service *Service) downloadFilename(ctx context.Context, rom romdomain.ROM) (string, error) {
	uploaded, err := service.repository.FindUploadedFilename(ctx, rom.ID)
	if err != nil {
		return "", err
	}
	return downloadFilename(rom, uploaded), nil
}

func (service *Service) UploadCover(ctx context.Context, requester identity.Requester, romID string, content []byte) (ROMDetailDTO, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	digest := sha256.Sum256(content)
	storagePath, err := service.storage.Write(ctx, hex.EncodeToString(digest[:]), content)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	rom.Identity.CoverArtPath = &storagePath
	if err := service.repository.SaveROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
}

func (service *Service) ReadCover(ctx context.Context, requester identity.Requester, romID string) ([]byte, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return nil, err
	}
	if rom.Identity.CoverArtPath == nil {
		return nil, fault.NotFound()
	}
	return service.storage.Read(ctx, *rom.Identity.CoverArtPath)
}

// Scrape fills only missing metadata and images. The title and platform are replaced only while
// still inferred from the filename (an unknown platform is always filled in).
func (service *Service) Scrape(ctx context.Context, requester identity.Requester, romID string) (ROMDetailDTO, error) {
	if service.scraper == nil {
		return ROMDetailDTO{}, fault.Internal(errors.New("scraper is not configured"))
	}
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	return service.scrape(ctx, rom)
}

// ScrapeUpload scrapes the ROM an upload created, on behalf of the background job queued once the
// upload is identified. Finding no match, or the ROM having been deleted since, is not an error;
// an unavailable metadata service is, so the job is retried.
func (service *Service) ScrapeUpload(ctx context.Context, uploadID string) error {
	if service.scraper == nil {
		return nil
	}
	romID, err := service.repository.FindROMIDByUploadID(ctx, uploadID)
	if err != nil || romID == nil {
		return err
	}
	rom, err := service.repository.FindROMByID(ctx, *romID)
	if err != nil || rom == nil {
		return err
	}
	if _, err := service.scrape(ctx, rom); err != nil && !fault.IsCode(err, fault.CodeNotFound) {
		return err
	}
	return nil
}

func (service *Service) scrape(ctx context.Context, rom *romdomain.ROM) (ROMDetailDTO, error) {
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if romFile == nil {
		return ROMDetailDTO{}, fault.Internal(fmt.Errorf("rom_file %s not found", rom.ROMFileID))
	}
	result, err := service.scraper.Scrape(ctx, ScrapeQuery{
		Platform: rom.Identity.Platform, SHA1: romFile.Hash.SHA1, CRC32: romFile.Hash.CRC32, MD5: romFile.Hash.MD5,
	})
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if result == nil {
		return ROMDetailDTO{}, &fault.Error{Code: fault.CodeNotFound, Message: "no match found for this ROM"}
	}

	identity := &rom.Identity
	inferred := identity.IdentifiedBy == romdomain.IdentifiedByFilename
	if platform := strings.TrimSpace(result.Platform); platform != "" && len(platform) <= MaxPlatformLength &&
		(inferred || romdomain.IsUnknownPlatform(identity.Platform)) {
		identity.Platform = platform
	}
	if title := strings.TrimSpace(result.Title); title != "" && inferred {
		identity.Title = title
		identity.IdentifiedBy = result.Source
		identity.Confidence = scrapedConfidence
	}
	fillMissing(&identity.Region, result.Region)
	fillMissing(&identity.Publisher, result.Publisher)
	fillMissing(&identity.ReleaseYear, result.ReleaseYear)
	fillMissing(&identity.Description, result.Description)
	for _, image := range []struct {
		target  **string
		content []byte
	}{
		{&identity.CoverArtPath, result.Cover},
		{&identity.TitleScreenPath, result.TitleScreen},
		{&identity.ScreenshotPath, result.Screenshot},
	} {
		if *image.target != nil || len(image.content) == 0 {
			continue
		}
		digest := sha256.Sum256(image.content)
		storagePath, err := service.storage.Write(ctx, hex.EncodeToString(digest[:]), image.content)
		if err != nil {
			return ROMDetailDTO{}, err
		}
		*image.target = &storagePath
	}
	if err := service.repository.SaveROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return detailOf(*rom, *romFile), nil
}

func (service *Service) ReadMedia(ctx context.Context, requester identity.Requester, romID, kind string) ([]byte, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return nil, err
	}
	var path *string
	switch kind {
	case MediaTitleScreen:
		path = rom.Identity.TitleScreenPath
	case MediaScreenshot:
		path = rom.Identity.ScreenshotPath
	}
	if path == nil {
		return nil, fault.NotFound()
	}
	return service.storage.Read(ctx, *path)
}

func fillMissing(target **string, value string) {
	if value = strings.TrimSpace(value); *target == nil && value != "" {
		*target = &value
	}
}

func (service *Service) findAccessibleROM(ctx context.Context, requester identity.Requester, romID string) (*romdomain.ROM, error) {
	rom, err := service.repository.FindROMByID(ctx, romID)
	if err != nil {
		return nil, err
	}
	if rom == nil || !requester.CanAccess(rom.UploaderID) {
		return nil, fault.NotFound()
	}
	return rom, nil
}

func (service *Service) detail(ctx context.Context, rom romdomain.ROM) (ROMDetailDTO, error) {
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if romFile == nil {
		return ROMDetailDTO{}, fault.Internal(fmt.Errorf("rom_file %s not found", rom.ROMFileID))
	}
	return detailOf(rom, *romFile), nil
}

// detailOf maps a ROM and its content file to the detail response.
func detailOf(rom romdomain.ROM, romFile romdomain.ROMFile) ROMDetailDTO {
	return ROMDetailDTO{
		ROMID:          rom.ID,
		Title:          rom.Identity.Title,
		Platform:       rom.Identity.Platform,
		PlatformName:   romdomain.PlatformName(rom.Identity.Platform),
		Region:         rom.Identity.Region,
		Version:        rom.Identity.Version,
		Publisher:      rom.Identity.Publisher,
		ReleaseYear:    rom.Identity.ReleaseYear,
		Description:    rom.Identity.Description,
		CoverArtURL:    imageURL(rom.ID, "cover", rom.Identity.CoverArtPath),
		TitleScreenURL: imageURL(rom.ID, "media/"+MediaTitleScreen, rom.Identity.TitleScreenPath),
		ScreenshotURL:  imageURL(rom.ID, "media/"+MediaScreenshot, rom.Identity.ScreenshotPath),
		Hash: ROMHashDTO{
			SHA256: romFile.Hash.SHA256,
			SHA1:   romFile.Hash.SHA1,
			MD5:    romFile.Hash.MD5,
			CRC32:  romFile.Hash.CRC32,
		},
	}
}

// optionalUnlessWhitespace stores a trimmed copy of value, or nil when it is blank.
func optionalUnlessWhitespace(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// normalizePlatform stores catalog platforms by their lowercase key and keeps other names as
// entered, so a platform named by a metadata source keeps its capitalization.
func normalizePlatform(raw string) string {
	trimmed := strings.TrimSpace(raw)
	key := romdomain.PlatformKeyForName(trimmed)
	if _, known := romdomain.PlatformByKey(key); known {
		return key
	}
	return trimmed
}

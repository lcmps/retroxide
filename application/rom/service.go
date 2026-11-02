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

func (service *Service) List(ctx context.Context, requester identity.Requester, limit int, cursor, platform *string, identified *bool) (ROMListResponseDTO, error) {
	return NewListROMService(service.repository).List(ctx, requester, limit, cursor, platform, identified)
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
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title != rom.Identity.Title {
		rom.Identity.IdentifiedBy = romdomain.IdentifiedByManual
		rom.Identity.Confidence = 100
	}
	rom.Identity.Title = title
	rom.Identity.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	rom.Identity.Region = optionalUnlessWhitespace(input.Region)
	rom.Identity.Version = optionalUnlessWhitespace(input.Version)
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
	content, err := service.storage.Open(ctx, romFile.StoragePath)
	if err != nil {
		return DownloadOutput{}, err
	}
	return DownloadOutput{
		Filename: romID + ".rom",
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

// Scrape fills only missing metadata and images; titles are replaced only while still inferred from the filename.
func (service *Service) Scrape(ctx context.Context, requester identity.Requester, romID string) (ROMDetailDTO, error) {
	if service.scraper == nil {
		return ROMDetailDTO{}, fault.Internal(errors.New("scraper is not configured"))
	}
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
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
	if title := strings.TrimSpace(result.Title); title != "" && identity.IdentifiedBy == romdomain.IdentifiedByFilename {
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

func optionalUnlessWhitespace(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

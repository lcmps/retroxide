package rom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Service struct {
	repository Repository
	storage    Storage
	audit      Audit
}

func NewService(repository Repository, storage Storage, audit Audit) *Service {
	return &Service{repository: repository, storage: storage, audit: audit}
}

func (service *Service) List(ctx context.Context, limit int, cursor, platform *string, identified *bool) (ROMListResponseDTO, error) {
	return NewListROMService(service.repository).List(ctx, limit, cursor, platform, identified)
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
	rom.Identity.Title = strings.TrimSpace(input.Title)
	rom.Identity.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	rom.Identity.Region = optionalUnlessWhitespace(input.Region)
	rom.Identity.Version = optionalUnlessWhitespace(input.Version)
	if err := service.repository.SaveROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
}

func (service *Service) Delete(ctx context.Context, requester identity.Requester, romID string) error {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return err
	}
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return err
	}
	if romFile == nil {
		return fault.Internal(fmt.Errorf("rom_file %s not found", rom.ROMFileID))
	}
	if err := service.repository.DeleteROM(ctx, romID); err != nil {
		return err
	}
	remaining, err := service.repository.CountROMsByROMFileID(ctx, rom.ROMFileID)
	if err != nil {
		return err
	}
	if remaining == 0 {
		if err := service.repository.DeleteROMFile(ctx, rom.ROMFileID); err != nil {
			return err
		}
		if err := service.storage.Delete(ctx, romFile.StoragePath); err != nil {
			slog.ErrorContext(ctx, "failed to delete unreferenced rom bytes", "storage_path", romFile.StoragePath, "error", err)
		}
	}
	return nil
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
	content, err := service.storage.Read(ctx, romFile.StoragePath)
	if err != nil {
		return DownloadOutput{}, err
	}
	lastModified, err := service.storage.LastModified(ctx, romFile.StoragePath)
	if err != nil {
		return DownloadOutput{}, err
	}
	if service.audit != nil {
		service.audit.DownloadServed(ctx, romID, romFile.ID, romFile.Hash.SHA256, uint64(len(content)))
	}
	return DownloadOutput{
		Filename:                romID + ".rom",
		MIMEType:                "application/octet-stream",
		Bytes:                   content,
		SHA256:                  romFile.Hash.SHA256,
		LastModifiedUnixSeconds: lastModified,
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
	var coverArtURL *string
	if rom.Identity.CoverArtPath != nil {
		url := "/api/v1/rom/" + rom.ID + "/cover"
		coverArtURL = &url
	}
	return ROMDetailDTO{
		ROMID:       rom.ID,
		Title:       rom.Identity.Title,
		Platform:    rom.Identity.Platform,
		Region:      rom.Identity.Region,
		Version:     rom.Identity.Version,
		CoverArtURL: coverArtURL,
		Hash: ROMHashDTO{
			SHA256: romFile.Hash.SHA256,
			SHA1:   romFile.Hash.SHA1,
			MD5:    romFile.Hash.MD5,
			CRC32:  romFile.Hash.CRC32,
		},
	}, nil
}

func optionalUnlessWhitespace(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

package bios

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
)

const (
	maxLabelRunes = 64
	// maxDateRunes matches the release_date column (16 characters).
	maxDateRunes = 16
	maxNoteRunes = 255
)

type Service struct {
	repository Repository
	storage    rom.Storage
	known      KnownList
}

var _ API = (*Service)(nil)

func NewService(repository Repository, storage rom.Storage, known KnownList) *Service {
	return &Service{repository: repository, storage: storage, known: known}
}

// List returns every BIOS file, of one platform when platform is set. BIOS files are shared, so
// every requester sees them all.
func (service *Service) List(ctx context.Context, requester identity.Requester, platform *string) ([]DTO, error) {
	if platform != nil && strings.TrimSpace(*platform) == "" {
		platform = nil
	}
	entries, err := service.repository.ListBIOS(ctx, platform)
	if err != nil {
		return nil, err
	}
	result := make([]DTO, 0, len(entries))
	for _, entry := range entries {
		result = append(result, toDTO(requester, entry))
	}
	return result, nil
}

func (service *Service) Get(ctx context.Context, requester identity.Requester, id string) (DTO, error) {
	entry, err := service.find(ctx, id)
	if err != nil {
		return DTO{}, err
	}
	return toDTO(requester, *entry), nil
}

func (service *Service) DownloadFilename(ctx context.Context, _ identity.Requester, id string) (string, error) {
	entry, err := service.find(ctx, id)
	if err != nil {
		return "", err
	}
	return entry.DownloadName(), nil
}

// Download opens a BIOS file for any requester, named as emulators expect it when recognized.
func (service *Service) Download(ctx context.Context, _ identity.Requester, id string) (rom.DownloadOutput, error) {
	entry, err := service.find(ctx, id)
	if err != nil {
		return rom.DownloadOutput{}, err
	}
	file, err := service.repository.FindROMFileByID(ctx, entry.ROMFileID)
	if err != nil {
		return rom.DownloadOutput{}, err
	}
	if file == nil {
		return rom.DownloadOutput{}, fault.NotFound()
	}
	content, err := service.storage.Open(ctx, file.StoragePath)
	if err != nil {
		return rom.DownloadOutput{}, err
	}
	return rom.DownloadOutput{
		Filename: entry.DownloadName(), MIMEType: "application/octet-stream", SHA256: entry.Hash.SHA256, Content: content,
	}, nil
}

// Label sets the labels of an unrecognized BIOS file; recognized files take theirs from the list.
func (service *Service) Label(ctx context.Context, requester identity.Requester, id string, input LabelInput) (DTO, error) {
	entry, err := service.modifiable(ctx, requester, id)
	if err != nil {
		return DTO{}, err
	}
	if entry.Recognized {
		return DTO{}, fault.Validation("recognized BIOS labels come from the known list")
	}
	labels := biosdomain.Labels{Region: trimmed(input.Region), Version: trimmed(input.Version), Model: trimmed(input.Model), Date: trimmed(input.Date), Note: trimmed(input.Note)}
	for _, field := range []struct {
		value *string
		limit int
	}{{labels.Region, maxLabelRunes}, {labels.Version, maxLabelRunes}, {labels.Model, maxLabelRunes}, {labels.Date, maxDateRunes}, {labels.Note, maxNoteRunes}} {
		if field.value != nil && utf8.RuneCountInString(*field.value) > field.limit {
			return DTO{}, fault.Validation("BIOS labels must be at most 64 characters, dates at most 16, notes at most 255")
		}
	}
	entry.Labels = labels
	if err := service.repository.UpdateBIOS(ctx, *entry); err != nil {
		return DTO{}, err
	}
	return toDTO(requester, *entry), nil
}

func (service *Service) Delete(ctx context.Context, requester identity.Requester, id string) error {
	if _, err := service.modifiable(ctx, requester, id); err != nil {
		return err
	}
	return service.repository.HardDeleteBIOS(ctx, id, func(path string) error {
		return service.storage.Delete(context.WithoutCancel(ctx), path)
	})
}

func (service *Service) RecheckUnrecognized(ctx context.Context) (int, error) {
	if service.known == nil {
		return 0, nil
	}
	entries, err := service.repository.ListUnrecognizedBIOS(ctx)
	if err != nil {
		return 0, err
	}
	recognized := 0
	var errs []error
	for _, entry := range entries {
		known, ok := service.known.Lookup(entry.Hash.SHA1, entry.Size)
		if !ok {
			continue
		}
		path := known.Path
		entry.Platform, entry.ExpectedPath, entry.Recognized, entry.Labels = known.Platform, &path, true, known.Labels
		if err := service.repository.UpdateBIOS(ctx, entry); err != nil {
			errs = append(errs, err)
			continue
		}
		recognized++
	}
	return recognized, errors.Join(errs...)
}

func (service *Service) find(ctx context.Context, id string) (*biosdomain.BIOS, error) {
	entry, err := service.repository.FindBIOS(ctx, id)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fault.NotFound()
	}
	return entry, nil
}

func (service *Service) modifiable(ctx context.Context, requester identity.Requester, id string) (*biosdomain.BIOS, error) {
	entry, err := service.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if !biosdomain.CanModify(requester, *entry) {
		return nil, fault.Forbidden()
	}
	return entry, nil
}

func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return &text
}

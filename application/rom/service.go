package rom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type Service struct {
	repository     Repository
	storage        Storage
	audit          Audit
	scraper        Scraper
	thumbnailer    Thumbnailer
	thumbnailSlots chan struct{}
	discs          Discs
	wakeCompressor func()
}

// WithDiscs lets discs download in their other formats and be compressed; wake starts the
// background compressor after a request.
func (service *Service) WithDiscs(discs Discs, wake func()) *Service {
	service.discs, service.wakeCompressor = discs, wake
	return service
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
	if err := ValidateDetails(input.Publisher, input.ReleaseYear, input.Genre, input.Description); err != nil {
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
		{input.Genre, &rom.Identity.Genre},
		{input.Description, &rom.Identity.Description},
	} {
		if field.input != nil {
			*field.target = optionalUnlessWhitespace(field.input)
		}
	}
	if err := service.repository.UpdateROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
}

func (service *Service) Delete(ctx context.Context, requester identity.Requester, romID string) error {
	if _, err := service.findAccessibleROM(ctx, requester, romID); err != nil {
		return err
	}
	return service.repository.HardDeleteROM(ctx, romID, service.deleteStoredFile(ctx))
}

// deleteStoredFile returns the callback a deletion runs, once committed, for each file nothing
// references any more. A file left behind is reclaimed by the storage sweep.
func (service *Service) deleteStoredFile(ctx context.Context) func(string) error {
	return func(path string) error {
		if err := service.storage.Delete(context.WithoutCancel(ctx), path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.WarnContext(ctx, "could not delete stored file; the storage sweep will reclaim it", "path", path, "error", err)
		}
		return nil
	}
}

// Download downloads a ROM in its first format: the one it was uploaded in.
func (service *Service) Download(ctx context.Context, requester identity.Requester, romID string) (DownloadOutput, error) {
	return service.DownloadAs(ctx, requester, romID, "")
}

// DownloadAs downloads a ROM in format, one of those its details list (empty for the first).
func (service *Service) DownloadAs(ctx context.Context, requester identity.Requester, romID string, format DownloadFormat) (DownloadOutput, error) {
	rom, romFile, tracks, filename, err := service.downloadSubject(ctx, requester, romID)
	if err != nil {
		return DownloadOutput{}, err
	}
	option, err := pickDownload(service.downloadOptions(*romFile, tracks, filename), format)
	if err != nil {
		return DownloadOutput{}, err
	}
	base := strings.TrimSuffix(filename, path.Ext(filename))
	served := func(ctx context.Context, bytesSent int64) {
		if service.audit != nil {
			service.audit.DownloadServed(ctx, rom.ID, romFile.ID, romFile.Hash.SHA256, uint64(max(bytesSent, 0)))
		}
	}
	switch option.Format {
	case DownloadTracks:
		return DownloadOutput{
			Filename: base + option.Extension, MIMEType: "application/zip", SHA256: romFile.Hash.SHA256, Served: served,
			Archive: func(out io.Writer) error {
				return service.discs.Archive(context.WithoutCancel(ctx), *romFile, base, out)
			},
		}, nil
	case DownloadISO:
		return DownloadOutput{
			Filename: base + option.Extension, MIMEType: "application/octet-stream", SHA256: romFile.Hash.SHA256, Served: served,
			Archive: func(out io.Writer) error { return service.discs.ISO(context.WithoutCancel(ctx), *romFile, out) },
		}, nil
	}
	content, err := service.storage.Open(ctx, romFile.StoragePath)
	if err != nil {
		return DownloadOutput{}, err
	}
	return DownloadOutput{
		Filename: base + option.Extension, MIMEType: "application/octet-stream", SHA256: romFile.Hash.SHA256,
		Content: content, Served: served,
	}, nil
}

// downloadSubject loads what a download needs: the ROM, its file and tracks, and its name.
func (service *Service) downloadSubject(ctx context.Context, requester identity.Requester, romID string) (*romdomain.ROM, *romdomain.ROMFile, []romdomain.DiscTrack, string, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if romFile == nil {
		return nil, nil, nil, "", fault.Internal(fmt.Errorf("rom_file %s not found", rom.ROMFileID))
	}
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	filename, err := service.downloadFilename(ctx, *rom)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return rom, romFile, tracks, filename, nil
}

// pickDownload returns the option for format, or the first for an empty format.
func pickDownload(options []DownloadOptionDTO, format DownloadFormat) (DownloadOptionDTO, error) {
	if format == "" {
		return options[0], nil
	}
	for _, option := range options {
		if option.Format == format {
			return option, nil
		}
	}
	return DownloadOptionDTO{}, fault.Validation(fmt.Sprintf("this ROM does not download as %q", format))
}

// downloadOptions lists the formats a ROM downloads in, the one it was uploaded in first.
func (service *Service) downloadOptions(romFile romdomain.ROMFile, tracks []romdomain.DiscTrack, filename string) []DownloadOptionDTO {
	extension := path.Ext(filename)
	if (romFile.Format != romdomain.FormatCHD && romFile.Format != romdomain.FormatTracks) || service.discs == nil {
		return []DownloadOptionDTO{{Format: DownloadFile, Extension: extension}}
	}
	zip := DownloadOptionDTO{Format: DownloadTracks, Extension: ".zip", Sheet: "cue"}
	for _, track := range tracks {
		if strings.EqualFold(path.Ext(track.Filename), ".raw") {
			zip.Sheet = "gdi" // GDI discs name their audio tracks .raw
		}
	}
	if strings.EqualFold(extension, ".gdi") {
		zip.Sheet = "gdi"
	}
	chd := DownloadOptionDTO{Format: DownloadCHD, Extension: ".chd"}
	var options []DownloadOptionDTO
	switch {
	case romFile.Format == romdomain.FormatTracks:
		options = []DownloadOptionDTO{zip}
	case romFile.SheetPath != nil: // uploaded as tracks, compressed since
		options = []DownloadOptionDTO{zip, chd}
	default: // uploaded as a CHD
		options = []DownloadOptionDTO{chd, zip}
	}
	if romdomain.ISOAvailable(tracks) {
		options = append(options, DownloadOptionDTO{Format: DownloadISO, Extension: ".iso"})
	}
	return options
}

// RequestCompression asks for a disc kept as tracks to be compressed to CHD in the background.
func (service *Service) RequestCompression(ctx context.Context, requester identity.Requester, romID string) (ROMDetailDTO, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if service.discs == nil {
		return ROMDetailDTO{}, fault.Validation("only discs can be compressed")
	}
	romFile, err := service.repository.FindROMFileByID(ctx, rom.ROMFileID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if romFile == nil {
		return ROMDetailDTO{}, fault.NotFound()
	}
	if err := service.discs.RequestCompression(ctx, *romFile); err != nil {
		return ROMDetailDTO{}, err
	}
	if service.wakeCompressor != nil {
		service.wakeCompressor()
	}
	return service.detail(ctx, *rom)
}

// DownloadFilename returns the name an accessible ROM downloads as in format (empty for the
// first of its formats).
func (service *Service) DownloadFilename(ctx context.Context, requester identity.Requester, romID string, format DownloadFormat) (string, error) {
	_, romFile, tracks, filename, err := service.downloadSubject(ctx, requester, romID)
	if err != nil {
		return "", err
	}
	option, err := pickDownload(service.downloadOptions(*romFile, tracks, filename), format)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(filename, path.Ext(filename)) + option.Extension, nil
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
	if err := service.storeCover(ctx, &rom.Identity, content); err != nil {
		return ROMDetailDTO{}, err
	}
	if err := service.repository.UpdateROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
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
	query := ScrapeQuery{Platform: rom.Identity.Platform, SHA1: romFile.Hash.SHA1, CRC32: romFile.Hash.CRC32, MD5: romFile.Hash.MD5}
	// Disc databases list track files, so a disc is looked up by its primary track.
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if primary, ok := romdomain.PrimaryTrack(tracks); ok {
		query.SHA1, query.CRC32, query.MD5 = primary.Hash.SHA1, primary.Hash.CRC32, primary.Hash.MD5
	}
	result, err := service.scraper.Scrape(ctx, query)
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
	if len(strings.TrimSpace(result.Genre)) <= MaxGenreLength {
		fillMissing(&identity.Genre, result.Genre)
	}
	fillMissing(&identity.Description, result.Description)
	if identity.CoverArtPath == nil && len(result.Cover) > 0 {
		if err := service.storeCover(ctx, identity, result.Cover); err != nil {
			return ROMDetailDTO{}, err
		}
	}
	for _, image := range []struct {
		target  **string
		content []byte
	}{
		{&identity.TitleScreenPath, result.TitleScreen},
		{&identity.ScreenshotPath, result.Screenshot},
	} {
		if *image.target != nil || len(image.content) == 0 {
			continue
		}
		storagePath, err := service.storeImage(ctx, image.content)
		if err != nil {
			return ROMDetailDTO{}, err
		}
		*image.target = &storagePath
	}
	if err := service.repository.UpdateROM(ctx, *rom); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.detail(ctx, *rom)
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
	detail := detailOf(rom, *romFile)
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	filename, err := service.downloadFilename(ctx, rom)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	detail.Downloads = service.downloadOptions(*romFile, tracks, filename)
	if romFile.Format == romdomain.FormatCHD || romFile.Format == romdomain.FormatTracks {
		if len(tracks) > 0 {
			disc := &DiscDTO{
				Format: string(romFile.Format), Tracks: make([]DiscTrackDTO, len(tracks)),
				CanCompress:          service.discs != nil && service.discs.CanCompressFile(ctx, *romFile),
				CompressionRequested: romFile.CompressRequested,
			}
			if romFile.CompressRequested && service.discs != nil {
				if percent, running := service.discs.CompressionProgress(romFile.ID); running {
					disc.CompressionPercent = &percent
				}
			}
			for index, track := range tracks {
				disc.Tracks[index] = DiscTrackDTO{
					Number: track.Number, Mode: track.Mode, Size: track.Size,
					SHA1: track.Hash.SHA1, MD5: track.Hash.MD5, CRC32: track.Hash.CRC32,
				}
			}
			detail.Disc = disc
		}
	}
	return detail, nil
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
		Genre:          rom.Identity.Genre,
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

package uploads

import (
	"context"
	"slices"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// MaxBIOSUploadBytes caps uploads marked as BIOS files, well above any real BIOS. The usual
// upload limit still applies when it is lower.
const MaxBIOSUploadBytes = 64 << 20

// BIOSOptions mark an upload as BIOS files for a platform, for files the known list may not
// recognize. Recognized BIOS files become BIOS entries without them.
type BIOSOptions struct {
	Enabled  bool
	Platform string
}

// KnownList recognizes BIOS files by checksum and lists the file types BIOS files come in.
type KnownList interface {
	Lookup(sha1 string, size uint64) (biosdomain.Known, bool)
	Extensions() []string
}

// BIOSLookup finds BIOS entries.
type BIOSLookup interface {
	FindBIOSByROMFileID(ctx context.Context, romFileID string) (*biosdomain.BIOS, error)
	FindBIOS(ctx context.Context, id string) (*biosdomain.BIOS, error)
}

// WithBIOS lets the service take BIOS files: uploads marked as BIOS files, and files the known
// list recognizes.
func (service *UploadService) WithBIOS(known KnownList, bioses BIOSLookup) *UploadService {
	service.known, service.bioses = known, bioses
	return service
}

// checkBIOSUpload applies the rules of an upload marked as BIOS files: a catalog platform, a file
// type BIOS files come in, and the BIOS size cap. size is 0 when not known yet.
func (service *UploadService) checkBIOSUpload(options BIOSOptions, filename string, size int64) error {
	if service.known == nil {
		return fault.Validation("this server does not take BIOS files")
	}
	if _, ok := romdomain.PlatformByKey(options.Platform); !ok {
		return fault.Validation("platform is not a known platform")
	}
	extension := ""
	if index := strings.LastIndexByte(filename, '.'); index >= 0 {
		extension = strings.ToLower(filename[index+1:])
	}
	if !slices.Contains(service.known.Extensions(), extension) {
		return fault.UnsupportedUploadType("BIOS files of this type are not known")
	}
	if limit := service.biosUploadLimit(); size > limit {
		return fault.UploadTooLarge(size, limit)
	}
	return nil
}

func (service *UploadService) biosUploadLimit() int64 {
	return min(service.policy.MaxUploadBytes, MaxBIOSUploadBytes)
}

// isBIOSTarget reports whether content becomes a BIOS entry: the upload says so, or the known
// list recognizes it.
func (service *UploadService) isBIOSTarget(options BIOSOptions, hash romdomain.ROMHash, size int64) bool {
	if options.Enabled {
		return true
	}
	if service.known == nil || size < 0 {
		return false
	}
	_, known := service.known.Lookup(hash.SHA1, uint64(size))
	return known
}

// existingBIOS returns the BIOS entry using a stored file, if any.
func (service *UploadService) existingBIOS(ctx context.Context, romFile romdomain.ROMFile) (*biosdomain.BIOS, error) {
	if service.bioses == nil {
		return nil, nil
	}
	return service.bioses.FindBIOSByROMFileID(ctx, romFile.ID)
}

// biosResult describes the BIOS entry an upload became, for its status.
func (service *UploadService) biosResult(ctx context.Context, biosID *string) (*BIOSResultDTO, error) {
	if biosID == nil || service.bioses == nil {
		return nil, nil
	}
	entry, err := service.bioses.FindBIOS(ctx, *biosID)
	if err != nil || entry == nil {
		return nil, err
	}
	return &BIOSResultDTO{
		Platform: entry.Platform, PlatformName: romdomain.PlatformName(entry.Platform),
		Region: entry.Labels.Region, Version: entry.Labels.Version, Recognized: entry.Recognized,
	}, nil
}

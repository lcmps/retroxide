package rom

import (
	"context"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// MergeROM moves the source ROM's discs onto the target, which keeps its own details, and deletes
// the source. The two must belong to one owner and be one platform, and not be two known regions.
func (service *Service) MergeROM(ctx context.Context, requester identity.Requester, targetID, sourceID string) (ROMDetailDTO, error) {
	if targetID == sourceID {
		return ROMDetailDTO{}, fault.Validation("a ROM cannot be merged with itself")
	}
	target, err := service.findAccessibleROM(ctx, requester, targetID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	source, err := service.findAccessibleROM(ctx, requester, sourceID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if target.UploaderID != source.UploaderID {
		return ROMDetailDTO{}, fault.Validation("ROMs of different owners cannot be merged")
	}
	if !strings.EqualFold(target.Identity.Platform, source.Identity.Platform) {
		return ROMDetailDTO{}, fault.Validation("ROMs of different platforms cannot be merged")
	}
	targetRegion, sourceRegion := derefOrEmpty(target.Identity.Region), derefOrEmpty(source.Identity.Region)
	if targetRegion != "" && sourceRegion != "" && !strings.EqualFold(targetRegion, sourceRegion) {
		return ROMDetailDTO{}, fault.Validation("ROMs of different regions cannot be merged")
	}
	if err := service.repository.MergeROMs(ctx, targetID, sourceID, service.deleteStoredFile(ctx)); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.reloadDetail(ctx, targetID)
}

// SplitDisc turns the disc with the number into a ROM of its own and returns that ROM.
func (service *Service) SplitDisc(ctx context.Context, requester identity.Requester, romID string, number int) (ROMDetailDTO, error) {
	disc, err := service.findDisc(ctx, requester, romID, number)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	newID, err := id.New("rom")
	if err != nil {
		return ROMDetailDTO{}, fault.Internal(err)
	}
	if err := service.repository.SplitROMDisc(ctx, romID, disc.ROMFileID, newID); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.reloadDetail(ctx, newID)
}

// ReorderDiscs numbers a ROM's discs in the order of romFileIDs, which must name each disc once.
func (service *Service) ReorderDiscs(ctx context.Context, requester identity.Requester, romID string, romFileIDs []string) (ROMDetailDTO, error) {
	if _, err := service.findAccessibleROM(ctx, requester, romID); err != nil {
		return ROMDetailDTO{}, err
	}
	if err := service.repository.ReorderROMDiscs(ctx, romID, romFileIDs); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.reloadDetail(ctx, romID)
}

// RemoveDisc deletes the disc with the number, and its file unless another ROM has it.
func (service *Service) RemoveDisc(ctx context.Context, requester identity.Requester, romID string, number int) (ROMDetailDTO, error) {
	disc, err := service.findDisc(ctx, requester, romID, number)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if err := service.repository.HardDeleteROMDisc(ctx, romID, disc.ROMFileID, service.deleteStoredFile(ctx)); err != nil {
		return ROMDetailDTO{}, err
	}
	return service.reloadDetail(ctx, romID)
}

func (service *Service) findDisc(ctx context.Context, requester identity.Requester, romID string, number int) (romdomain.ROMDisc, error) {
	if _, err := service.findAccessibleROM(ctx, requester, romID); err != nil {
		return romdomain.ROMDisc{}, err
	}
	discs, err := service.repository.FindROMDiscs(ctx, romID)
	if err != nil {
		return romdomain.ROMDisc{}, err
	}
	for _, disc := range discs {
		if disc.Number == number {
			return disc, nil
		}
	}
	return romdomain.ROMDisc{}, fault.NotFound()
}

func (service *Service) reloadDetail(ctx context.Context, romID string) (ROMDetailDTO, error) {
	rom, err := service.repository.FindROMByID(ctx, romID)
	if err != nil {
		return ROMDetailDTO{}, err
	}
	if rom == nil {
		return ROMDetailDTO{}, fault.NotFound()
	}
	return service.detail(ctx, *rom)
}

func derefOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

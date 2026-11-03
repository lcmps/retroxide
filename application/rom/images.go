package rom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// thumbnailSlots bounds how many covers are scaled at once. Scaling decodes the full-size
// cover, so on a small device a library page of new covers would otherwise compete with
// everything else for the CPU and memory.
const thumbnailSlots = 2

// Image is a stored image opened for streaming. Version is its SHA-256 when it is stored at
// its content address (every image this service stores), and empty otherwise.
type Image struct {
	StoredContent
	Version string
}

// WithThumbnails makes the service keep a small copy of every cover for library grids.
func (service *Service) WithThumbnails(thumbnailer Thumbnailer) *Service {
	service.thumbnailer = thumbnailer
	service.thumbnailSlots = make(chan struct{}, thumbnailSlots)
	return service
}

func (service *Service) OpenCover(ctx context.Context, requester identity.Requester, romID string) (Image, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return Image{}, err
	}
	if rom.Identity.CoverArtPath == nil {
		return Image{}, fault.NotFound()
	}
	return service.openImage(ctx, *rom.Identity.CoverArtPath)
}

// OpenCoverThumbnail opens the small copy of a ROM's cover. Covers stored before thumbnails
// existed get theirs made on first request.
func (service *Service) OpenCoverThumbnail(ctx context.Context, requester identity.Requester, romID string) (Image, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return Image{}, err
	}
	cover := rom.Identity.CoverArtPath
	if cover == nil {
		return Image{}, fault.NotFound()
	}
	path := *cover
	switch {
	case rom.Identity.CoverThumbPath != nil:
		path = *rom.Identity.CoverThumbPath
	case service.thumbnailer != nil:
		content, err := service.storage.Read(ctx, *cover)
		if err != nil {
			return Image{}, err
		}
		if path, err = service.thumbnailFor(ctx, *cover, content); err != nil {
			return Image{}, err
		}
		if err := service.repository.SetCoverThumbnail(ctx, rom.ID, *cover, path); err != nil {
			return Image{}, err
		}
	}
	return service.openImage(ctx, path)
}

func (service *Service) OpenMedia(ctx context.Context, requester identity.Requester, romID, kind string) (Image, error) {
	rom, err := service.findAccessibleROM(ctx, requester, romID)
	if err != nil {
		return Image{}, err
	}
	var path *string
	switch kind {
	case MediaTitleScreen:
		path = rom.Identity.TitleScreenPath
	case MediaScreenshot:
		path = rom.Identity.ScreenshotPath
	}
	if path == nil {
		return Image{}, fault.NotFound()
	}
	return service.openImage(ctx, *path)
}

func (service *Service) openImage(ctx context.Context, path string) (Image, error) {
	content, err := service.storage.Open(ctx, path)
	if err != nil {
		return Image{}, err
	}
	version := ""
	if address := filepath.Base(path); len(address) == sha256.Size*2 && isLowerHex(address) {
		version = address
	}
	return Image{StoredContent: content, Version: version}, nil
}

// storeCover stores a cover and its thumbnail, recording both on identity.
func (service *Service) storeCover(ctx context.Context, identity *romdomain.ROMIdentity, content []byte) error {
	cover, err := service.storeImage(ctx, content)
	if err != nil {
		return err
	}
	identity.CoverArtPath, identity.CoverThumbPath = &cover, nil
	if service.thumbnailer == nil {
		return nil
	}
	thumbnail, err := service.thumbnailFor(ctx, cover, content)
	if err != nil {
		return err
	}
	identity.CoverThumbPath = &thumbnail
	return nil
}

// storeImage stores content at its content address and returns its storage key.
func (service *Service) storeImage(ctx context.Context, content []byte) (string, error) {
	digest := sha256.Sum256(content)
	return service.storage.Write(ctx, hex.EncodeToString(digest[:]), content)
}

// thumbnailFor stores the thumbnail of the cover at coverPath and returns its key. A cover that
// is already small, or that cannot be decoded, serves as its own thumbnail, so each cover is
// scaled at most once.
func (service *Service) thumbnailFor(ctx context.Context, coverPath string, content []byte) (string, error) {
	select {
	case service.thumbnailSlots <- struct{}{}:
		defer func() { <-service.thumbnailSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	thumbnail, err := service.thumbnailer.Thumbnail(content)
	if err != nil {
		return coverPath, nil
	}
	return service.storeImage(ctx, thumbnail)
}

// Package share lets people share a ROM through single-use links: whoever redeems a link first
// gets their own copy of the ROM, using the same stored files.
package share

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// linkPath prefixes every share link; the web app serves the page that redeems it.
const linkPath = "/share/"

// tokenBytes is how much randomness a link carries.
const tokenBytes = 32

// Repository stores links and copies ROMs.
type Repository interface {
	FindROMByID(ctx context.Context, id string) (*romdomain.ROM, error)
	FindROMDiscs(ctx context.Context, romID string) ([]romdomain.ROMDisc, error)
	CreateShareLink(ctx context.Context, tokenHash, romID, createdBy string) error
	FindUnusedShareLink(ctx context.Context, tokenHash string) (romID string, found bool, err error)
	UserHasROMFile(ctx context.Context, userID, romFileID string) (bool, error)
	RedeemShareLink(ctx context.Context, tokenHash, userID, newROMID string) (string, error)
}

// API is what the HTTP layer uses.
type API interface {
	Create(ctx context.Context, requester identity.Requester, romID string) (LinkDTO, error)
	Preview(ctx context.Context, requester identity.Requester, token string) (PreviewDTO, error)
	Cover(ctx context.Context, requester identity.Requester, token string) (rom.StoredContent, error)
	Redeem(ctx context.Context, requester identity.Requester, token string) (RedeemedDTO, error)
}

// LinkDTO is a new share link, relative to the server's address.
type LinkDTO struct {
	URL string `json:"url"`
}

// PreviewDTO shows what a link offers before it is redeemed.
type PreviewDTO struct {
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platform_name"`
	DiscCount    int    `json:"disc_count"`
	HasCover     bool   `json:"has_cover"`
	// AlreadyInLibrary says the requester has the ROM, so redeeming would be refused.
	AlreadyInLibrary bool `json:"already_in_library"`
}

// RedeemedDTO names the requester's new copy.
type RedeemedDTO struct {
	ROMID string `json:"rom_id"`
}

type Service struct {
	repository Repository
	storage    rom.Storage
}

var _ API = (*Service)(nil)

func NewService(repository Repository, storage rom.Storage) *Service {
	return &Service{repository: repository, storage: storage}
}

// Create makes a new link to a ROM the requester uploaded, or to any ROM for an admin.
func (service *Service) Create(ctx context.Context, requester identity.Requester, romID string) (LinkDTO, error) {
	if !isPerson(requester) {
		return LinkDTO{}, fault.Forbidden()
	}
	shared, err := service.repository.FindROMByID(ctx, romID)
	if err != nil {
		return LinkDTO{}, err
	}
	if shared == nil || !(requester.IsAdmin() || requester.Owns(shared.UploaderID)) {
		return LinkDTO{}, fault.NotFound()
	}
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return LinkDTO{}, fault.Internal(fmt.Errorf("generating share token: %w", err))
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	if err := service.repository.CreateShareLink(ctx, hashToken(token), shared.ID, requester.UserID); err != nil {
		return LinkDTO{}, err
	}
	return LinkDTO{URL: linkPath + token}, nil
}

func (service *Service) Preview(ctx context.Context, requester identity.Requester, token string) (PreviewDTO, error) {
	shared, err := service.offered(ctx, requester, token)
	if err != nil {
		return PreviewDTO{}, err
	}
	discs, err := service.repository.FindROMDiscs(ctx, shared.ID)
	if err != nil {
		return PreviewDTO{}, err
	}
	has, err := service.repository.UserHasROMFile(ctx, requester.UserID, firstFile(*shared, discs))
	if err != nil {
		return PreviewDTO{}, err
	}
	return PreviewDTO{
		Title: shared.Identity.Title, Platform: shared.Identity.Platform, PlatformName: romdomain.PlatformName(shared.Identity.Platform),
		DiscCount: max(len(discs), 1), HasCover: coverPath(*shared) != "", AlreadyInLibrary: has,
	}, nil
}

// Cover opens the offered ROM's cover (its small copy when there is one) for the preview.
func (service *Service) Cover(ctx context.Context, requester identity.Requester, token string) (rom.StoredContent, error) {
	shared, err := service.offered(ctx, requester, token)
	if err != nil {
		return rom.StoredContent{}, err
	}
	path := coverPath(*shared)
	if path == "" {
		return rom.StoredContent{}, fault.NotFound()
	}
	return service.storage.Open(ctx, path)
}

// Redeem spends the link, adding a copy of its ROM to the requester's library.
func (service *Service) Redeem(ctx context.Context, requester identity.Requester, token string) (RedeemedDTO, error) {
	if !isPerson(requester) {
		return RedeemedDTO{}, fault.Forbidden()
	}
	newID, err := id.New("rom")
	if err != nil {
		return RedeemedDTO{}, fault.Internal(fmt.Errorf("generating rom id: %w", err))
	}
	romID, err := service.repository.RedeemShareLink(ctx, hashToken(token), requester.UserID, newID)
	if err != nil {
		return RedeemedDTO{}, err
	}
	return RedeemedDTO{ROMID: romID}, nil
}

// offered returns the ROM an unused link offers; anything else is not found.
func (service *Service) offered(ctx context.Context, requester identity.Requester, token string) (*romdomain.ROM, error) {
	if !isPerson(requester) {
		return nil, fault.Forbidden()
	}
	romID, found, err := service.repository.FindUnusedShareLink(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fault.NotFound()
	}
	shared, err := service.repository.FindROMByID(ctx, romID)
	if err != nil {
		return nil, err
	}
	if shared == nil {
		return nil, fault.NotFound()
	}
	return shared, nil
}

// isPerson excludes client tokens: links are for people.
func isPerson(requester identity.Requester) bool {
	return requester.Role == identity.RoleUser || requester.Role == identity.RoleAdmin
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func firstFile(shared romdomain.ROM, discs []romdomain.ROMDisc) string {
	if len(discs) > 0 {
		return discs[0].ROMFileID
	}
	return shared.ROMFileID
}

func coverPath(shared romdomain.ROM) string {
	for _, path := range []*string{shared.Identity.CoverThumbPath, shared.Identity.CoverArtPath} {
		if path != nil && *path != "" {
			return *path
		}
	}
	return ""
}

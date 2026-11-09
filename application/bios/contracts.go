// Package bios serves BIOS files: shared by every user, listed per platform, downloadable by
// anyone signed in or connected, and changed only by their uploader or an admin.
package bios

import (
	"context"

	"github.com/lcmps/retroxide/application/rom"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// Repository stores BIOS entries.
type Repository interface {
	ListBIOS(ctx context.Context, platform *string) ([]biosdomain.BIOS, error)
	FindBIOS(ctx context.Context, id string) (*biosdomain.BIOS, error)
	ListUnrecognizedBIOS(ctx context.Context) ([]biosdomain.BIOS, error)
	UpdateBIOS(ctx context.Context, entry biosdomain.BIOS) error
	// HardDeleteBIOS deletes the entry, and its stored file unless a game still uses it.
	HardDeleteBIOS(ctx context.Context, id string, deletePath func(string) error) error
	FindROMFileByID(ctx context.Context, id string) (*romdomain.ROMFile, error)
}

// KnownList recognizes BIOS files by checksum.
type KnownList interface {
	Lookup(sha1 string, size uint64) (biosdomain.Known, bool)
}

// API is what the HTTP layer uses.
type API interface {
	List(ctx context.Context, requester identity.Requester, platform *string) ([]DTO, error)
	Get(ctx context.Context, requester identity.Requester, id string) (DTO, error)
	Download(ctx context.Context, requester identity.Requester, id string) (rom.DownloadOutput, error)
	DownloadFilename(ctx context.Context, requester identity.Requester, id string) (string, error)
	Label(ctx context.Context, requester identity.Requester, id string, input LabelInput) (DTO, error)
	Delete(ctx context.Context, requester identity.Requester, id string) error
	// RecheckUnrecognized looks unrecognized entries up again, after the known list changed, and
	// returns how many it recognized.
	RecheckUnrecognized(ctx context.Context) (int, error)
}

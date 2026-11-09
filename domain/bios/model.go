// Package bios describes BIOS and firmware files: shared by every user, listed per platform, and
// recognized by checksum against a list of known files.
package bios

import (
	"path"
	"time"

	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// Labels describe which BIOS a file is. A field nobody could determine is nil, never a guess.
type Labels struct {
	Region  *string
	Version *string
	Model   *string
	// Date is the BIOS build date as YYYY-MM-DD.
	Date *string
	// Note is anything else worth showing, such as "Debug" or the file's role ("Boot ROM").
	Note *string
}

// Known is a BIOS file from the known list: its platform, the path emulators expect it at, and
// the checksum and size that recognize it.
type Known struct {
	Platform string
	// Path is the name the known list gives the file, such as pcsx2/bios/ps2-0160e-20010704.bin;
	// emulators look for the file there, relative to their system folder.
	Path string
	Size uint64
	// SHA1 is lowercase hex.
	SHA1   string
	Labels Labels
}

// BIOS is a stored BIOS file. Every user can see and download it; only its uploader or an admin
// can change or delete it.
type BIOS struct {
	ID         string
	ROMFileID  string
	UploaderID string
	Platform   string
	// Filename is the name the file was uploaded with.
	Filename string
	// ExpectedPath is where emulators look for the file, known only for recognized files.
	ExpectedPath *string
	Recognized   bool
	Labels       Labels
	CreatedAt    time.Time
	// Hash and Size describe the stored content. Reads fill them in; writes ignore them.
	Hash romdomain.ROMHash
	Size uint64
}

// CanModify reports whether requester may label or delete entry: an admin, or the user who
// uploaded it. Client tokens only read BIOS files.
func CanModify(requester identity.Requester, entry BIOS) bool {
	switch requester.Role {
	case identity.RoleAdmin:
		return true
	case identity.RoleUser:
		return requester.Owns(entry.UploaderID)
	default:
		return false
	}
}

// DownloadName is the filename a download of entry gets: the one emulators expect when the file
// was recognized, otherwise the name it was uploaded with.
func (entry BIOS) DownloadName() string {
	if entry.ExpectedPath != nil {
		return path.Base(*entry.ExpectedPath)
	}
	return entry.Filename
}

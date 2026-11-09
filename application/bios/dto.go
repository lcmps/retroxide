package bios

import (
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// DTO is a BIOS file as the API shows it.
type DTO struct {
	ID           string `json:"bios_id"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platform_name"`
	Filename     string `json:"filename"`
	// ExpectedPath is where emulators look for the file, relative to their system folder; for a
	// file the known list did not recognize, the name it was uploaded with.
	ExpectedPath string  `json:"expected_path"`
	Recognized   bool    `json:"recognized"`
	Region       *string `json:"region"`
	Version      *string `json:"version"`
	Model        *string `json:"model"`
	Date         *string `json:"date"`
	Note         *string `json:"note"`
	Size         uint64  `json:"size"`
	SHA256       string  `json:"sha256"`
	SHA1         string  `json:"sha1"`
	MD5          string  `json:"md5"`
	CRC32        string  `json:"crc32"`
	// CanModify tells the requester whether they may label or delete the file.
	CanModify bool `json:"can_modify"`
}

// LabelInput sets the labels of a BIOS file the known list did not recognize. Omitted fields are
// cleared.
type LabelInput struct {
	Region  *string `json:"region"`
	Version *string `json:"version"`
	Model   *string `json:"model"`
	Date    *string `json:"date"`
	Note    *string `json:"note"`
}

// ListDTO wraps a listing.
type ListDTO struct {
	Items []DTO `json:"items"`
}

func toDTO(requester identity.Requester, entry biosdomain.BIOS) DTO {
	expected := entry.Filename
	if entry.ExpectedPath != nil {
		expected = *entry.ExpectedPath
	}
	return DTO{
		ID: entry.ID, Platform: entry.Platform, PlatformName: romdomain.PlatformName(entry.Platform),
		Filename: entry.Filename, ExpectedPath: expected, Recognized: entry.Recognized,
		Region: entry.Labels.Region, Version: entry.Labels.Version, Model: entry.Labels.Model,
		Date: entry.Labels.Date, Note: entry.Labels.Note, Size: entry.Size,
		SHA256: entry.Hash.SHA256, SHA1: entry.Hash.SHA1, MD5: entry.Hash.MD5, CRC32: entry.Hash.CRC32,
		CanModify: biosdomain.CanModify(requester, entry),
	}
}

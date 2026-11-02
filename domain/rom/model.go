package rom

import (
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrInvalidROMHash = errors.New("domain: invalid rom hash")

type ROMHash struct {
	SHA256 string
	SHA1   string
	MD5    string
	CRC32  string
}

func NewROMHash(sha256, sha1, md5, crc32 string) (ROMHash, error) {
	for _, hash := range []struct {
		name  string
		value string
		size  int
	}{
		{name: "sha256", value: sha256, size: 32},
		{name: "sha1", value: sha1, size: 20},
		{name: "md5", value: md5, size: 16},
		{name: "crc32", value: crc32, size: 4},
	} {
		decoded, err := hex.DecodeString(hash.value)
		if err != nil || len(decoded) != hash.size {
			return ROMHash{}, fmt.Errorf("%w: %s", ErrInvalidROMHash, hash.name)
		}
	}

	return ROMHash{SHA256: sha256, SHA1: sha1, MD5: md5, CRC32: crc32}, nil
}

const (
	IdentifiedByFilename = "retroxide_filename_v1"
	IdentifiedByManual   = "manual"
)

type ROMIdentity struct {
	Title           string
	Platform        string
	Region          *string
	Version         *string
	Publisher       *string
	ReleaseYear     *string
	Description     *string
	CoverArtPath    *string
	TitleScreenPath *string
	ScreenshotPath  *string
	IdentifiedBy    string
	Confidence      uint8
}

type ROM struct {
	ID         string
	ROMFileID  string
	UploaderID string
	Identity   ROMIdentity
}

type ROMFile struct {
	ID          string
	Hash        ROMHash
	Size        uint64
	StoragePath string
}

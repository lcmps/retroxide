package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"hash/crc32"
	"io"
)

type Service struct{}

func New() Service {
	return Service{}
}

func (Service) Hash(content []byte) (romdomain.ROMHash, error) {
	sha256Hash := sha256.New()
	sha1Hash := sha1.New()
	md5Hash := md5.New()
	crc32Hash := crc32.NewIEEE()
	if _, err := io.MultiWriter(sha256Hash, sha1Hash, md5Hash, crc32Hash).Write(content); err != nil {
		return romdomain.ROMHash{}, fmt.Errorf("hashing rom content: %w", err)
	}

	return romdomain.NewROMHash(
		hex.EncodeToString(sha256Hash.Sum(nil)),
		hex.EncodeToString(sha1Hash.Sum(nil)),
		hex.EncodeToString(md5Hash.Sum(nil)),
		fmt.Sprintf("%08x", crc32Hash.Sum32()),
	)
}

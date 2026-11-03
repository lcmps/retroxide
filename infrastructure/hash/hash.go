package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhash "hash"
	"hash/crc32"
	"io"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// Service computes the SHA-256, SHA-1, MD5, and CRC32 bundle that identifies ROM content.
type Service struct{}

func New() Service {
	return Service{}
}

// NewDigest returns a streaming digest computing every hash in a single pass over the content,
// each hash on its own goroutine.
func (Service) NewDigest() romdomain.Digest {
	return newParallelDigest()
}

// ResumeDigest continues hashing from progress saved with MarshalBinary, or starts afresh when
// state is empty.
func (Service) ResumeDigest(state []byte) (romdomain.ResumableDigest, error) {
	if len(state) == 0 {
		return newParallelDigest(), nil
	}
	return restoreParallelDigest(state)
}

// Hash hashes content held in memory. Content smaller than one block is hashed inline, where
// handing it to goroutines would cost more than it saves.
func (Service) Hash(content []byte) (romdomain.ROMHash, error) {
	var digest romdomain.Digest = newParallelDigest()
	if len(content) < blockSize {
		digest = newSequentialDigest()
	}
	if _, err := digest.Write(content); err != nil {
		return romdomain.ROMHash{}, fmt.Errorf("hashing rom content: %w", err)
	}
	return digest.Sum()
}

func newSequentialDigest() *digest {
	digest := &digest{sha256: sha256.New(), sha1: sha1.New(), md5: md5.New(), crc32: crc32.NewIEEE()}
	digest.Writer = io.MultiWriter(digest.sha256, digest.sha1, digest.md5, digest.crc32)
	return digest
}

type digest struct {
	io.Writer
	sha256, sha1, md5 stdhash.Hash
	crc32             stdhash.Hash32
}

func (digest *digest) Sum() (romdomain.ROMHash, error) {
	return romdomain.NewROMHash(
		hex.EncodeToString(digest.sha256.Sum(nil)),
		hex.EncodeToString(digest.sha1.Sum(nil)),
		hex.EncodeToString(digest.md5.Sum(nil)),
		fmt.Sprintf("%08x", digest.crc32.Sum32()),
	)
}

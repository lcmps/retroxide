package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	stdhash "hash"
	"hash/crc32"
	"sync/atomic"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const (
	// blockSize is how much content each hash receives at a time: large enough that handing a
	// block to the hashes costs little next to hashing it.
	blockSize = 1 << 20
	// blocksInFlight bounds the memory a digest holds while slower hashes catch up.
	blocksInFlight = 4
)

// parallelDigest computes SHA-256, SHA-1, MD5, and CRC32 concurrently. Run one after another the
// hashes cost the sum of their times; on their own goroutines, only the slowest (MD5) counts.
//
// Writes are copied into blocks, and every full block is handed to the four hashes. Each hash
// consumes blocks in order: the goroutine for a block waits until that hash finished the previous
// one. Goroutines exit as soon as their block is hashed, so a digest abandoned mid-stream (a
// failed upload) leaks nothing.
type parallelDigest struct {
	hashes [4]stdhash.Hash
	// done[i] is closed once hash i has consumed every block handed out so far.
	done      [4]chan struct{}
	free      chan []byte
	allocated int
	current   []byte
}

func newParallelDigest() *parallelDigest {
	return &parallelDigest{
		hashes: [4]stdhash.Hash{sha256.New(), sha1.New(), md5.New(), crc32.NewIEEE()},
		free:   make(chan []byte, blocksInFlight),
	}
}

func (digest *parallelDigest) Write(content []byte) (int, error) {
	written := len(content)
	for len(content) > 0 {
		if digest.current == nil {
			digest.current = digest.takeBlock()
		}
		copied := copy(digest.current[len(digest.current):cap(digest.current)], content)
		digest.current = digest.current[:len(digest.current)+copied]
		content = content[copied:]
		if len(digest.current) == cap(digest.current) {
			digest.dispatch()
		}
	}
	return written, nil
}

// takeBlock returns an empty block, waiting for the hashes to release one once blocksInFlight
// are in use.
func (digest *parallelDigest) takeBlock() []byte {
	if digest.allocated < blocksInFlight {
		digest.allocated++
		return make([]byte, 0, blockSize)
	}
	return (<-digest.free)[:0]
}

// dispatch hands the current block to every hash; the last one to finish releases it.
func (digest *parallelDigest) dispatch() {
	block := digest.current
	digest.current = nil
	var remaining atomic.Int32
	remaining.Store(int32(len(digest.hashes)))
	for index, hash := range digest.hashes {
		previous, next := digest.done[index], make(chan struct{})
		digest.done[index] = next
		go func() {
			defer close(next)
			if previous != nil {
				<-previous
			}
			_, _ = hash.Write(block) // hash.Hash writes never fail
			if remaining.Add(-1) == 0 {
				digest.free <- block
			}
		}()
	}
}

// wait hashes any content still buffered and waits until every hash has consumed it.
func (digest *parallelDigest) wait() {
	if len(digest.current) > 0 {
		digest.dispatch()
	}
	for _, done := range digest.done {
		if done != nil {
			<-done
		}
	}
}

// digestStateVersion prefixes saved progress, so a format change is detected, not misread.
const digestStateVersion = 1

// MarshalBinary saves the progress of all four hashes; the digest stays usable.
func (digest *parallelDigest) MarshalBinary() ([]byte, error) {
	digest.wait()
	state := []byte{digestStateVersion}
	for _, hash := range digest.hashes {
		saved, err := hash.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("saving hash progress: %w", err)
		}
		state = binary.AppendUvarint(state, uint64(len(saved)))
		state = append(state, saved...)
	}
	return state, nil
}

// restoreParallelDigest continues a digest from MarshalBinary's output.
func restoreParallelDigest(state []byte) (*parallelDigest, error) {
	digest := newParallelDigest()
	if len(state) == 0 || state[0] != digestStateVersion {
		return nil, errors.New("hash progress has an unknown format")
	}
	rest := state[1:]
	for _, hash := range digest.hashes {
		length, read := binary.Uvarint(rest)
		if read <= 0 || uint64(len(rest)-read) < length {
			return nil, errors.New("hash progress is truncated")
		}
		if err := hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(rest[read : read+int(length)]); err != nil {
			return nil, fmt.Errorf("restoring hash progress: %w", err)
		}
		rest = rest[read+int(length):]
	}
	if len(rest) != 0 {
		return nil, errors.New("hash progress has trailing data")
	}
	return digest, nil
}

// Sum hashes any remaining content and returns the bundle; the digest is unusable afterwards.
func (digest *parallelDigest) Sum() (romdomain.ROMHash, error) {
	digest.wait()
	return romdomain.NewROMHash(
		hex.EncodeToString(digest.hashes[0].Sum(nil)),
		hex.EncodeToString(digest.hashes[1].Sum(nil)),
		hex.EncodeToString(digest.hashes[2].Sum(nil)),
		fmt.Sprintf("%08x", digest.hashes[3].(stdhash.Hash32).Sum32()),
	)
}

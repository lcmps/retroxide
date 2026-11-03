package rom

import "io"

// Digest computes a ROMHash incrementally as content is written to it, so large ROMs can be
// hashed while they stream to storage.
type Digest interface {
	io.Writer
	Sum() (ROMHash, error)
}

// ResumableDigest is a Digest whose progress can be saved, so hashing an upload sent in parts
// can continue in a later request, or after a restart, where it left off.
type ResumableDigest interface {
	Digest
	// MarshalBinary returns the progress so far; the digest stays usable.
	MarshalBinary() ([]byte, error)
}

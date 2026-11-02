package rom

import "io"

// Digest computes a ROMHash incrementally as content is written to it, so large ROMs can be
// hashed while they stream to storage.
type Digest interface {
	io.Writer
	Sum() (ROMHash, error)
}

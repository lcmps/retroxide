package chd

// bitReader reads big-endian bit fields from a byte slice, as MAME's bitstream does: reading
// past the end yields zero bits and is reported by overflowed.
type bitReader struct {
	data   []byte
	offset int    // next byte to load
	buffer uint32 // bits not yet consumed, left-aligned
	bits   int    // how many bits of buffer are valid
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

// peek returns the next n (at most 32) bits without consuming them.
func (reader *bitReader) peek(n int) uint32 {
	if n == 0 {
		return 0
	}
	if n > reader.bits {
		for reader.bits <= 24 {
			if reader.offset < len(reader.data) {
				reader.buffer |= uint32(reader.data[reader.offset]) << (24 - reader.bits)
			}
			reader.offset++
			reader.bits += 8
		}
	}
	return reader.buffer >> (32 - n)
}

func (reader *bitReader) remove(n int) {
	if n >= 32 {
		reader.buffer = 0
	} else {
		reader.buffer <<= n
	}
	reader.bits -= n
}

func (reader *bitReader) read(n int) uint32 {
	value := reader.peek(n)
	reader.remove(n)
	return value
}

// overflowed reports whether more bits were consumed than the data holds.
func (reader *bitReader) overflowed() bool {
	return reader.offset-reader.bits/8 > len(reader.data)
}

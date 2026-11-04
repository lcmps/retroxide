package chd

import "errors"

var errHuffman = errors.New("chd: invalid huffman data")

// huffmanDecoder decodes MAME's canonical Huffman codes with a full lookup table.
type huffmanDecoder struct {
	maxBits int
	lengths []uint8
	codes   []uint32
	// lookup maps every maxBits-bit window to (symbol << 5) | code length.
	lookup []uint32
}

func newHuffmanDecoder(numCodes, maxBits int) *huffmanDecoder {
	return &huffmanDecoder{maxBits: maxBits, lengths: make([]uint8, numCodes), codes: make([]uint32, numCodes)}
}

func (decoder *huffmanDecoder) decode(reader *bitReader) uint32 {
	entry := decoder.lookup[reader.peek(decoder.maxBits)]
	reader.remove(int(entry & 0x1f))
	return entry >> 5
}

// importTreeRLE reads code lengths stored with a simple run-length scheme (used by the map).
func (decoder *huffmanDecoder) importTreeRLE(reader *bitReader) error {
	numBits := 3
	switch {
	case decoder.maxBits >= 16:
		numBits = 5
	case decoder.maxBits >= 8:
		numBits = 4
	}
	for node := 0; node < len(decoder.lengths); {
		bits := reader.read(numBits)
		if bits != 1 {
			decoder.lengths[node] = uint8(bits)
			node++
			continue
		}
		bits = reader.read(numBits)
		if bits == 1 {
			decoder.lengths[node] = 1
			node++
			continue
		}
		repeat := int(reader.read(numBits)) + 3
		if node+repeat > len(decoder.lengths) {
			return errHuffman
		}
		for ; repeat > 0; repeat-- {
			decoder.lengths[node] = uint8(bits)
			node++
		}
	}
	if err := decoder.build(); err != nil {
		return err
	}
	if reader.overflowed() {
		return errHuffman
	}
	return nil
}

// importTreeHuffman reads code lengths that are themselves Huffman-coded (used by the huff
// codec).
func (decoder *huffmanDecoder) importTreeHuffman(reader *bitReader) error {
	small := newHuffmanDecoder(24, 6)
	small.lengths[0] = uint8(reader.read(3))
	start := int(reader.read(3)) + 1
	count := uint32(0)
	for index := 1; index < 24; index++ {
		if index < start || count == 7 {
			small.lengths[index] = 0
			continue
		}
		count = reader.read(3)
		if count == 7 {
			small.lengths[index] = 0
		} else {
			small.lengths[index] = uint8(count)
		}
	}
	if err := small.build(); err != nil {
		return err
	}
	fullBits := 0
	for temp := len(decoder.lengths) - 9; temp != 0; temp >>= 1 {
		fullBits++
	}
	last := uint8(0)
	for code := 0; code < len(decoder.lengths); {
		value := small.decode(reader)
		if value != 0 {
			last = uint8(value - 1)
			decoder.lengths[code] = last
			code++
			continue
		}
		repeat := int(reader.read(3)) + 2
		if repeat == 7+2 {
			repeat += int(reader.read(fullBits))
		}
		for ; repeat != 0 && code < len(decoder.lengths); repeat-- {
			decoder.lengths[code] = last
			code++
		}
	}
	if err := decoder.build(); err != nil {
		return err
	}
	if reader.overflowed() {
		return errHuffman
	}
	return nil
}

// build assigns canonical codes to the code lengths and fills the lookup table.
func (decoder *huffmanDecoder) build() error {
	var histogram [33]uint32
	for _, length := range decoder.lengths {
		if int(length) > decoder.maxBits {
			return errHuffman
		}
		histogram[length]++
	}
	start := uint32(0)
	for length := 32; length > 0; length-- {
		next := (start + histogram[length]) >> 1
		if length != 1 && next*2 != start+histogram[length] {
			return errHuffman
		}
		histogram[length] = start
		start = next
	}
	for symbol, length := range decoder.lengths {
		if length > 0 {
			decoder.codes[symbol] = histogram[length]
			histogram[length]++
		}
	}
	decoder.lookup = make([]uint32, 1<<decoder.maxBits)
	for symbol, length := range decoder.lengths {
		if length == 0 {
			continue
		}
		shift := decoder.maxBits - int(length)
		first := decoder.codes[symbol] << shift
		last := ((decoder.codes[symbol] + 1) << shift) - 1
		if int(last) >= len(decoder.lookup) || last < first {
			return errHuffman
		}
		value := uint32(symbol)<<5 | uint32(length)
		for index := first; index <= last; index++ {
			decoder.lookup[index] = value
		}
	}
	return nil
}

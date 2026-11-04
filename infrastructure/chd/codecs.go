package chd

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz/lzma"
)

// Codec tags, as stored in the header.
const (
	codecZlib    = 'z'<<24 | 'l'<<16 | 'i'<<8 | 'b'
	codecLZMA    = 'l'<<24 | 'z'<<16 | 'm'<<8 | 'a'
	codecHuffman = 'h'<<24 | 'u'<<16 | 'f'<<8 | 'f'
	codecFLAC    = 'f'<<24 | 'l'<<16 | 'a'<<8 | 'c'
	codecZstd    = 'z'<<24 | 's'<<16 | 't'<<8 | 'd'
	codecCDZlib  = 'c'<<24 | 'd'<<16 | 'z'<<8 | 'l'
	codecCDLZMA  = 'c'<<24 | 'd'<<16 | 'l'<<8 | 'z'
	codecCDFLAC  = 'c'<<24 | 'd'<<16 | 'f'<<8 | 'l'
	codecCDZstd  = 'c'<<24 | 'd'<<16 | 'z'<<8 | 's'
)

var errDecompress = errors.New("chd: hunk does not decompress")

// codec decompresses one hunk's worth of data into dest, which it fills exactly.
type codec interface {
	decompress(src, dest []byte) error
}

func newCodec(tag uint32) (codec, error) {
	switch tag {
	case codecZlib:
		return deflateCodec{}, nil
	case codecLZMA:
		return lzmaCodec{}, nil
	case codecZstd:
		return newZstdCodec()
	case codecHuffman:
		return huffmanCodec{}, nil
	case codecFLAC:
		return flacCodec{}, nil
	case codecCDZlib:
		return cdCodec{base: deflateCodec{}, subcode: deflateCodec{}}, nil
	case codecCDLZMA:
		return cdCodec{base: lzmaCodec{}, subcode: deflateCodec{}}, nil
	case codecCDZstd:
		// Unlike the others, the zstd CD codec compresses the subcode with zstd too.
		base, err := newZstdCodec()
		return cdCodec{base: base, subcode: base}, err
	case codecCDFLAC:
		return cdFLACCodec{}, nil
	default:
		return nil, fmt.Errorf("%w: codec %q", ErrUnsupported, tagString(tag))
	}
}

func tagString(tag uint32) string {
	var name [4]byte
	binary.BigEndian.PutUint32(name[:], tag)
	return string(name[:])
}

// deflateCodec reads raw deflate streams (zlib without its header).
type deflateCodec struct{}

func (deflateCodec) decompress(src, dest []byte) error {
	reader := flate.NewReader(bytes.NewReader(src))
	defer reader.Close()
	if _, err := io.ReadFull(reader, dest); err != nil {
		return errDecompress
	}
	return nil
}

// lzmaCodec reads raw LZMA streams with MAME's fixed properties (lc=3, lp=0, pb=2) and no end
// marker; each hunk is an independent stream of known size.
type lzmaCodec struct{}

func (lzmaCodec) decompress(src, dest []byte) error {
	// Give the decoder the classic 13-byte header the stream lacks: properties, dictionary size
	// (the hunk is the whole window), and the exact uncompressed size.
	header := make([]byte, 13)
	header[0] = 93
	binary.LittleEndian.PutUint32(header[1:], uint32(max(len(dest), 4096)))
	binary.LittleEndian.PutUint64(header[5:], uint64(len(dest)))
	reader, err := lzma.NewReader(io.MultiReader(bytes.NewReader(header), bytes.NewReader(src)))
	if err != nil {
		return errDecompress
	}
	if _, err := io.ReadFull(reader, dest); err != nil {
		return errDecompress
	}
	return nil
}

type zstdCodec struct{ decoder *zstd.Decoder }

func newZstdCodec() (zstdCodec, error) {
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	return zstdCodec{decoder: decoder}, err
}

func (codec zstdCodec) decompress(src, dest []byte) error {
	out, err := codec.decoder.DecodeAll(src, dest[:0])
	if err != nil || len(out) != len(dest) {
		return errDecompress
	}
	if &out[0] != &dest[0] { // grew past dest's capacity
		copy(dest, out)
	}
	return nil
}

// huffmanCodec is MAME's 8-bit Huffman coding, with the tree stored before the data.
type huffmanCodec struct{}

func (huffmanCodec) decompress(src, dest []byte) error {
	reader := newBitReader(src)
	decoder := newHuffmanDecoder(256, 16)
	if err := decoder.importTreeHuffman(reader); err != nil {
		return errDecompress
	}
	for index := range dest {
		dest[index] = byte(decoder.decode(reader))
	}
	if reader.overflowed() {
		return errDecompress
	}
	return nil
}

// flacCodec stores 16-bit stereo samples as FLAC; the first byte says which byte order the
// samples are written in.
type flacCodec struct{}

func (flacCodec) decompress(src, dest []byte) error {
	if len(src) == 0 || len(dest)%4 != 0 {
		return errDecompress
	}
	var order binary.ByteOrder
	switch src[0] {
	case 'L':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return errDecompress
	}
	samples := make([]int16, len(dest)/2)
	if _, err := decodeFLAC(src[1:], samples); err != nil {
		return errDecompress
	}
	for index, sample := range samples {
		order.PutUint16(dest[index*2:], uint16(sample))
	}
	return nil
}

// CD geometry: each frame is a 2352-byte sector followed by 96 bytes of subcode.
const (
	sectorBytes  = 2352
	subcodeBytes = 96
	frameBytes   = sectorBytes + subcodeBytes
)

// cdCodec compresses sector data and subcode separately, after stripping sync and ECC from
// sectors whose ECC could be regenerated (flagged in a bitmap before the data).
type cdCodec struct{ base, subcode codec }

func (codec cdCodec) decompress(src, dest []byte) error {
	frames := len(dest) / frameBytes
	header, baseLength, err := cdHeader(src, len(dest))
	if err != nil {
		return err
	}
	sectors := make([]byte, frames*sectorBytes)
	if err := codec.base.decompress(src[header:header+baseLength], sectors); err != nil {
		return err
	}
	subcode := make([]byte, frames*subcodeBytes)
	if err := codec.subcode.decompress(src[header+baseLength:], subcode); err != nil {
		return err
	}
	assembleCDFrames(dest, sectors, subcode, src)
	return nil
}

// cdHeader returns the size of a CD hunk's header (ECC bitmap and base length) and the
// compressed length of its sector data.
func cdHeader(src []byte, destLength int) (int, int, error) {
	frames := destLength / frameBytes
	eccBytes := (frames + 7) / 8
	lengthBytes := 2
	if destLength >= 65536 {
		lengthBytes = 3
	}
	header := eccBytes + lengthBytes
	if len(src) < header {
		return 0, 0, errDecompress
	}
	baseLength := int(src[eccBytes])<<8 | int(src[eccBytes+1])
	if lengthBytes == 3 {
		baseLength = baseLength<<8 | int(src[eccBytes+2])
	}
	if len(src) < header+baseLength {
		return 0, 0, errDecompress
	}
	return header, baseLength, nil
}

// assembleCDFrames interleaves sectors and subcode into frames, restoring the sync header and
// ECC of sectors the ECC bitmap (at the start of src) marks as stripped.
func assembleCDFrames(dest, sectors, subcode, src []byte) {
	for frame := 0; frame < len(dest)/frameBytes; frame++ {
		sector := dest[frame*frameBytes : frame*frameBytes+sectorBytes]
		copy(sector, sectors[frame*sectorBytes:])
		copy(dest[frame*frameBytes+sectorBytes:(frame+1)*frameBytes], subcode[frame*subcodeBytes:])
		if src[frame/8]&(1<<(frame%8)) != 0 {
			copy(sector, syncHeader[:])
			generateECC(sector)
		}
	}
}

// cdFLACCodec stores CD audio as FLAC (samples big-endian, as CHD keeps audio) with the
// subcode deflated after it.
type cdFLACCodec struct{}

func (cdFLACCodec) decompress(src, dest []byte) error {
	frames := len(dest) / frameBytes
	samples := make([]int16, frames*sectorBytes/2)
	used, err := decodeFLAC(src, samples)
	if err != nil {
		return errDecompress
	}
	sectors := make([]byte, frames*sectorBytes)
	for index, sample := range samples {
		binary.BigEndian.PutUint16(sectors[index*2:], uint16(sample))
	}
	subcode := make([]byte, frames*subcodeBytes)
	if err := (deflateCodec{}).decompress(src[used:], subcode); err != nil {
		return err
	}
	for frame := 0; frame < frames; frame++ {
		copy(dest[frame*frameBytes:], sectors[frame*sectorBytes:(frame+1)*sectorBytes])
		copy(dest[frame*frameBytes+sectorBytes:(frame+1)*frameBytes], subcode[frame*subcodeBytes:])
	}
	return nil
}

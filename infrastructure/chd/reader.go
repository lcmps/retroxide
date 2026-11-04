// Package chd reads MAME CHD (Compressed Hunks of Data) version 5 images, the format
// emulators use for compressed CD, GD-ROM, and DVD dumps, and extracts CD tracks exactly as
// chdman extractcd does, so their hashes match the disc databases.
//
// The format handling is ported from libchdr (BSD-3-Clause, Copyright Aaron Giles, Romain
// Tisserand, and contributors) and MAME's cdrom.cpp and chdman.cpp (BSD-3-Clause).
package chd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrNotCHD reports content that is not a CHD image.
	ErrNotCHD = errors.New("chd: not a CHD image")
	// ErrUnsupported reports a valid CHD this reader cannot read: an older version, one that
	// depends on a parent image, or an unsupported codec.
	ErrUnsupported = errors.New("chd: unsupported CHD")
	// ErrCorrupt reports a CHD whose structure or data fails its checks.
	ErrCorrupt = errors.New("chd: corrupt CHD")
)

const headerBytes = 124

// Map entry kinds.
const (
	kindCodec0 = iota
	kindCodec1
	kindCodec2
	kindCodec3
	kindNone
	kindSelf
	kindParent
	kindRLESmall
	kindRLELarge
	kindSelf0
	kindSelf1
	kindParentSelf
	kindParent0
	kindParent1
)

// Header is a CHD's fixed header.
type Header struct {
	Compressors  [4]uint32
	LogicalBytes uint64
	HunkBytes    uint32
	UnitBytes    uint32
	HunkCount    uint32
	// RawSHA1 covers the uncompressed data; SHA1 covers the data and its metadata. chdman
	// leaves both zero for uncompressed images.
	RawSHA1, SHA1 [20]byte

	mapOffset, metaOffset uint64
}

type mapEntry struct {
	kind   uint8
	length uint32
	offset uint64
	crc    uint16
}

// Reader reads a CHD's hunks and metadata. Its methods are safe for concurrent use when the
// source's ReadAt is, as it is for files.
type Reader struct {
	source  io.ReaderAt
	size    int64
	header  Header
	entries []mapEntry
	codecs  [4]codec
}

// Open reads the header and map of the CHD in source, which is size bytes long.
func Open(source io.ReaderAt, size int64) (*Reader, error) {
	raw := make([]byte, headerBytes)
	if _, err := source.ReadAt(raw, 0); err != nil || !bytes.Equal(raw[:8], []byte("MComprHD")) {
		return nil, ErrNotCHD
	}
	length, version := binary.BigEndian.Uint32(raw[8:]), binary.BigEndian.Uint32(raw[12:])
	if version != 5 {
		return nil, fmt.Errorf("%w: version %d (only version 5 is read; convert it with chdman copy)", ErrUnsupported, version)
	}
	if length != headerBytes {
		return nil, ErrCorrupt
	}
	header := Header{
		LogicalBytes: binary.BigEndian.Uint64(raw[32:]),
		mapOffset:    binary.BigEndian.Uint64(raw[40:]),
		metaOffset:   binary.BigEndian.Uint64(raw[48:]),
		HunkBytes:    binary.BigEndian.Uint32(raw[56:]),
		UnitBytes:    binary.BigEndian.Uint32(raw[60:]),
	}
	for index := range header.Compressors {
		header.Compressors[index] = binary.BigEndian.Uint32(raw[16+4*index:])
	}
	copy(header.RawSHA1[:], raw[64:84])
	copy(header.SHA1[:], raw[84:104])
	if !bytes.Equal(raw[104:124], make([]byte, 20)) {
		return nil, fmt.Errorf("%w: it depends on a parent CHD", ErrUnsupported)
	}
	if header.HunkBytes == 0 || header.HunkBytes > 16<<20 || header.UnitBytes == 0 || header.HunkBytes%header.UnitBytes != 0 {
		return nil, ErrCorrupt
	}
	hunks := (header.LogicalBytes + uint64(header.HunkBytes) - 1) / uint64(header.HunkBytes)
	if hunks > 1<<26 {
		return nil, ErrCorrupt
	}
	header.HunkCount = uint32(hunks)

	reader := &Reader{source: source, size: size, header: header}
	for index, tag := range header.Compressors {
		if tag == 0 {
			continue
		}
		codec, err := newCodec(tag)
		if err != nil {
			return nil, err
		}
		reader.codecs[index] = codec
	}
	var err error
	if header.Compressors[0] == 0 {
		err = reader.readRawMap()
	} else {
		err = reader.readCompressedMap()
	}
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// Header returns the CHD's header.
func (reader *Reader) Header() Header { return reader.header }

// readRawMap reads the map of an uncompressed CHD: one 32-bit hunk position per hunk.
func (reader *Reader) readRawMap() error {
	raw := make([]byte, 4*int(reader.header.HunkCount))
	if _, err := reader.source.ReadAt(raw, int64(reader.header.mapOffset)); err != nil {
		return ErrCorrupt
	}
	reader.entries = make([]mapEntry, reader.header.HunkCount)
	for index := range reader.entries {
		position := uint64(binary.BigEndian.Uint32(raw[4*index:])) * uint64(reader.header.HunkBytes)
		reader.entries[index] = mapEntry{kind: kindNone, offset: position, length: reader.header.HunkBytes}
	}
	return nil
}

// readCompressedMap decodes a v5 compressed map: Huffman-coded entry kinds with run lengths,
// then each entry's length, CRC, or reference, checked against the map's CRC.
func (reader *Reader) readCompressedMap() error {
	head := make([]byte, 16)
	if _, err := reader.source.ReadAt(head, int64(reader.header.mapOffset)); err != nil {
		return ErrCorrupt
	}
	mapBytes := binary.BigEndian.Uint32(head[0:])
	firstOffset := uint64(head[4])<<40 | uint64(head[5])<<32 | uint64(binary.BigEndian.Uint32(head[6:]))
	mapCRC := binary.BigEndian.Uint16(head[10:])
	lengthBits, selfBits, parentBits := int(head[12]), int(head[13]), int(head[14])
	if lengthBits > 32 || selfBits > 32 || parentBits > 32 || int64(reader.header.mapOffset)+16+int64(mapBytes) > reader.size {
		return ErrCorrupt
	}
	compressed := make([]byte, mapBytes)
	if _, err := reader.source.ReadAt(compressed, int64(reader.header.mapOffset)+16); err != nil {
		return ErrCorrupt
	}
	bitsIn := newBitReader(compressed)
	decoder := newHuffmanDecoder(16, 8)
	if err := decoder.importTreeRLE(bitsIn); err != nil {
		return ErrCorrupt
	}

	count := int(reader.header.HunkCount)
	kinds := make([]uint8, count)
	repeat, last := 0, uint8(0)
	for index := range kinds {
		if repeat > 0 {
			kinds[index] = last
			repeat--
			continue
		}
		if bitsIn.overflowed() {
			return ErrCorrupt
		}
		switch value := uint8(decoder.decode(bitsIn)); value {
		case kindRLESmall:
			kinds[index] = last
			repeat = 2 + int(decoder.decode(bitsIn))
		case kindRLELarge:
			kinds[index] = last
			repeat = 2 + 16 + int(decoder.decode(bitsIn))<<4
			repeat += int(decoder.decode(bitsIn))
		default:
			kinds[index] = value
			last = value
		}
	}

	reader.entries = make([]mapEntry, count)
	raw := make([]byte, 12*count) // the map as libchdr lays it out, for the CRC check
	offset := firstOffset
	var lastSelf, lastParent uint64
	for index, kind := range kinds {
		entry := mapEntry{kind: kind}
		switch kind {
		case kindCodec0, kindCodec1, kindCodec2, kindCodec3:
			entry.offset = offset
			entry.length = bitsIn.read(lengthBits)
			entry.crc = uint16(bitsIn.read(16))
			offset += uint64(entry.length)
		case kindNone:
			entry.offset = offset
			entry.length = reader.header.HunkBytes
			entry.crc = uint16(bitsIn.read(16))
			offset += uint64(entry.length)
		case kindSelf:
			lastSelf = uint64(bitsIn.read(selfBits))
			entry.offset = lastSelf
		case kindParent:
			lastParent = uint64(bitsIn.read(parentBits))
			entry.offset = lastParent
		case kindSelf1:
			lastSelf++
			fallthrough
		case kindSelf0:
			entry.kind, entry.offset = kindSelf, lastSelf
		case kindParentSelf:
			lastParent = uint64(index) * uint64(reader.header.HunkBytes) / uint64(reader.header.UnitBytes)
			entry.kind, entry.offset = kindParent, lastParent
		case kindParent1:
			lastParent += uint64(reader.header.HunkBytes / reader.header.UnitBytes)
			fallthrough
		case kindParent0:
			entry.kind, entry.offset = kindParent, lastParent
		default:
			return ErrCorrupt
		}
		reader.entries[index] = entry
		row := raw[12*index:]
		row[0] = entry.kind
		row[1], row[2], row[3] = byte(entry.length>>16), byte(entry.length>>8), byte(entry.length)
		row[4], row[5] = byte(entry.offset>>40), byte(entry.offset>>32)
		binary.BigEndian.PutUint32(row[6:], uint32(entry.offset))
		binary.BigEndian.PutUint16(row[10:], entry.crc)
	}
	if crc16(raw) != mapCRC {
		return ErrCorrupt
	}
	return nil
}

// ReadHunk decompresses hunk index into dest, which must hold HunkBytes, checking its CRC.
func (reader *Reader) ReadHunk(index uint32, dest []byte) error {
	return reader.readHunk(index, dest, 0)
}

func (reader *Reader) readHunk(index uint32, dest []byte, depth int) error {
	if index >= reader.header.HunkCount || len(dest) != int(reader.header.HunkBytes) {
		return ErrCorrupt
	}
	entry := reader.entries[index]
	switch entry.kind {
	case kindCodec0, kindCodec1, kindCodec2, kindCodec3:
		codec := reader.codecs[entry.kind]
		if codec == nil || entry.length > reader.header.HunkBytes*2 {
			return ErrCorrupt
		}
		compressed := make([]byte, entry.length)
		if _, err := reader.source.ReadAt(compressed, int64(entry.offset)); err != nil {
			return ErrCorrupt
		}
		if err := codec.decompress(compressed, dest); err != nil {
			return fmt.Errorf("%w: hunk %d: %v", ErrCorrupt, index, err)
		}
	case kindNone:
		if entry.offset == 0 && reader.header.Compressors[0] == 0 {
			clear(dest) // an uncompressed CHD's unwritten hunk
			return nil
		}
		if _, err := reader.source.ReadAt(dest, int64(entry.offset)); err != nil {
			return ErrCorrupt
		}
		if reader.header.Compressors[0] == 0 {
			return nil // uncompressed CHDs store no hunk CRCs
		}
	case kindSelf:
		// A copy of an earlier hunk. Chains are short; bound them against corrupt loops.
		if depth > 16 || entry.offset >= uint64(index) {
			return ErrCorrupt
		}
		return reader.readHunk(uint32(entry.offset), dest, depth+1)
	default:
		return fmt.Errorf("%w: it depends on a parent CHD", ErrUnsupported)
	}
	if crc16(dest) != entry.crc {
		return fmt.Errorf("%w: hunk %d fails its CRC", ErrCorrupt, index)
	}
	return nil
}

// Metadata returns every metadata entry with tag, in order.
func (reader *Reader) Metadata(tag uint32) ([][]byte, error) {
	var found [][]byte
	offset := reader.header.metaOffset
	for steps := 0; offset != 0; steps++ {
		if steps > 65536 || int64(offset)+16 > reader.size {
			return nil, ErrCorrupt
		}
		head := make([]byte, 16)
		if _, err := reader.source.ReadAt(head, int64(offset)); err != nil {
			return nil, ErrCorrupt
		}
		entryTag := binary.BigEndian.Uint32(head)
		length := binary.BigEndian.Uint32(head[4:]) & 0x00ffffff
		if entryTag == tag {
			data := make([]byte, length)
			if _, err := reader.source.ReadAt(data, int64(offset)+16); err != nil {
				return nil, ErrCorrupt
			}
			found = append(found, data)
		}
		offset = binary.BigEndian.Uint64(head[8:])
	}
	return found, nil
}

// crc16 is CRC-16/CCITT-FALSE, which CHD uses for hunks and its map.
func crc16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc = crc<<8 ^ crc16Table[byte(crc>>8)^b]
	}
	return crc
}

var crc16Table = func() (table [256]uint16) {
	for index := range table {
		crc := uint16(index) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
		table[index] = crc
	}
	return table
}()

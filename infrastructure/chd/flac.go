package chd

import (
	"errors"
	"math/bits"
)

var errFLAC = errors.New("chd: invalid flac data")

// flacReader reads FLAC's big-endian bit fields.
type flacReader struct {
	data []byte
	pos  int // in bits
}

func (reader *flacReader) bit() (uint32, error) {
	if reader.pos >= len(reader.data)*8 {
		return 0, errFLAC
	}
	value := uint32(reader.data[reader.pos>>3]>>(7-reader.pos&7)) & 1
	reader.pos++
	return value, nil
}

func (reader *flacReader) uint(n int) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if reader.pos+n > len(reader.data)*8 {
		return 0, errFLAC
	}
	var value uint64
	for n > 0 {
		offset := reader.pos & 7
		take := min(8-offset, n)
		chunk := uint64(reader.data[reader.pos>>3]>>(8-offset-take)) & (1<<take - 1)
		value = value<<take | chunk
		reader.pos += take
		n -= take
	}
	return value, nil
}

func (reader *flacReader) int(n int) (int64, error) {
	value, err := reader.uint(n)
	if err != nil || n == 0 {
		return 0, err
	}
	return int64(value<<(64-n)) >> (64 - n), nil
}

// unary counts zero bits up to the next one bit, which it consumes.
func (reader *flacReader) unary() (uint64, error) {
	var count uint64
	for {
		if reader.pos >= len(reader.data)*8 {
			return 0, errFLAC
		}
		// Skip whole zero bytes quickly when aligned.
		if reader.pos&7 == 0 && reader.data[reader.pos>>3] == 0 {
			count += 8
			reader.pos += 8
			continue
		}
		offset := reader.pos & 7
		rest := reader.data[reader.pos>>3] << offset
		if rest == 0 {
			count += uint64(8 - offset)
			reader.pos += 8 - offset
			continue
		}
		zeros := bits.LeadingZeros8(rest)
		count += uint64(zeros)
		reader.pos += zeros + 1
		return count, nil
	}
}

func (reader *flacReader) align() {
	reader.pos = (reader.pos + 7) &^ 7
}

// decodeFLAC decodes headerless FLAC frames (as CHD stores them) into interleaved stereo 16-bit
// samples until samples is full, and returns how many bytes of data the frames used.
func decodeFLAC(data []byte, samples []int16) (int, error) {
	const channels = 2
	reader := &flacReader{data: data}
	filled := 0
	for filled < len(samples) {
		decoded, err := decodeFLACFrame(reader, samples[filled:])
		if err != nil {
			return 0, err
		}
		filled += decoded * channels
	}
	return reader.pos / 8, nil
}

// decodeFLACFrame decodes one frame into out (interleaved stereo) and returns its block size.
func decodeFLACFrame(reader *flacReader, out []int16) (int, error) {
	if reader.pos&7 != 0 {
		return 0, errFLAC
	}
	sync, err := reader.uint(15)
	if err != nil || sync != 0x3FFE<<1 {
		return 0, errFLAC
	}
	if _, err := reader.uint(1); err != nil { // blocking strategy
		return 0, err
	}
	blockCode, _ := reader.uint(4)
	rateCode, _ := reader.uint(4)
	assignment, _ := reader.uint(4)
	sizeCode, _ := reader.uint(3)
	if _, err := reader.uint(1); err != nil {
		return 0, err
	}
	// The frame or sample number, UTF-8 style: the leading ones of the first byte give the length.
	first, err := reader.uint(8)
	if err != nil {
		return 0, err
	}
	for extra := bits.LeadingZeros8(^uint8(first)) - 1; extra > 0; extra-- {
		if _, err := reader.uint(8); err != nil {
			return 0, err
		}
	}
	var blockSize int
	switch {
	case blockCode == 1:
		blockSize = 192
	case blockCode >= 2 && blockCode <= 5:
		blockSize = 576 << (blockCode - 2)
	case blockCode == 6:
		value, err := reader.uint(8)
		if err != nil {
			return 0, err
		}
		blockSize = int(value) + 1
	case blockCode == 7:
		value, err := reader.uint(16)
		if err != nil {
			return 0, err
		}
		blockSize = int(value) + 1
	case blockCode >= 8:
		blockSize = 256 << (blockCode - 8)
	default:
		return 0, errFLAC
	}
	switch rateCode {
	case 12:
		_, err = reader.uint(8)
	case 13, 14:
		_, err = reader.uint(16)
	case 15:
		err = errFLAC
	}
	if err != nil {
		return 0, err
	}
	sampleBits := map[uint64]int{0: 16, 1: 8, 2: 12, 4: 16, 5: 20, 6: 24, 7: 32}[sizeCode]
	if sampleBits == 0 {
		return 0, errFLAC
	}
	if _, err := reader.uint(8); err != nil { // header CRC-8
		return 0, err
	}
	if assignment > 10 || (assignment < 8 && assignment != 1) {
		return 0, errFLAC // CHD audio is always stereo
	}
	if blockSize*2 > len(out) {
		return 0, errFLAC
	}

	var channels [2][]int32
	for channel := range channels {
		channelBits := sampleBits
		// The side channel carries one extra bit.
		if (assignment == 8 && channel == 1) || (assignment == 9 && channel == 0) || (assignment == 10 && channel == 1) {
			channelBits++
		}
		samples, err := decodeSubframe(reader, blockSize, channelBits)
		if err != nil {
			return 0, err
		}
		channels[channel] = samples
	}
	reader.align()
	if _, err := reader.uint(16); err != nil { // frame CRC-16
		return 0, err
	}

	left, right := channels[0], channels[1]
	for index := 0; index < blockSize; index++ {
		a, b := left[index], right[index]
		switch assignment {
		case 8: // left, side
			b = a - b
		case 9: // side, right
			a = a + b
		case 10: // mid, side
			mid := a<<1 | b&1
			a, b = (mid+b)>>1, (mid-b)>>1
		}
		out[index*2] = int16(a)
		out[index*2+1] = int16(b)
	}
	return blockSize, nil
}

func decodeSubframe(reader *flacReader, blockSize, sampleBits int) ([]int32, error) {
	if pad, err := reader.uint(1); err != nil || pad != 0 {
		return nil, errFLAC
	}
	kind, err := reader.uint(6)
	if err != nil {
		return nil, err
	}
	wasted := 0
	if flag, err := reader.uint(1); err != nil {
		return nil, err
	} else if flag == 1 {
		count, err := reader.unary()
		if err != nil {
			return nil, err
		}
		wasted = int(count) + 1
		sampleBits -= wasted
	}
	samples := make([]int32, blockSize)
	switch {
	case kind == 0: // constant
		value, err := reader.int(sampleBits)
		if err != nil {
			return nil, err
		}
		for index := range samples {
			samples[index] = int32(value)
		}
	case kind == 1: // verbatim
		for index := range samples {
			value, err := reader.int(sampleBits)
			if err != nil {
				return nil, err
			}
			samples[index] = int32(value)
		}
	case kind >= 8 && kind <= 12: // fixed predictor
		order := int(kind - 8)
		if err := readWarmup(reader, samples, order, sampleBits); err != nil {
			return nil, err
		}
		if err := readResidual(reader, samples, order); err != nil {
			return nil, err
		}
		fixedPredict(samples, order)
	case kind >= 32: // linear predictor
		order := int(kind-32) + 1
		if order > blockSize {
			return nil, errFLAC
		}
		if err := readWarmup(reader, samples, order, sampleBits); err != nil {
			return nil, err
		}
		precision, err := reader.uint(4)
		if err != nil || precision == 15 {
			return nil, errFLAC
		}
		shift, err := reader.int(5)
		if err != nil || shift < 0 {
			return nil, errFLAC
		}
		coefficients := make([]int64, order)
		for index := range coefficients {
			if coefficients[index], err = reader.int(int(precision) + 1); err != nil {
				return nil, err
			}
		}
		if err := readResidual(reader, samples, order); err != nil {
			return nil, err
		}
		for index := order; index < blockSize; index++ {
			var sum int64
			for tap, coefficient := range coefficients {
				sum += coefficient * int64(samples[index-1-tap])
			}
			samples[index] += int32(sum >> shift)
		}
	default:
		return nil, errFLAC
	}
	if wasted > 0 {
		for index := range samples {
			samples[index] <<= wasted
		}
	}
	return samples, nil
}

func readWarmup(reader *flacReader, samples []int32, order, sampleBits int) error {
	if order > len(samples) {
		return errFLAC
	}
	for index := 0; index < order; index++ {
		value, err := reader.int(sampleBits)
		if err != nil {
			return err
		}
		samples[index] = int32(value)
	}
	return nil
}

// readResidual reads the Rice-coded residual into samples[order:].
func readResidual(reader *flacReader, samples []int32, order int) error {
	method, err := reader.uint(2)
	if err != nil || method > 1 {
		return errFLAC
	}
	paramBits, escape := 4, uint64(15)
	if method == 1 {
		paramBits, escape = 5, 31
	}
	partitionOrder, err := reader.uint(4)
	if err != nil {
		return err
	}
	partitions := 1 << partitionOrder
	if len(samples)%partitions != 0 {
		return errFLAC
	}
	perPartition := len(samples) / partitions
	index := order
	for partition := 0; partition < partitions; partition++ {
		count := perPartition
		if partition == 0 {
			count -= order
		}
		if count < 0 {
			return errFLAC
		}
		param, err := reader.uint(paramBits)
		if err != nil {
			return err
		}
		if param == escape {
			rawBits, err := reader.uint(5)
			if err != nil {
				return err
			}
			for ; count > 0; count-- {
				value, err := reader.int(int(rawBits))
				if err != nil {
					return err
				}
				samples[index] = int32(value)
				index++
			}
			continue
		}
		for ; count > 0; count-- {
			quotient, err := reader.unary()
			if err != nil {
				return err
			}
			low, err := reader.uint(int(param))
			if err != nil {
				return err
			}
			value := quotient<<param | low
			samples[index] = int32(int64(value>>1) ^ -int64(value&1))
			index++
		}
	}
	return nil
}

func fixedPredict(samples []int32, order int) {
	for index := order; index < len(samples); index++ {
		var prediction int32
		switch order {
		case 1:
			prediction = samples[index-1]
		case 2:
			prediction = 2*samples[index-1] - samples[index-2]
		case 3:
			prediction = 3*samples[index-1] - 3*samples[index-2] + samples[index-3]
		case 4:
			prediction = 4*samples[index-1] - 6*samples[index-2] + 4*samples[index-3] - samples[index-4]
		}
		samples[index] += prediction
	}
}

package chd

// CD-ROM error correction (ECMA-130 P and Q parity), which CD codecs drop when it can be
// regenerated and which the reader must recompute to restore the original sector.

var syncHeader = [12]byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

const (
	modeOffset   = 15
	eccPOffset   = 2076
	eccPBytes    = 86
	eccPComps    = 24
	eccQOffset   = 2248
	eccQBytes    = 52
	eccQComps    = 43
	eccDataStart = 12
)

// eccLow multiplies by x in GF(2^8) (polynomial 0x11d); eccHigh inverts (x+1) multiplication.
var eccLow, eccHigh = func() (low, high [256]byte) {
	for index := 0; index < 256; index++ {
		value := index << 1
		if index&0x80 != 0 {
			value ^= 0x11d
		}
		low[index] = byte(value)
		high[byte(value)^byte(index)] = byte(index)
	}
	return low, high
}()

// generateECC fills in a sector's P and Q parity bytes.
func generateECC(sector []byte) {
	for row := 0; row < eccPBytes; row++ {
		value1, value2 := eccRow(sector, eccPComps, func(component int) int { return row + 86*component })
		sector[eccPOffset+row] = value1
		sector[eccPOffset+eccPBytes+row] = value2
	}
	for row := 0; row < eccQBytes; row++ {
		value1, value2 := eccRow(sector, eccQComps, func(component int) int {
			return (86*(row>>1) + (row & 1) + 88*component) % 2236
		})
		sector[eccQOffset+row] = value1
		sector[eccQOffset+eccQBytes+row] = value2
	}
}

func eccRow(sector []byte, components int, offset func(int) int) (byte, byte) {
	data := sector[eccDataStart:]
	mode2 := sector[modeOffset] == 2
	var value1, value2 byte
	for component := 0; component < components; component++ {
		position := offset(component)
		var b byte
		if !mode2 || position >= 4 { // mode 2 parity treats the header as zero
			b = data[position]
		}
		value1 = eccLow[value1^b]
		value2 ^= b
	}
	value1 = eccHigh[eccLow[value1]^value2]
	return value1, value2 ^ value1
}

//go:build chdfixtures

package chd

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateFixtureSources writes the CUE/BIN and GDI sources testdata/generate.sh turns
// into CHDs with chdman. Run it through that script, not on its own.
func TestGenerateFixtureSources(t *testing.T) {
	dir := os.Getenv("CHD_FIXTURE_SOURCES")
	if dir == "" {
		t.Skip("set CHD_FIXTURE_SOURCES")
	}
	random := rand.New(rand.NewPCG(1, 2))
	// MODE1 sectors with valid ECC, so chdman strips it and the reader must regenerate it.
	mode1 := make([]byte, 0, 64*sectorBytes)
	for index := 0; index < 64; index++ {
		sector := make([]byte, sectorBytes)
		copy(sector, syncHeader[:])
		lba := index + 150
		sector[12], sector[13], sector[14] = bcd(lba/4500), bcd(lba/75%60), bcd(lba%75)
		sector[modeOffset] = 1
		for position := 16; position < 2064; position++ {
			sector[position] = byte(position*7 + index)
			if position%97 == 0 {
				sector[position] = byte(random.IntN(256))
			}
		}
		generateECC(sector)
		mode1 = append(mode1, sector...)
	}
	audio := func(frames int, frequency float64) []byte {
		out := make([]byte, frames*sectorBytes)
		for sample := 0; sample < frames*sectorBytes/4; sample++ {
			value := int16(6000*math.Sin(float64(sample)*frequency)) + int16(random.IntN(9)-4)
			binary.LittleEndian.PutUint16(out[sample*4:], uint16(value))
			binary.LittleEndian.PutUint16(out[sample*4+2:], uint16(-value))
		}
		return out
	}
	mode2 := make([]byte, 40*sectorBytes)
	for position := range mode2 {
		mode2[position] = byte(position*13 + position/2352)
	}
	files := map[string][]byte{
		"Game (Track 1).bin": mode1,
		"Game (Track 2).bin": audio(30+45, 0.03), // a 30-frame pregap stored before the track
		"Game (Track 3).bin": mode2,
		"Game (Track 4).bin": audio(30+21, 0.05),
		"disc01.bin":         mode1[:32*sectorBytes],
		"disc02.raw":         audio(45, 0.04),
		"disc03.bin":         mode1[32*sectorBytes:],
	}
	raw := make([]byte, 64*1024)
	for position := range raw {
		raw[position] = byte(position*31 + position>>9)
		if position%11 == 0 {
			raw[position] = byte(random.IntN(256))
		}
	}
	files["disk.raw"] = raw
	files["Game.cue"] = []byte(`FILE "Game (Track 1).bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 00:00:00
FILE "Game (Track 2).bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:00:30
FILE "Game (Track 3).bin" BINARY
  TRACK 03 MODE2/2352
    INDEX 01 00:00:00
FILE "Game (Track 4).bin" BINARY
  TRACK 04 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:00:30
`)
	files["Disc.gdi"] = []byte("3\n1 0 4 2352 disc01.bin 0\n2 182 0 2352 disc02.raw 0\n3 45000 4 2352 disc03.bin 0\n")
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func bcd(value int) byte { return byte(value/10<<4 | value%10) }

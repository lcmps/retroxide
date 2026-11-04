package discs

import (
	"testing"

	"github.com/lcmps/retroxide/application/fault"
)

const redumpCUE = `FILE "Game (Track 1).bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
FILE "Game (Track 2).bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
`

const singleFileCUE = `REM a single file holding every track
FILE "Game.bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    PREGAP 00:02:00
    INDEX 01 00:10:00
  TRACK 03 AUDIO
    INDEX 00 00:20:00
    INDEX 01 00:22:00
`

func TestParseSheetReadsRedumpAndSingleFileCUEs(t *testing.T) {
	sheet, err := ParseSheet(SheetCUE, redumpCUE)
	if err != nil {
		t.Fatal(err)
	}
	if !sheet.OneTrackPerFile() || len(sheet.Tracks) != 2 {
		t.Fatalf("redump sheet = %+v", sheet)
	}
	if track := sheet.Tracks[1]; track.Mode != "AUDIO" || track.Start != 0 || track.Pregap != 150 || !track.PregapStored {
		t.Fatalf("track 2 = %+v; want a stored 150-frame pregap", track)
	}
	spans, err := sheet.Layout(map[string]int64{"Game (Track 1).bin": 2352 * 10, "Game (Track 2).bin": 2352 * 160})
	if err != nil || spans[1].Offset != 0 || spans[1].Length != 2352*160 {
		t.Fatalf("layout = %+v, %v", spans, err)
	}

	single, err := ParseSheet(SheetCUE, singleFileCUE)
	if err != nil {
		t.Fatal(err)
	}
	if single.OneTrackPerFile() || len(single.Files()) != 1 {
		t.Fatalf("single-file sheet = %+v", single)
	}
	if track := single.Tracks[1]; track.Pregap != 150 || track.PregapStored || track.Start != 750 {
		t.Fatalf("track 2 = %+v; want a generated pregap", track)
	}
	spans, err = single.Layout(map[string]int64{"Game.bin": 2352 * 2000})
	if err != nil {
		t.Fatal(err)
	}
	// Track 3's pregap (INDEX 00 at 20s) is stored, so it starts there.
	for index, want := range [][2]int64{{0, 750}, {750, 1500 - 750}, {1500, 2000 - 1500}} {
		if spans[index].Offset != want[0]*2352 || spans[index].Length != want[1]*2352 {
			t.Errorf("track %d span = %d+%d, want sectors %d+%d", index+1, spans[index].Offset/2352, spans[index].Length/2352, want[0], want[1])
		}
	}
}

func TestParseSheetReadsGDI(t *testing.T) {
	sheet, err := ParseSheet(SheetGDI, "3\n1 0 4 2352 track01.bin 0\n2 450 0 2352 \"track 02.raw\" 0\n3 45000 4 2352 track03.bin 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(sheet.Tracks) != 3 || sheet.Tracks[1].File != "track 02.raw" || sheet.Tracks[1].Mode != "AUDIO" || sheet.Tracks[2].Mode != "MODE1/2352" {
		t.Fatalf("GDI = %+v", sheet)
	}
}

func TestParseSheetRejectsWhatItCannotUse(t *testing.T) {
	for name, sheet := range map[string]struct {
		kind SheetKind
		text string
	}{
		"no tracks":          {SheetCUE, "REM nothing\n"},
		"wave file":          {SheetCUE, "FILE \"a.wav\" WAVE\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n"},
		"path in name":       {SheetCUE, "FILE \"../a.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n"},
		"no index":           {SheetCUE, "FILE \"a.bin\" BINARY\n  TRACK 01 AUDIO\n"},
		"bad mode":           {SheetCUE, "FILE \"a.bin\" BINARY\n  TRACK 01 CDG\n    INDEX 01 00:00:00\n"},
		"out of order":       {SheetCUE, "FILE \"a.bin\" BINARY\n  TRACK 02 AUDIO\n    INDEX 01 00:00:00\n"},
		"bad time":           {SheetCUE, "FILE \"a.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:61:00\n"},
		"gdi count mismatch": {SheetGDI, "2\n1 0 4 2352 a.bin 0\n"},
		"gdi bad type":       {SheetGDI, "1\n1 0 7 2352 a.bin 0\n"},
	} {
		if _, err := ParseSheet(sheet.kind, sheet.text); !fault.IsCode(err, fault.CodeValidation) {
			t.Errorf("%s: %v, want a validation error", name, err)
		}
	}
	sheet, _ := ParseSheet(SheetCUE, redumpCUE)
	if _, err := sheet.Layout(map[string]int64{"Game (Track 1).bin": 2352}); !fault.IsCode(err, fault.CodeValidation) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := sheet.Layout(map[string]int64{"Game (Track 1).bin": 2352, "Game (Track 2).bin": 1000}); !fault.IsCode(err, fault.CodeValidation) {
		t.Errorf("partial sector: %v", err)
	}
}

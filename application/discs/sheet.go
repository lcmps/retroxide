// Package discs handles CD and GD-ROM images: reading CUE and GDI sheets, indexing each track's
// hashes (what disc databases list), compressing discs to CHD, and rebuilding their tracks.
package discs

import (
	"bufio"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
)

// sectorBytes maps CUE track modes to their sector size.
var sectorBytes = map[string]int{
	"AUDIO": 2352, "MODE1/2048": 2048, "MODE1/2352": 2352, "MODE2/2336": 2336, "MODE2/2352": 2352,
	"MODE2/2048": 2048, "MODE2/2324": 2324, "CDI/2336": 2336, "CDI/2352": 2352,
}

// SheetKind is the format of a disc's sheet.
type SheetKind string

const (
	SheetCUE SheetKind = "cue"
	SheetGDI SheetKind = "gdi"
)

// SheetKindFor returns the sheet kind a file name stands for, if any.
func SheetKindFor(filename string) (SheetKind, bool) {
	switch strings.ToLower(path.Ext(filename)) {
	case ".cue":
		return SheetCUE, true
	case ".gdi":
		return SheetGDI, true
	}
	return "", false
}

// SheetTrack is one track as a sheet describes it.
type SheetTrack struct {
	Number      int
	Mode        string
	SectorBytes int
	// File is the track's file; Start is where, in sectors, the track begins in it (its INDEX 00
	// when the pregap is in the file, else its INDEX 01).
	File  string
	Start int
	// Pregap is in frames; PregapStored says it is in the file (INDEX 00 before INDEX 01)
	// rather than generated (a PREGAP command).
	Pregap       int
	PregapStored bool
	Postgap      int
}

// Sheet is a parsed CUE sheet or GDI file.
type Sheet struct {
	Kind   SheetKind
	Tracks []SheetTrack
}

// Files lists the files the sheet names, in order of first use.
func (sheet Sheet) Files() []string {
	var files []string
	seen := map[string]bool{}
	for _, track := range sheet.Tracks {
		if !seen[track.File] {
			seen[track.File] = true
			files = append(files, track.File)
		}
	}
	return files
}

// OneTrackPerFile reports whether every file holds exactly one track, as Redump sets do.
func (sheet Sheet) OneTrackPerFile() bool {
	return len(sheet.Files()) == len(sheet.Tracks)
}

func invalidSheet(format string, args ...any) error {
	return fault.Validation("invalid disc sheet: " + fmt.Sprintf(format, args...))
}

// ParseSheet parses a CUE sheet or GDI file.
func ParseSheet(kind SheetKind, text string) (Sheet, error) {
	switch kind {
	case SheetCUE:
		return parseCUE(text)
	case SheetGDI:
		return parseGDI(text)
	}
	return Sheet{}, invalidSheet("unknown sheet type")
}

func parseCUE(text string) (Sheet, error) {
	sheet := Sheet{Kind: SheetCUE}
	var file string
	var current *SheetTrack
	var index0 = -1
	finish := func() error {
		if current == nil {
			return nil
		}
		if current.Start < 0 {
			return invalidSheet("track %d has no INDEX 01", current.Number)
		}
		if index0 >= 0 {
			current.Pregap = current.Start - index0
			current.PregapStored = current.Pregap > 0
			current.Start = index0
		}
		sheet.Tracks = append(sheet.Tracks, *current)
		current, index0 = nil, -1
		return nil
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	for line := 1; scanner.Scan(); line++ {
		fields := cueFields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FILE":
			if err := finish(); err != nil {
				return Sheet{}, err
			}
			if len(fields) < 3 {
				return Sheet{}, invalidSheet("line %d: FILE needs a name and a type", line)
			}
			if kind := strings.ToUpper(fields[len(fields)-1]); kind != "BINARY" {
				return Sheet{}, invalidSheet("line %d: only BINARY track files are supported, not %s", line, kind)
			}
			file = strings.Join(fields[1:len(fields)-1], " ")
			if file == "" || file == "." || file == ".." || strings.ContainsAny(file, "/\\") {
				return Sheet{}, invalidSheet("line %d: track files must be plain names in the same folder", line)
			}
		case "TRACK":
			if err := finish(); err != nil {
				return Sheet{}, err
			}
			if file == "" || len(fields) < 3 {
				return Sheet{}, invalidSheet("line %d: TRACK before FILE, or without a mode", line)
			}
			number, err := strconv.Atoi(fields[1])
			mode := strings.ToUpper(fields[2])
			if err != nil || number < 1 || number > 99 || number != len(sheet.Tracks)+1 {
				return Sheet{}, invalidSheet("line %d: tracks must be numbered 1, 2, 3...", line)
			}
			if sectorBytes[mode] == 0 {
				return Sheet{}, invalidSheet("line %d: unsupported track mode %s", line, fields[2])
			}
			current = &SheetTrack{Number: number, Mode: mode, SectorBytes: sectorBytes[mode], File: file, Start: -1}
		case "INDEX":
			if current == nil || len(fields) < 3 {
				return Sheet{}, invalidSheet("line %d: INDEX outside a track", line)
			}
			frames, err := parseMSF(fields[2])
			if err != nil {
				return Sheet{}, invalidSheet("line %d: %v", line, err)
			}
			switch fields[1] {
			case "00", "0":
				index0 = frames
			case "01", "1":
				current.Start = frames
			}
		case "PREGAP", "POSTGAP":
			if current == nil || len(fields) < 2 {
				return Sheet{}, invalidSheet("line %d: %s outside a track", line, fields[0])
			}
			frames, err := parseMSF(fields[1])
			if err != nil {
				return Sheet{}, invalidSheet("line %d: %v", line, err)
			}
			if strings.EqualFold(fields[0], "PREGAP") {
				current.Pregap = frames
			} else {
				current.Postgap = frames
			}
		}
	}
	if err := finish(); err != nil {
		return Sheet{}, err
	}
	if len(sheet.Tracks) == 0 {
		return Sheet{}, invalidSheet("no tracks")
	}
	return sheet, scanner.Err()
}

// cueFields splits a CUE line into words, keeping quoted names whole.
func cueFields(line string) []string {
	var fields []string
	line = strings.TrimSpace(line)
	for line != "" {
		if line[0] == '"' {
			end := strings.IndexByte(line[1:], '"')
			if end < 0 {
				fields = append(fields, line[1:])
				break
			}
			fields = append(fields, line[1:1+end])
			line = strings.TrimSpace(line[2+end:])
			continue
		}
		word, rest, _ := strings.Cut(line, " ")
		fields = append(fields, word)
		line = strings.TrimSpace(rest)
	}
	return fields
}

func parseMSF(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("%q is not mm:ss:ff", value)
	}
	var numbers [3]int
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return 0, fmt.Errorf("%q is not mm:ss:ff", value)
		}
		numbers[index] = number
	}
	if numbers[1] >= 60 || numbers[2] >= 75 {
		return 0, fmt.Errorf("%q is out of range", value)
	}
	return (numbers[0]*60+numbers[1])*75 + numbers[2], nil
}

func parseGDI(text string) (Sheet, error) {
	sheet := Sheet{Kind: SheetGDI}
	scanner := bufio.NewScanner(strings.NewReader(text))
	count := -1
	for line := 1; scanner.Scan(); line++ {
		fields := cueFields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if count < 0 {
			number, err := strconv.Atoi(fields[0])
			if err != nil || number < 1 || number > 99 {
				return Sheet{}, invalidSheet("line %d: the first line gives the track count", line)
			}
			count = number
			continue
		}
		if len(fields) < 5 {
			return Sheet{}, invalidSheet("line %d: expected track, sector, type, size, and file", line)
		}
		number, err1 := strconv.Atoi(fields[0])
		kind, err2 := strconv.Atoi(fields[2])
		size, err3 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || err3 != nil || number != len(sheet.Tracks)+1 || (size != 2352 && size != 2048) {
			return Sheet{}, invalidSheet("line %d: malformed track", line)
		}
		mode := "AUDIO"
		if kind == 4 {
			mode = fmt.Sprintf("MODE1/%d", size)
		} else if kind != 0 || size != 2352 {
			return Sheet{}, invalidSheet("line %d: unsupported track type %d", line, kind)
		}
		file := fields[4]
		if file == "." || file == ".." || strings.ContainsAny(file, "/\\") {
			return Sheet{}, invalidSheet("line %d: track files must be plain names in the same folder", line)
		}
		sheet.Tracks = append(sheet.Tracks, SheetTrack{Number: number, Mode: mode, SectorBytes: size, File: file})
	}
	if count < 1 || len(sheet.Tracks) != count {
		return Sheet{}, invalidSheet("the sheet lists %d tracks but describes %d", count, len(sheet.Tracks))
	}
	return sheet, scanner.Err()
}

// TrackSpan is where a track's data lies within its file.
type TrackSpan struct {
	Track  SheetTrack
	Offset int64
	Length int64
}

// Layout places each track within its file, given every file's size: a track runs from its
// start to the next track's start in the same file, or to the end of the file.
func (sheet Sheet) Layout(sizes map[string]int64) ([]TrackSpan, error) {
	spans := make([]TrackSpan, len(sheet.Tracks))
	for index, track := range sheet.Tracks {
		size, ok := sizes[track.File]
		if !ok {
			return nil, fault.Validation(fmt.Sprintf("the sheet needs %q, which was not uploaded", track.File))
		}
		start := int64(track.Start) * int64(track.SectorBytes)
		end := size
		if index+1 < len(sheet.Tracks) && sheet.Tracks[index+1].File == track.File {
			next := sheet.Tracks[index+1]
			end = int64(next.Start) * int64(next.SectorBytes)
		}
		if start > end || end > size || (end-start)%int64(track.SectorBytes) != 0 || end == start {
			return nil, fault.Validation(fmt.Sprintf("track %d does not fit %q (%d bytes)", track.Number, track.File, size))
		}
		spans[index] = TrackSpan{Track: track, Offset: start, Length: end - start}
	}
	return spans, nil
}

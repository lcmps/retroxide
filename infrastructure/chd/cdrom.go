package chd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Metadata tags describing CD tracks.
const (
	tagCDTrack   = 'C'<<24 | 'H'<<16 | 'T'<<8 | 'R' // CHTR: track, type, subtype, frames
	tagCDTrack2  = 'C'<<24 | 'H'<<16 | 'T'<<8 | '2' // CHT2: adds pregap and postgap
	tagGDTrack   = 'C'<<24 | 'H'<<16 | 'G'<<8 | 'D' // CHGD: GD-ROM, adds padding
	tagGDTrackLE = 'C'<<24 | 'H'<<16 | 'G'<<8 | 'T' // CHGT: legacy little-endian GD-ROM
	trackPadding = 4
	maxTracks    = 99
)

// trackDataSizes maps CHD track types to the bytes of each sector a track file holds.
var trackDataSizes = map[string]int{
	"MODE1": 2048, "MODE1/2048": 2048, "MODE1_RAW": 2352, "MODE1/2352": 2352,
	"MODE2": 2336, "MODE2/2336": 2336, "MODE2_FORM1": 2048, "MODE2/2048": 2048,
	"MODE2_FORM2": 2324, "MODE2/2324": 2324, "MODE2_FORM_MIX": 2336, "MODE2_RAW": 2352,
	"MODE2/2352": 2352, "CDI/2352": 2352, "AUDIO": 2352,
}

// Track is one track of a CD or GD-ROM image.
type Track struct {
	Number int
	// Type is the CHD track type, such as MODE2_RAW or AUDIO.
	Type string
	// SectorBytes is how many bytes of each sector the track's file holds.
	SectorBytes int
	// Frames counts the track's sectors in the image, including a pregap stored with it.
	Frames int
	// Pregap is the track's pregap in frames; PregapStored says whether its data is in the
	// image (as it is for CUE sheets with INDEX 00) or generated.
	Pregap       int
	PregapStored bool
	Postgap      int
	// padFrames are GD-ROM filler frames between areas, which track files leave out.
	padFrames  int
	chdFrame   int // the track's first frame within the CHD
	discOffset int // the track's first sector on a GD-ROM, for the GDI sheet
}

// CueMode is the track's mode as a CUE sheet writes it: AUDIO, MODE1/2352, MODE2/2336...
func (track Track) CueMode() string {
	switch {
	case strings.HasPrefix(track.Type, "MODE1"):
		return fmt.Sprintf("MODE1/%04d", track.SectorBytes)
	case strings.HasPrefix(track.Type, "MODE2"), strings.HasPrefix(track.Type, "CDI"):
		return fmt.Sprintf("MODE2/%04d", track.SectorBytes)
	}
	return "AUDIO"
}

// Audio reports whether the track is CD audio.
func (track Track) Audio() bool { return track.Type == "AUDIO" }

// FileFrames is how many sectors the track's file holds.
func (track Track) FileFrames() int { return track.Frames - track.padFrames }

// FileBytes is the size of the track's file.
func (track Track) FileBytes() int64 { return int64(track.FileFrames()) * int64(track.SectorBytes) }

// Disc is the track layout of a CD or GD-ROM CHD.
type Disc struct {
	Tracks []Track
	// GDROM reports a Dreamcast GD-ROM, whose sheet is a GDI file rather than a CUE sheet.
	GDROM bool
}

// IsCD reports whether the CHD holds a CD or GD-ROM (rather than a DVD or hard disk).
func (reader *Reader) IsCD() bool {
	return reader.header.UnitBytes == frameBytes && reader.header.HunkBytes%frameBytes == 0
}

// Disc reads the CHD's track layout.
func (reader *Reader) Disc() (*Disc, error) {
	if !reader.IsCD() {
		return nil, fmt.Errorf("%w: not a CD image", ErrUnsupported)
	}
	disc := &Disc{}
	entries, err := reader.Metadata(tagCDTrack2)
	if err == nil && len(entries) == 0 {
		entries, err = reader.Metadata(tagCDTrack)
	}
	if err == nil && len(entries) == 0 {
		if legacy, _ := reader.Metadata(tagGDTrackLE); len(legacy) > 0 {
			return nil, fmt.Errorf("%w: legacy GD-ROM CHD (convert it with chdman copy)", ErrUnsupported)
		}
		entries, err = reader.Metadata(tagGDTrack)
		disc.GDROM = true
	}
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > maxTracks {
		return nil, fmt.Errorf("%w: no track metadata", ErrCorrupt)
	}
	disc.Tracks = make([]Track, len(entries))
	chdFrame, discOffset := 0, 0
	for index, entry := range entries {
		fields := parseFields(entry)
		number, _ := strconv.Atoi(fields["TRACK"])
		if number != index+1 {
			return nil, fmt.Errorf("%w: track %d is out of order", ErrCorrupt, number)
		}
		track := Track{Number: number, Type: fields["TYPE"]}
		track.SectorBytes = trackDataSizes[track.Type]
		if track.SectorBytes == 0 {
			return nil, fmt.Errorf("%w: unknown track type %q", ErrUnsupported, track.Type)
		}
		track.Frames, _ = strconv.Atoi(fields["FRAMES"])
		track.Pregap, _ = strconv.Atoi(fields["PREGAP"])
		track.Postgap, _ = strconv.Atoi(fields["POSTGAP"])
		track.padFrames, _ = strconv.Atoi(fields["PAD"])
		track.PregapStored = track.Pregap > 0 && strings.HasPrefix(fields["PGTYPE"], "V")
		if track.Frames <= 0 || track.padFrames < 0 || track.padFrames > track.Frames || track.Pregap < 0 || track.Pregap > track.Frames {
			return nil, fmt.Errorf("%w: track %d has an invalid length", ErrCorrupt, number)
		}
		track.chdFrame = chdFrame
		track.discOffset = discOffset
		chdFrame += (track.Frames + trackPadding - 1) / trackPadding * trackPadding
		discOffset += track.Frames
		disc.Tracks[index] = track
	}
	if uint64(chdFrame)*frameBytes > uint64(reader.header.HunkCount)*uint64(reader.header.HunkBytes) {
		return nil, fmt.Errorf("%w: tracks exceed the image", ErrCorrupt)
	}
	return disc, nil
}

// parseFields reads metadata such as "TRACK:1 TYPE:AUDIO FRAMES:450" into a map.
func parseFields(entry []byte) map[string]string {
	fields := map[string]string{}
	for _, token := range strings.Fields(strings.TrimRight(string(entry), "\x00")) {
		if key, value, found := strings.Cut(token, ":"); found {
			fields[key] = value
		}
	}
	return fields
}

// ExtractTrack writes the track's file (as chdman extractcd writes it, so its hashes match
// the disc databases): each sector's data bytes, with audio in little-endian byte order.
// Hunks are decompressed on several cores at once, which matters most for LZMA.
func (reader *Reader) ExtractTrack(track Track, out io.Writer) (int64, error) {
	if track.FileFrames() == 0 {
		return 0, nil
	}
	writer := bufio.NewWriterSize(out, 1<<20)
	framesPerHunk := int(reader.header.HunkBytes) / frameBytes
	first := track.chdFrame / framesPerHunk
	last := (track.chdFrame + track.FileFrames() - 1) / framesPerHunk
	hunks := reader.decodeHunks(first, last)
	defer hunks.stop()
	sector := make([]byte, track.SectorBytes)
	var written int64
	var hunk []byte
	loaded := -1
	for frame := 0; frame < track.FileFrames(); frame++ {
		position := track.chdFrame + frame
		if hunkIndex := position / framesPerHunk; hunkIndex != loaded {
			var err error
			if hunk, err = hunks.next(); err != nil {
				return written, err
			}
			loaded = hunkIndex
		}
		offset := position % framesPerHunk * frameBytes
		copy(sector, hunk[offset:offset+track.SectorBytes])
		if track.Audio() {
			// CHD keeps audio big-endian; track files hold it little-endian.
			for index := 0; index+1 < len(sector); index += 2 {
				sector[index], sector[index+1] = sector[index+1], sector[index]
			}
		}
		count, err := writer.Write(sector)
		written += int64(count)
		if err != nil {
			return written, err
		}
	}
	return written, writer.Flush()
}

// TrackFileName names a track's file as chdman extractcd does for the sheet base name.
func (disc *Disc) TrackFileName(base string, track Track) string {
	if disc.GDROM {
		extension := ".bin"
		if track.Audio() {
			extension = ".raw"
		}
		return fmt.Sprintf("%s%02d%s", base, track.Number, extension)
	}
	if len(disc.Tracks) >= 10 {
		return fmt.Sprintf("%s (Track %02d).bin", base, track.Number)
	}
	return fmt.Sprintf("%s (Track %d).bin", base, track.Number)
}

// SheetName names the disc's sheet: base.gdi for GD-ROMs, base.cue otherwise.
func (disc *Disc) SheetName(base string) string {
	if disc.GDROM {
		return base + ".gdi"
	}
	return base + ".cue"
}

// Sheet writes the disc's CUE sheet (or GDI file), one file per track, as chdman extractcd
// writes it with split tracks.
func (disc *Disc) Sheet(base string) string {
	var sheet strings.Builder
	if disc.GDROM {
		fmt.Fprintf(&sheet, "%d\n", len(disc.Tracks))
		for _, track := range disc.Tracks {
			kind := 4
			if track.Audio() {
				kind = 0
			}
			name := disc.TrackFileName(base, track)
			if strings.Contains(name, " ") {
				name = `"` + name + `"`
			}
			fmt.Fprintf(&sheet, "%d %d %d %d %s %d\n", track.Number, track.discOffset, kind, track.SectorBytes, name, 0)
		}
		return sheet.String()
	}
	for _, track := range disc.Tracks {
		fmt.Fprintf(&sheet, "FILE \"%s\" BINARY\n", disc.TrackFileName(base, track))
		fmt.Fprintf(&sheet, "  TRACK %02d %s\n", track.Number, track.CueMode())
		switch {
		case track.Pregap > 0 && !track.PregapStored:
			fmt.Fprintf(&sheet, "    PREGAP %s\n", msf(track.Pregap))
			fmt.Fprintf(&sheet, "    INDEX 01 %s\n", msf(0))
		case track.Pregap > 0:
			fmt.Fprintf(&sheet, "    INDEX 00 %s\n", msf(0))
			fmt.Fprintf(&sheet, "    INDEX 01 %s\n", msf(track.Pregap))
		default:
			fmt.Fprintf(&sheet, "    INDEX 01 %s\n", msf(0))
		}
		if track.Postgap > 0 {
			fmt.Fprintf(&sheet, "    POSTGAP %s\n", msf(track.Postgap))
		}
	}
	return sheet.String()
}

// msf formats frames as minutes:seconds:frames, 75 frames a second.
func msf(frames int) string {
	return fmt.Sprintf("%02d:%02d:%02d", frames/(60*75), frames/75%60, frames%75)
}

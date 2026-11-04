package rom

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// FileFormat says how a ROM file is stored.
type FileFormat string

const (
	// FormatFile is a single file kept as uploaded (a cartridge ROM, an ISO, an archive).
	FormatFile FileFormat = "file"
	// FormatCHD is a CHD disc image; its tracks are indexed in DiscTracks.
	FormatCHD FileFormat = "chd"
	// FormatTracks is a disc kept as its sheet (CUE or GDI) plus one stored file per track.
	FormatTracks FileFormat = "tracks"
	// FormatPending is a file stored before discs were recognized, not yet inspected.
	FormatPending FileFormat = "pending"
)

// DiscTrack is one track of a disc: its layout and the hashes of the track file, which are
// what disc databases (such as Redump's) list.
type DiscTrack struct {
	Number int
	// Mode is the track's CUE mode: AUDIO, MODE1/2352, MODE2/2352, and so on.
	Mode string
	// SectorBytes is how many bytes each sector takes in the track file.
	SectorBytes  int
	Frames       int
	Pregap       int
	PregapStored bool
	Postgap      int
	Size         int64
	Hash         ROMHash
	// Filename is the track file's name in the disc's sheet.
	Filename string
	// StoragePath holds the track file while the disc is stored as separate tracks.
	StoragePath *string
}

// Audio reports whether the track is CD audio.
func (track DiscTrack) Audio() bool { return track.Mode == "AUDIO" }

// DiscIdentity is a fingerprint of a disc's contents: the same disc gets the same identity
// whether it arrives as a CUE/BIN set or as a CHD, however the CHD was compressed.
func DiscIdentity(tracks []DiscTrack) string {
	var manifest strings.Builder
	for _, track := range tracks {
		fmt.Fprintf(&manifest, "%d %s %d %s\n", track.Number, track.Mode, track.Size, strings.ToLower(track.Hash.SHA1))
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:])
}

// ISOAvailable reports whether a disc can be written as an ISO image: a single MODE1 data
// track, whose 2048-byte sectors an ISO holds completely. Other discs (with audio tracks, or
// MODE2 sectors such as PlayStation's XA audio and video) would lose data.
func ISOAvailable(tracks []DiscTrack) bool {
	return len(tracks) == 1 && (tracks[0].Mode == "MODE1/2048" || tracks[0].Mode == "MODE1/2352")
}

// PrimaryTrack returns the track that identifies the disc in hash databases: the first data
// track, or the first track of an audio disc.
func PrimaryTrack(tracks []DiscTrack) (DiscTrack, bool) {
	for _, track := range tracks {
		if !track.Audio() {
			return track, true
		}
	}
	if len(tracks) > 0 {
		return tracks[0], true
	}
	return DiscTrack{}, false
}

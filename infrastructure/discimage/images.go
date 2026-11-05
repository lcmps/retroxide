// Package discimage adapts the CHD reader and the chdman-go library to the disc service.
package discimage

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/lcmps/retroxide/application/discs"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/chd"
)

// Hasher computes the hash bundle that identifies content.
type Hasher interface {
	NewDigest() romdomain.Digest
}

// Images reads CHD images with the built-in reader.
type Images struct {
	hasher Hasher
}

func NewImages(hasher Hasher) Images {
	return Images{hasher: hasher}
}

var _ discs.Images = Images{}

func (images Images) Inspect(ctx context.Context, content io.ReaderAt, size int64, base string) ([]romdomain.DiscTrack, bool, error) {
	reader, disc, ok, err := openDisc(content, size)
	if !ok || err != nil {
		return nil, ok, err
	}
	tracks := make([]romdomain.DiscTrack, len(disc.Tracks))
	for index, track := range disc.Tracks {
		if err := ctx.Err(); err != nil {
			return nil, true, err
		}
		digest := images.hasher.NewDigest()
		written, err := reader.ExtractTrack(track, digest)
		if err != nil {
			return nil, true, err
		}
		hash, err := digest.Sum()
		if err != nil {
			return nil, true, err
		}
		tracks[index] = romdomain.DiscTrack{
			Number: track.Number, Mode: track.CueMode(), SectorBytes: track.SectorBytes,
			Frames: track.FileFrames(), Pregap: track.Pregap, PregapStored: track.PregapStored,
			Postgap: track.Postgap, Size: written, Hash: hash, Filename: disc.TrackFileName(base, track),
		}
	}
	return tracks, true, nil
}

func (images Images) Extract(ctx context.Context, content io.ReaderAt, size int64, base string, add discs.AddFile) error {
	reader, disc, ok, err := openDisc(content, size)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("discimage: not a CD or GD-ROM image")
	}
	sheet := disc.Sheet(base)
	if err := add(disc.SheetName(base), int64(len(sheet)), func(writer io.Writer) error {
		_, err := io.Copy(writer, bytes.NewReader([]byte(sheet)))
		return err
	}); err != nil {
		return err
	}
	for _, track := range disc.Tracks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := add(disc.TrackFileName(base, track), track.FileBytes(), func(writer io.Writer) error {
			_, err := reader.ExtractTrack(track, writer)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (images Images) WriteTrack(ctx context.Context, content io.ReaderAt, size int64, number int, out io.Writer) error {
	reader, disc, ok, err := openDisc(content, size)
	if err != nil {
		return err
	}
	if !ok || number < 1 || number > len(disc.Tracks) {
		return errors.New("discimage: no such track")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = reader.ExtractTrack(disc.Tracks[number-1], out)
	return err
}

// openDisc opens content as a CD or GD-ROM CHD; ok is false for anything else.
func openDisc(content io.ReaderAt, size int64) (*chd.Reader, *chd.Disc, bool, error) {
	reader, err := chd.Open(content, size)
	if errors.Is(err, chd.ErrNotCHD) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, true, err
	}
	if !reader.IsCD() {
		return nil, nil, false, nil
	}
	disc, err := reader.Disc()
	if err != nil {
		return nil, nil, true, err
	}
	return reader, disc, true, nil
}

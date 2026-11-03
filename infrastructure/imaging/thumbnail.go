// Package imaging makes the small cover copies that library grids show.
package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	// Decoders for every image format covers arrive in.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"

	"github.com/lcmps/retroxide/application/rom"
)

const (
	// maxWidth and maxHeight bound thumbnails: a library tile is 124 CSS pixels wide with a 25:36
	// cover, so this is sharp on screens with twice as many pixels.
	maxWidth  = 256
	maxHeight = 368
	quality   = 80
	// maxPixels rejects images whose header claims a size large enough to exhaust memory when
	// decoded (a decompression bomb); real cover art is far smaller.
	maxPixels = 40_000_000
)

// background fills transparent areas, matching the library tile behind covers.
var background = color.RGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xff}

// Thumbnailer scales cover art down to library-grid size as JPEG.
type Thumbnailer struct{}

func NewThumbnailer() Thumbnailer {
	return Thumbnailer{}
}

var _ rom.Thumbnailer = Thumbnailer{}

// Thumbnail returns a JPEG copy of content that fits maxWidth by maxHeight. It returns
// rom.ErrThumbnailNotSmaller when the copy would not be smaller than content, as with small
// pixel-art PNGs, so the original can serve as its own thumbnail.
func (Thumbnailer) Thumbnail(content []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("reading image header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxPixels {
		return nil, errors.New("image dimensions are out of range")
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	width, height := fit(source.Bounds().Dx(), source.Bounds().Dy())
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(target, target.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	// Catmull-Rom averages every source pixel under each target pixel, so detail does not alias
	// away when a cover shrinks several times over.
	draw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, target, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encoding thumbnail: %w", err)
	}
	if encoded.Len() >= len(content) {
		return nil, rom.ErrThumbnailNotSmaller
	}
	return encoded.Bytes(), nil
}

// fit scales width by height down, keeping its aspect ratio, to fit maxWidth by maxHeight.
// Images that already fit keep their size.
func fit(width, height int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	if width*maxHeight > height*maxWidth {
		return maxWidth, max(1, height*maxWidth/width)
	}
	return max(1, width*maxHeight/height), maxHeight
}

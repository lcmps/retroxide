package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/lcmps/retroxide/application/rom"
)

func encodePNG(t *testing.T, width, height int, fill func(x, y int) color.Color) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			source.Set(x, y, fill(x, y))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestThumbnailScalesLargeCoversToFitTheTile(t *testing.T) {
	for _, size := range []image.Point{{1500, 2000}, {2000, 900}, {300, 1600}} {
		content := encodePNG(t, size.X, size.Y, func(x, y int) color.Color {
			return color.NRGBA{R: uint8(x * 7), G: uint8(y * 13), B: uint8(x ^ y), A: 0xff}
		})
		thumbnail, err := NewThumbnailer().Thumbnail(content)
		if err != nil {
			t.Fatalf("%v: %v", size, err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(thumbnail))
		if err != nil {
			t.Fatalf("%v: thumbnail is not a JPEG: %v", size, err)
		}
		bounds := decoded.Bounds()
		if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight || (bounds.Dx() != maxWidth && bounds.Dy() != maxHeight) {
			t.Fatalf("%v: thumbnail is %dx%d, want it to fill %dx%d", size, bounds.Dx(), bounds.Dy(), maxWidth, maxHeight)
		}
		if ratio, want := float64(bounds.Dx())/float64(bounds.Dy()), float64(size.X)/float64(size.Y); ratio < want*0.95 || ratio > want*1.05 {
			t.Fatalf("%v: aspect ratio %.2f, want %.2f", size, ratio, want)
		}
	}
}

func TestThumbnailFillsTransparencyWithTheTileBackground(t *testing.T) {
	content := encodePNG(t, 1024, 1024, func(x, y int) color.Color {
		if (x/7+y/5)%3 == 0 {
			return color.NRGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff}
		}
		return color.NRGBA{}
	})
	thumbnail, err := NewThumbnailer().Thumbnail(content)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := jpeg.Decode(bytes.NewReader(thumbnail))
	// A fully transparent corner region becomes the tile grey, not black.
	sample := color.RGBAModel.Convert(decoded.At(1, 2)).(color.RGBA)
	if sample.R < 0x20 && sample.G < 0x20 && sample.B < 0x20 {
		t.Fatalf("transparent area became %v, want the tile background", sample)
	}
}

func TestThumbnailKeepsSmallImagesAndRejectsGarbage(t *testing.T) {
	small := encodePNG(t, 64, 64, func(int, int) color.Color { return color.NRGBA{R: 0x10, A: 0xff} })
	if _, err := NewThumbnailer().Thumbnail(small); !errors.Is(err, rom.ErrThumbnailNotSmaller) {
		t.Fatalf("small image error = %v, want ErrThumbnailNotSmaller", err)
	}
	if _, err := NewThumbnailer().Thumbnail([]byte("not an image")); err == nil || errors.Is(err, rom.ErrThumbnailNotSmaller) {
		t.Fatalf("garbage error = %v, want a decoding error", err)
	}
}

func TestFitKeepsAspectRatioWithinBounds(t *testing.T) {
	for _, test := range []struct{ width, height, wantWidth, wantHeight int }{
		{100, 100, 100, 100},
		{1000, 1000, 256, 256},
		{1500, 2000, 256, 341},
		{256, 2000, 47, 368},
		{5000, 1, 256, 1},
	} {
		if width, height := fit(test.width, test.height); width != test.wantWidth || height != test.wantHeight {
			t.Errorf("fit(%d, %d) = %d, %d; want %d, %d", test.width, test.height, width, height, test.wantWidth, test.wantHeight)
		}
	}
}

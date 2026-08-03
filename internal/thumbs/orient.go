package thumbs

import (
	"image"

	"github.com/tomaszcichy9825/culler/internal/exif"
)

// sourceOrientation reads the EXIF orientation out of the source bytes.
// Anything unreadable or absent is 1: already upright.
func sourceOrientation(srcJPEG []byte) int {
	f, err := exif.Parse(srcJPEG)
	if err != nil || !f.Orientation.Present {
		return 1
	}
	o := int(f.Orientation.Value)
	if o < 1 || o > 8 {
		return 1
	}
	return o
}

// orient bakes an EXIF orientation into the pixels. The eight cases are the
// TIFF orientation values; 1 is upright and returns the image untouched.
func orient(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	dw, dh := w, h
	if o >= 5 { // the four transposed cases swap the axes
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // mirror horizontal
				dx, dy = w-1-x, y
			case 3: // rotate 180
				dx, dy = w-1-x, h-1-y
			case 4: // mirror vertical
				dx, dy = x, h-1-y
			case 5: // mirror horizontal, rotate 270 CW
				dx, dy = y, x
			case 6: // rotate 90 CW
				dx, dy = h-1-y, x
			case 7: // mirror horizontal, rotate 90 CW
				dx, dy = h-1-y, w-1-x
			case 8: // rotate 270 CW
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

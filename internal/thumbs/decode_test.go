package thumbs

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"

	"golang.org/x/image/tiff"
)

// The cache is what stops the grid handing a whole photograph to the webview
// for every tile. It only ever accepted JPEG bytes, so a frame in any other
// format failed to enter it and was served whole instead — which on a 228 MB
// TIFF means the tile never arrives at all.
//
// Anything the build can decode belongs in the cache. The formats here are the
// ones pure Go reads; HEIC and AVIF need a C library and are still passed
// through untouched.

func makeImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func makeTIFF(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, makeImage(w, h), nil); err != nil {
		t.Fatalf("encode tiff: %v", err)
	}
	return buf.Bytes()
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, makeImage(w, h)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestPutAcceptsATIFF(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.Put(key("aa"), SizeGrid, makeTIFF(t, 1200, 900))
	if err != nil {
		t.Fatalf("a TIFF must be cacheable, or the grid serves the whole file: %v", err)
	}

	// What lands in the cache is always a JPEG, whatever went in: that is what
	// the preview route promises the webview.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the cached thumbnail is not a JPEG: %v", err)
	}
	if got := img.Bounds().Dx(); got != int(SizeGrid) {
		t.Errorf("cached width %d, want the grid size %d", got, int(SizeGrid))
	}
}

func TestPutAcceptsAPNG(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(key("bb"), SizeGrid, makePNG(t, 800, 600)); err != nil {
		t.Fatalf("a PNG must be cacheable: %v", err)
	}
}

// A JPEG still goes in, which is the path every frame took before.
func TestPutStillAcceptsAJPEG(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(key("cc"), SizeGrid, makeJPEG(t, 1024, 768)); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// Bytes that are no image at all are still refused, rather than writing a
// broken entry the grid would then serve forever.
func TestPutRefusesSomethingThatIsNotAnImage(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(key("dd"), SizeGrid, []byte("this is not a photograph")); err == nil {
		t.Error("junk bytes were accepted into the cache")
	}
}

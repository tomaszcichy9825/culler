//go:build libraw

// Package rawdev develops a RAW file into full-resolution RGB pixels. This is
// the LibRaw-backed half, compiled only with `-tags libraw`; see
// rawdev_stub.go for the package documentation and for what an ordinary build
// gets instead.
//
// The C API is used rather than the C++ one because cgo speaks C: every call
// below returns a status code, and a non-zero code is turned into an error and
// returned. Nothing here dereferences a pointer LibRaw did not confirm, and
// the pixel buffer it hands back is bounds-checked against its own declared
// size before a single byte is read out of it.
//
// Building this needs LibRaw's headers and library on the system —
// `brew install libraw`, `apt install libraw-dev` — and, on Homebrew, an
// escape hatch: its libraw.pc advertises `-Xpreprocessor -fopenmp`, which is
// not on cgo's linker-flag allowlist, so the build wants
// CGO_LDFLAGS_ALLOW='-Xpreprocessor|-fopenmp' in the environment. `make
// build-libraw` sets it; a hand-rolled `go build -tags libraw` must too.
package rawdev

/*
#cgo pkg-config: libraw
#include <stdlib.h>
#include <libraw/libraw.h>
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"unsafe"
)

// ErrUnavailable exists in this build only so that callers compile against one
// API either way. It is never returned here.
var ErrUnavailable = errors.New("rawdev: built without LibRaw support (rebuild with -tags libraw)")

const (
	// developQuality is high because these bytes are what the user zooms into
	// to judge focus. The cache re-encodes at its own quality afterwards.
	developQuality = 92

	// maxPixels refuses an implausible frame rather than trying to allocate
	// for it. LibRaw reports dimensions as 16-bit, so a corrupt file can claim
	// 65535 square; 300 megapixels is comfortably past any real sensor.
	maxPixels = 300 << 20
)

// develops serialises LibRaw calls. The default (non-reentrant) LibRaw build
// is the one `pkg-config libraw` selects, and one develop of a 40MP frame
// holds several hundred megabytes of pixels at once — so a single develop at a
// time is both the safe choice and the kind one. Zoom is a deliberate,
// one-frame-at-a-time gesture; nothing here is on the grid's hot path.
var develops = make(chan struct{}, 1)

// Available reports whether Develop can do anything. True in this build.
func Available() bool { return true }

// Develop demosaics the RAW at path and returns it as full-resolution JPEG
// bytes, upright, in sRGB, with the camera's own white balance. Auto-brighten
// is off so the result sits where the sensor put it rather than drifting away
// from the embedded preview the user was just looking at.
func Develop(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("rawdev: empty path")
	}

	develops <- struct{}{}
	defer func() { <-develops }()

	// A context per call: LibRaw keeps all its state in this handle, and
	// closing it releases the unpacked sensor data along with everything else.
	h := C.libraw_init(0)
	if h == nil {
		return nil, errors.New("rawdev: libraw_init failed")
	}
	defer C.libraw_close(h)

	h.params.half_size = 0     // full resolution — the entire point of the tier
	h.params.use_camera_wb = 1 // as shot, not LibRaw's own guess
	h.params.output_color = 1  // sRGB, because nothing tags the bytes downstream
	h.params.output_bps = 8
	h.params.no_auto_bright = 1

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	if rc := C.libraw_open_file(h, cpath); rc != 0 {
		return nil, librawErr("open", rc)
	}
	if rc := C.libraw_unpack(h); rc != 0 {
		return nil, librawErr("unpack", rc)
	}
	if rc := C.libraw_dcraw_process(h); rc != 0 {
		return nil, librawErr("develop", rc)
	}

	var rc C.int
	img := C.libraw_dcraw_make_mem_image(h, &rc)
	if img == nil {
		return nil, librawErr("render", rc)
	}
	defer C.libraw_dcraw_clear_mem(img)

	rgba, err := toRGBA(img)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: developQuality}); err != nil {
		return nil, fmt.Errorf("rawdev: encode %s: %w", path, err)
	}
	return buf.Bytes(), nil
}

// toRGBA copies LibRaw's interleaved 8-bit RGB into a Go image. The copy is
// unavoidable — the C buffer dies with the handle — and it is also where every
// assumption about the buffer gets checked, since everything below this point
// is pointer arithmetic over memory Go did not allocate.
func toRGBA(img *C.libraw_processed_image_t) (*image.RGBA, error) {
	if img._type != C.LIBRAW_IMAGE_BITMAP {
		return nil, fmt.Errorf("rawdev: libraw returned format %d, want a bitmap", int(img._type))
	}
	if img.colors != 3 || img.bits != 8 {
		return nil, fmt.Errorf("rawdev: unsupported bitmap: %d colours at %d bits", int(img.colors), int(img.bits))
	}
	w, h := int(img.width), int(img.height)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("rawdev: empty bitmap %dx%d", w, h)
	}
	if w*h > maxPixels {
		return nil, fmt.Errorf("rawdev: implausible bitmap %dx%d", w, h)
	}
	want := w * h * 3
	if int(img.data_size) < want {
		return nil, fmt.Errorf("rawdev: short bitmap: %d bytes for %dx%d", int(img.data_size), w, h)
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(&img.data[0])), want)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, j := 0, 0; i < want; i, j = i+3, j+4 {
		dst.Pix[j] = src[i]
		dst.Pix[j+1] = src[i+1]
		dst.Pix[j+2] = src[i+2]
		dst.Pix[j+3] = 0xFF
	}
	return dst, nil
}

// librawErr turns a LibRaw status code into an error carrying LibRaw's own
// wording, which distinguishes an unsupported camera from a truncated file.
func librawErr(stage string, rc C.int) error {
	return fmt.Errorf("rawdev: %s: %s", stage, C.GoString(C.libraw_strerror(rc)))
}

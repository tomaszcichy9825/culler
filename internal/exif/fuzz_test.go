package exif

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// seeds are the shapes a parser meets on a real card, plus the degenerate ones
// it meets on a failing one.
func seeds() [][]byte {
	var out [][]byte
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		block := fullTIFF(order)
		out = append(out, block, jpegWith(block))
	}
	out = append(out,
		[]byte{},
		[]byte("II*\x00\x08\x00\x00\x00"),
		[]byte("MM\x00*\x00\x00\x00\x08"),
		[]byte("FUJIFILMCCD-RAW "),
		[]byte{0xFF, 0xD8, 0xFF, 0xD9},
		[]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0},
		jpegWith(nil),
	)
	return out
}

// Parse is fed whatever bytes happen to sit on the card. It may return an
// error for any of them; it may not panic, and it may not return fields it
// could not have read.
func FuzzParse(f *testing.F) {
	for _, s := range seeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fields, err := Parse(data)
		if err != nil {
			return
		}
		if fields.GPS.Present && (fields.GPS.Latitude > 90 || fields.GPS.Latitude < -90) {
			t.Fatalf("latitude %v is not on the planet", fields.GPS.Latitude)
		}
		if fields.GPS.Present && (fields.GPS.Longitude > 180 || fields.GPS.Longitude < -180) {
			t.Fatalf("longitude %v is not on the planet", fields.GPS.Longitude)
		}
		if fields.DateTimeOriginal.Present && fields.DateTimeOriginal.Value.IsZero() {
			t.Fatal("a present timestamp cannot be the zero time")
		}
		// Text comes out of the file, so it can never be longer than the file.
		for _, s := range []string{fields.Make.Value, fields.Model.Value, fields.LensModel.Value} {
			if len(s) > len(data) {
				t.Fatalf("returned %d characters from a %d byte input", len(s), len(data))
			}
		}
	})
}

// RewriteJPEG is fed the same bytes. Anything it agrees to write must still be
// a JPEG, must still hold the image data it was given, and must be re-readable
// by this package's own parser.
func FuzzRewriteJPEG(f *testing.F) {
	for _, s := range seeds() {
		f.Add(s)
	}
	when := time.Date(2026, 8, 3, 19, 42, 7, 370000000, time.UTC)
	artist := "Fuzz"

	f.Fuzz(func(t *testing.T, data []byte) {
		// Alternate between stripping GPS and setting it, so both the erase path
		// and the create-the-GPS-IFD path are hammered against arbitrary input.
		c := Changes{DateTimeOriginal: &when, Artist: &artist}
		if len(data) > 0 && data[0]%2 == 0 {
			c.StripGPS = true
		} else {
			c.SetGPS = &GPSCoord{Latitude: 51.5066667, Longitude: -0.1275, Altitude: 35.5, HasAltitude: true}
		}
		out, err := RewriteJPEG(data, c)
		if err != nil {
			if out != nil {
				t.Fatalf("returned %d bytes alongside error %v", len(out), err)
			}
			return
		}
		if !hasSOI(out) {
			t.Fatalf("produced %d bytes that are not a JPEG", len(out))
		}
		// The entropy-coded stream begins at SOS and is copied wholesale; if
		// the input had one, the output has the same one.
		if tail := afterSOS(data); tail != nil && !bytes.Contains(out, tail) {
			t.Fatal("the image data did not come through the rewrite")
		}
		if _, err := Parse(out); err != nil {
			t.Fatalf("the file this package wrote is one it cannot read: %v", err)
		}
	})
}

// afterSOS returns everything from the start-of-scan marker onwards, which is
// the part of a JPEG a metadata rewrite must never touch.
func afterSOS(data []byte) []byte {
	segs := segments(data)
	if len(segs) == 0 {
		return nil
	}
	end := segs[len(segs)-1].end
	if end >= len(data) {
		return nil
	}
	return data[end:]
}

package exif

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

// near reports whether two coordinates agree to about a metre, which is finer
// than a camera records and far finer than a dropped pin means.
func nearCoord(a, b float64) bool { return math.Abs(a-b) < 1e-5 }

func TestRewriteJPEGWritesGPSWhereThereWasNone(t *testing.T) {
	// A frame off a camera with no GPS: an IFD0 with a Make and an EXIF pointer,
	// and no GPS directory at all. Writing a location has to create one.
	b := newTIFF(binary.LittleEndian)
	exif := b.ifd([]tag{ascii(tagLensModel, "XF23mmF2 R WR")}, 0)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM"), b.long(tagExifIFD, exif)}, 0)
	bare := jpegWith(b.done(ifd0))

	after, err := RewriteJPEG(bare, Changes{SetGPS: &GPSCoord{
		Latitude:    51.5066667,
		Longitude:   -0.1275,
		Altitude:    35.5,
		HasAltitude: true,
	}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.GPS.Present {
		t.Fatalf("no GPS read back after writing one: %+v", f.GPS)
	}
	if !nearCoord(f.GPS.Latitude, 51.5066667) || !nearCoord(f.GPS.Longitude, -0.1275) {
		t.Errorf("coordinates = %.6f, %.6f; want 51.506667, -0.127500", f.GPS.Latitude, f.GPS.Longitude)
	}
	if !f.GPS.HasAltitude || math.Abs(f.GPS.Altitude-35.5) > 0.05 {
		t.Errorf("altitude = %v (has %v), want 35.5", f.GPS.Altitude, f.GPS.HasAltitude)
	}
	// The IFD that never had GPS keeps everything it did have.
	if f.Make.Value != "FUJIFILM" || f.LensModel.Value != "XF23mmF2 R WR" {
		t.Errorf("creating the GPS IFD lost an unrelated tag: %+v", f)
	}
}

func TestRewriteJPEGWritesSouthWestAndBelowSeaLevel(t *testing.T) {
	b := newTIFF(binary.BigEndian)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM")}, 0)
	bare := jpegWith(b.done(ifd0))

	after, err := RewriteJPEG(bare, Changes{SetGPS: &GPSCoord{
		Latitude:    -33.8688,
		Longitude:   151.2093,
		Altitude:    -12.5,
		HasAltitude: true,
	}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearCoord(f.GPS.Latitude, -33.8688) || f.GPS.Latitude >= 0 {
		t.Errorf("latitude = %.6f, want -33.8688 (S)", f.GPS.Latitude)
	}
	if !nearCoord(f.GPS.Longitude, 151.2093) {
		t.Errorf("longitude = %.6f, want 151.2093 (E)", f.GPS.Longitude)
	}
	if !f.GPS.HasAltitude || math.Abs(f.GPS.Altitude-(-12.5)) > 0.05 {
		t.Errorf("altitude = %v, want -12.5 (below sea level)", f.GPS.Altitude)
	}
}

func TestRewriteJPEGOverwritesExistingGPS(t *testing.T) {
	// fullTIFF already carries a position; writing a new one must replace it,
	// not read back the old coordinates.
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{SetGPS: &GPSCoord{
		Latitude:  48.8584,
		Longitude: 2.2945,
	}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearCoord(f.GPS.Latitude, 48.8584) || !nearCoord(f.GPS.Longitude, 2.2945) {
		t.Errorf("coordinates = %.6f, %.6f; want 48.8584, 2.2945", f.GPS.Latitude, f.GPS.Longitude)
	}
	// Overwriting GPS leaves the rest of the file alone.
	if f.Model.Value != "X-T5" || f.ISO.Value != 640 {
		t.Errorf("writing GPS damaged the rest: %+v", f)
	}
}

func TestSetGPSWithoutAltitudeClearsAStaleOne(t *testing.T) {
	// fullTIFF already carries an altitude. Writing a location that carries none
	// must take the old altitude off, not leave it paired with the new
	// coordinates — the same hazard the capture-time write guards against for a
	// leftover sub-second.
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{SetGPS: &GPSCoord{
		Latitude:    48.8584,
		Longitude:   2.2945,
		HasAltitude: false,
	}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.GPS.Present {
		t.Fatalf("the new coordinates did not read back: %+v", f.GPS)
	}
	if f.GPS.HasAltitude {
		t.Errorf("a stale altitude survived the new location: %v m", f.GPS.Altitude)
	}
}

func TestSetGPSTakesPrecedenceOverStrip(t *testing.T) {
	// If a caller somehow asks to both set and strip, the location the user
	// chose wins over the removal — losing a location the user just set is the
	// worse surprise.
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{
		SetGPS:   &GPSCoord{Latitude: 10, Longitude: 20},
		StripGPS: true,
	})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.GPS.Present || !nearCoord(f.GPS.Latitude, 10) || !nearCoord(f.GPS.Longitude, 20) {
		t.Errorf("SetGPS did not win over StripGPS: %+v", f.GPS)
	}
}

func TestRenderXMPCarriesGPS(t *testing.T) {
	xmp := string(RenderXMP(Changes{SetGPS: &GPSCoord{
		Latitude:    51.5066667,
		Longitude:   -0.1275,
		Altitude:    35.5,
		HasAltitude: true,
	}}))
	if !strings.Contains(xmp, "<exif:GPSLatitude>") || !strings.Contains(xmp, "<exif:GPSLongitude>") {
		t.Errorf("XMP carries no GPS: %s", xmp)
	}
	// The XMP form is "deg,decimalminuteH", so the hemisphere letters are there.
	if !strings.Contains(xmp, "N<") || !strings.Contains(xmp, "W<") {
		t.Errorf("XMP coordinates carry no hemisphere: %s", xmp)
	}
	if !strings.Contains(xmp, "<exif:GPSAltitude>") {
		t.Errorf("XMP carries no altitude: %s", xmp)
	}
}

func TestChangesWithGPSIsNotEmpty(t *testing.T) {
	if (Changes{SetGPS: &GPSCoord{Latitude: 1, Longitude: 2}}).Empty() {
		t.Error("a change that sets a location is not empty")
	}
}

package exif

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadJPEGEveryField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{"little endian", binary.LittleEndian},
		{"big endian", binary.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, "DSCF0001.JPG", jpegWith(fullTIFF(tc.order)))
			f, err := Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}

			if got := f.DateTimeOriginal.Value; !got.Equal(time.Date(2026, 8, 3, 19, 42, 7, 370000000, time.FixedZone("", 2*3600))) {
				t.Errorf("DateTimeOriginal = %s, want 2026-08-03 19:42:07.37 +02:00", got)
			}
			if !f.DateTimeOriginal.HasOffset || !f.DateTimeOriginal.HasSubSec {
				t.Errorf("sub-second and offset should both be recorded as present: %+v", f.DateTimeOriginal)
			}
			if f.Make.Value != "FUJIFILM" || f.Model.Value != "X-T5" {
				t.Errorf("Make/Model = %q/%q, want FUJIFILM/X-T5", f.Make.Value, f.Model.Value)
			}
			if f.LensModel.Value != "XF35mmF1.4 R" {
				t.Errorf("LensModel = %q", f.LensModel.Value)
			}
			if f.Artist.Value != "Old Artist" || f.Copyright.Value != "Old Copyright" {
				t.Errorf("Artist/Copyright = %q/%q", f.Artist.Value, f.Copyright.Value)
			}
			if f.ExposureTime.Num != 1 || f.ExposureTime.Den != 250 {
				t.Errorf("ExposureTime = %d/%d, want 1/250", f.ExposureTime.Num, f.ExposureTime.Den)
			}
			if got := f.FNumber.Float(); math.Abs(got-2.8) > 1e-9 {
				t.Errorf("FNumber = %v, want 2.8", got)
			}
			if f.ISO.Value != 640 {
				t.Errorf("ISO = %d, want 640", f.ISO.Value)
			}
			if got := f.FocalLength.Float(); got != 35 {
				t.Errorf("FocalLength = %v, want 35", got)
			}
			if f.Orientation.Value != 6 {
				t.Errorf("Orientation = %d, want 6", f.Orientation.Value)
			}
			if f.ImageWidth.Value != 6240 || f.ImageHeight.Value != 4160 {
				t.Errorf("dimensions = %d×%d, want 6240×4160", f.ImageWidth.Value, f.ImageHeight.Value)
			}

			if !f.GPS.Present {
				t.Fatal("GPS should be present")
			}
			// 51°30'24" N, 0°7'39" W
			if math.Abs(f.GPS.Latitude-51.506666666) > 1e-6 {
				t.Errorf("latitude = %v, want 51.5066…", f.GPS.Latitude)
			}
			if math.Abs(f.GPS.Longitude-(-0.1275)) > 1e-6 {
				t.Errorf("longitude = %v, want -0.1275", f.GPS.Longitude)
			}
			if !f.GPS.HasAltitude || math.Abs(f.GPS.Altitude-35.5) > 1e-9 {
				t.Errorf("altitude = %v (present %v), want 35.5", f.GPS.Altitude, f.GPS.HasAltitude)
			}
		})
	}
}

func TestReadMissingTagIsAbsentNotZero(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	ifd0 := b.ifd([]tag{ascii(tagMake, "NIKON")}, 0)
	path := writeTemp(t, "sparse.JPG", jpegWith(b.done(ifd0)))

	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !f.Make.Present {
		t.Error("Make was written and should be present")
	}
	if f.Model.Present || f.ISO.Present || f.FNumber.Present || f.DateTimeOriginal.Present || f.GPS.Present {
		t.Errorf("absent tags should not read back as zero values: %+v", f)
	}
	if f.ISO.Value != 0 {
		t.Errorf("an absent ISO still carries its zero value, %d", f.ISO.Value)
	}
}

func TestReadSouthWestGPSSigns(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	gps := b.ifd([]tag{
		ascii(tagGPSLatitudeRef, "S"),
		b.rational(tagGPSLatitude, [2]uint32{33, 1}, [2]uint32{51, 1}, [2]uint32{0, 1}),
		ascii(tagGPSLongitudeRef, "E"),
		b.rational(tagGPSLongitude, [2]uint32{151, 1}, [2]uint32{12, 1}, [2]uint32{0, 1}),
		bytes1(tagGPSAltitudeRef, 1),
		b.rational(tagGPSAltitude, [2]uint32{12, 1}),
	}, 0)
	ifd0 := b.ifd([]tag{b.long(tagGPSIFD, gps)}, 0)
	path := writeTemp(t, "sydney.JPG", jpegWith(b.done(ifd0)))

	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if math.Abs(f.GPS.Latitude-(-33.85)) > 1e-9 {
		t.Errorf("latitude = %v, want -33.85 (S is negative)", f.GPS.Latitude)
	}
	if math.Abs(f.GPS.Longitude-151.2) > 1e-9 {
		t.Errorf("longitude = %v, want 151.2", f.GPS.Longitude)
	}
	if f.GPS.Altitude != -12 {
		t.Errorf("altitude = %v, want -12 (ref 1 is below sea level)", f.GPS.Altitude)
	}
}

func TestReadRAWTIFFHeader(t *testing.T) {
	// A TIFF-based RAW is the same IFD structure with no JPEG wrapper.
	path := writeTemp(t, "DSCF0001.DNG", fullTIFF(binary.LittleEndian))
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.Model.Value != "X-T5" || f.ISO.Value != 640 {
		t.Errorf("RAW header did not parse: %+v", f)
	}
}

func TestReadRAFHeader(t *testing.T) {
	// Fujifilm RAF puts a whole JPEG, EXIF and all, at an offset named in a
	// fixed header slot.
	inner := jpegWith(fullTIFF(binary.BigEndian))
	raf := make([]byte, rafPreviewLengthPos+4)
	copy(raf, "FUJIFILMCCD-RAW ")
	binary.BigEndian.PutUint32(raf[rafPreviewOffsetPos:], uint32(len(raf)))
	binary.BigEndian.PutUint32(raf[rafPreviewLengthPos:], uint32(len(inner)))
	raf = append(raf, inner...)

	f, err := Read(writeTemp(t, "DSCF0001.RAF", raf))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.Make.Value != "FUJIFILM" || f.ISO.Value != 640 {
		t.Errorf("RAF EXIF did not parse: %+v", f)
	}
}

func TestReadRejectsUnknownContainer(t *testing.T) {
	if _, err := Read(writeTemp(t, "notes.txt", []byte("this is not a photograph"))); err == nil {
		t.Fatal("a text file should not parse as EXIF")
	}
}

func TestReadJPEGWithoutEXIF(t *testing.T) {
	f, err := Read(writeTemp(t, "bare.JPG", jpegWith(nil)))
	if err != nil {
		t.Fatalf("a JPEG with no APP1 is readable, just empty: %v", err)
	}
	if f.Make.Present || f.DateTimeOriginal.Present {
		t.Errorf("no EXIF should mean no fields: %+v", f)
	}
}

func TestShotTimeFallsBackToNothing(t *testing.T) {
	path := writeTemp(t, "DSCF0001.JPG", jpegWith(fullTIFF(binary.LittleEndian)))
	shot, ok := ShotTime(path)
	if !ok {
		t.Fatal("a frame with DateTimeOriginal should report a shot time")
	}
	if shot.Year() != 2026 || shot.Minute() != 42 {
		t.Errorf("shot = %s", shot)
	}
	if _, ok := ShotTime(writeTemp(t, "bare.JPG", jpegWith(nil))); ok {
		t.Error("a frame with no DateTimeOriginal has no shot time to report")
	}
}

// Atoi tolerates an inner sign, so "+-5:00" would sail through a parse built
// on it. The offset grammar is sign, two digits, colon, two digits — anything
// else is not an offset.
func TestParseOffsetRejectsMalformedValues(t *testing.T) {
	for _, s := range []string{"++5:00", "+-5:00", "+05:+0", "+0a:00", "+05:0b", "005:00", "+24:00", "+05:60"} {
		if _, ok := parseOffset(s); ok {
			t.Errorf("parseOffset(%q) accepted a malformed offset", s)
		}
	}
	zone, ok := parseOffset("-05:30")
	if !ok {
		t.Fatal("parseOffset rejected a well-formed offset")
	}
	if _, secs := time.Now().In(zone).Zone(); secs != -(5*3600 + 30*60) {
		t.Errorf("offset -05:30 parsed to %d seconds", secs)
	}
}

// A malformed offset tag must not turn the capture time into the zero time:
// the timestamp stays a wall clock and the zone stays honestly unknown.
func TestReadMalformedOffsetKeepsTheWallClock(t *testing.T) {
	for _, offset := range []string{"++5:00", "+-5:00", "+05:+0"} {
		t.Run(offset, func(t *testing.T) {
			b := newTIFF(binary.LittleEndian)
			exifIFD := b.ifd([]tag{
				ascii(tagDateTimeOriginal, "2026:08:03 19:42:07"),
				ascii(tagOffsetTimeOriginal, offset),
			}, 0)
			ifd0 := b.ifd([]tag{b.long(tagExifIFD, exifIFD)}, 0)

			f, err := Parse(jpegWith(b.done(ifd0)))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !f.DateTimeOriginal.Present {
				t.Fatal("the capture time is gone")
			}
			if f.DateTimeOriginal.Value.IsZero() {
				t.Fatal("a present timestamp cannot be the zero time")
			}
			want := time.Date(2026, 8, 3, 19, 42, 7, 0, time.UTC)
			if !f.DateTimeOriginal.Value.Equal(want) {
				t.Errorf("DateTimeOriginal = %s, want the wall clock %s", f.DateTimeOriginal.Value, want)
			}
			if f.DateTimeOriginal.HasOffset {
				t.Errorf("the malformed offset %q was treated as a recorded zone", offset)
			}
		})
	}
}

// Malformed containers must come back as errors or empty fields, never as a
// panic and never as a read past the end of the buffer.
func TestReadSurvivesTruncationAtEveryLength(t *testing.T) {
	full := jpegWith(fullTIFF(binary.LittleEndian))
	dir := t.TempDir()
	for n := 0; n <= len(full); n++ {
		path := filepath.Join(dir, "cut.JPG")
		if err := os.WriteFile(path, full[:n], 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err != nil {
			continue // an unparseable prefix is a fine answer
		}
	}
}

func TestReadSurvivesHostileOffsets(t *testing.T) {
	cases := map[string][]byte{
		"ifd0 past the end":      {'I', 'I', 42, 0, 0xFF, 0xFF, 0xFF, 0xFF},
		"absurd entry count":     {'I', 'I', 42, 0, 8, 0, 0, 0, 0xFF, 0xFF},
		"header only":            {'I', 'I', 42, 0},
		"empty":                  {},
		"exif pointer to itself": append([]byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x69, 0x87, 4, 0, 1, 0, 0, 0}, []byte{8, 0, 0, 0, 0, 0, 0, 0}...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, "hostile.DNG", data)
			if _, err := Read(path); err != nil {
				return
			}
		})
	}
}

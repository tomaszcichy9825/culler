// Package exif reads the metadata a camera writes into a frame, and writes a
// small, deliberately chosen subset of it back.
//
// # Reading
//
// Read walks JPEG APP1 segments and TIFF-based RAW headers with the same
// defensive discipline as internal/preview: every offset is bounds-checked
// before it is sliced, every loop is capped, an input it cannot make sense of
// is an error rather than a guess, and nothing here panics or reads past the
// end of the buffer whatever bytes happen to sit on the card. A tag that is
// not in the file is absent, not zero — every field carries its own presence
// flag, because "ISO 0" and "no ISO recorded" are different facts and the
// editor has to draw them differently.
//
// # Writing, and why it goes through the op engine
//
// This package writes metadata two ways, and never touches RAW image data:
//
//   - JPEG: RewriteJPEG rebuilds the APP1 segment and copies every other byte
//     of the file through untouched — the ICC profile, the quantisation and
//     Huffman tables, the entropy-coded stream, any XMP or IPTC segment, and
//     the EXIF thumbnail. Byte-for-byte identical outside APP1 is a tested
//     property, not an aspiration.
//   - RAW: RenderXMP produces a minimal, well-formed XMP sidecar. The RAW file
//     itself is never opened for writing.
//
// The APP1 rewrite moves as little as it can. Rather than re-serialising the
// whole TIFF block — which would relocate every value and break the offsets
// buried inside a MakerNote, a private tag no third party can safely rewrite —
// it does three things only:
//
//  1. A value that fits the span the old one occupied is overwritten in place.
//  2. A value that does not fit is appended to the end of the TIFF block and
//     the entry's offset field is repointed at it. Every other byte stays put.
//  3. An IFD that gains or loses an entry is rebuilt at the end of the block
//     and its parent pointer is repointed. The entries are copied verbatim,
//     offsets and all, so the values they name never move either. The old
//     copy of the IFD becomes unreachable padding.
//
// Stripping GPS removes the GPSInfo pointer from IFD0 and then zeroes the GPS
// IFD's own bytes and the value spans only it referenced — coordinates that
// merely became unreachable would still be sitting in the file for anyone with
// a hex editor, which is not what "strip GPS" means to the person asking.
//
// # Undo
//
// Nothing in this package is applied directly by the UI. internal/app's
// ExifService stages the rewritten bytes in the application data directory and
// then hands the op engine two ordinary actions per file:
//
//	move  <original>  ->  <app data>/backup/<date>/<name>
//	copy  <staged>    ->  <original>
//
// That is the whole trick, and both halves are chosen for how they undo. The
// existing Executor replays a batch backwards: it reverses a copy by removing
// the file the copy created, and a move by putting the file back where it came
// from unless something new occupies that place. So undo removes the edited
// frame, which frees the original path, and then moves the untouched backup
// into it. An in-place edit needs no new verb, no change to internal/ops and
// no special case in the journal, and the restored file is byte-identical
// because it is the same file, never rewritten.
//
// A frame whose sidecar did not exist yet gets the copy alone: there is no
// original to keep, and undoing a copy is exactly "remove what was created".
package exif

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// The tags this package reads and writes. Everything else in a file is carried
// through a rewrite untouched and never interpreted.
const (
	// IFD0
	tagImageWidth  = 0x0100
	tagImageLength = 0x0101
	tagMake        = 0x010F
	tagModel       = 0x0110
	tagOrientation = 0x0112
	tagArtist      = 0x013B
	tagCopyright   = 0x8298
	tagExifIFD     = 0x8769
	tagGPSIFD      = 0x8825

	// EXIF IFD
	tagExposureTime       = 0x829A
	tagFNumber            = 0x829D
	tagISOSpeedRatings    = 0x8827
	tagOffsetTimeOriginal = 0x9011
	tagDateTimeOriginal   = 0x9003
	tagMakerNote          = 0x927C
	tagSubSecTimeOriginal = 0x9291
	tagFocalLength        = 0x920A
	tagPixelXDimension    = 0xA002
	tagPixelYDimension    = 0xA003
	tagLensModel          = 0xA434

	// GPS IFD
	tagGPSLatitudeRef  = 0x0001
	tagGPSLatitude     = 0x0002
	tagGPSLongitudeRef = 0x0003
	tagGPSLongitude    = 0x0004
	tagGPSAltitudeRef  = 0x0005
	tagGPSAltitude     = 0x0006
)

// How much of a file is read looking for metadata. EXIF lives in the first
// segments of a JPEG and the first IFDs of a RAW; reading a 60 MB RAW in full
// to find 8 KB of tags at the front would make opening a card unbearable.
const headerBudget = 4 << 20

// ErrUnsupported is returned for an input that is not a container this package
// recognises.
var ErrUnsupported = errors.New("exif: unrecognised container format")

// Text is a string-valued tag and whether the file carried it.
type Text struct {
	Value   string `json:"value"`
	Present bool   `json:"present"`
}

// Number is an integer-valued tag and whether the file carried it.
type Number struct {
	Value   int64 `json:"value"`
	Present bool  `json:"present"`
}

// Rational is a tag written as a fraction, kept as one: 1/250 is what the
// camera recorded and 0.004 is a lossy rendering of it.
type Rational struct {
	Num     int64 `json:"num"`
	Den     int64 `json:"den"`
	Present bool  `json:"present"`
}

// Float is the fraction's value, and 0 for a denominator of zero — which some
// bodies write for "unknown" and which must not divide.
func (r Rational) Float() float64 {
	if r.Den == 0 {
		return 0
	}
	return float64(r.Num) / float64(r.Den)
}

// Timestamp is DateTimeOriginal with the two tags that qualify it. The sub-
// second and offset tags are optional and recorded separately, because a
// frame whose zone is unknown must not be presented as if it were UTC.
type Timestamp struct {
	Value     time.Time `json:"value"`
	Present   bool      `json:"present"`
	HasSubSec bool      `json:"hasSubSec"`
	HasOffset bool      `json:"hasOffset"`
	// Offset as written, e.g. "+02:00". Empty when the file did not say.
	Offset string `json:"offset"`
}

// GPS is a position, already converted out of the degrees/minutes/seconds
// rationals and signed by its hemisphere reference.
type GPS struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Altitude    float64 `json:"altitude"`
	Present     bool    `json:"present"`
	HasAltitude bool    `json:"hasAltitude"`
}

// Fields is everything this package extracts from one frame.
type Fields struct {
	DateTimeOriginal Timestamp `json:"dateTimeOriginal"`
	Make             Text      `json:"make"`
	Model            Text      `json:"model"`
	LensModel        Text      `json:"lensModel"`
	Artist           Text      `json:"artist"`
	Copyright        Text      `json:"copyright"`
	ExposureTime     Rational  `json:"exposureTime"`
	FNumber          Rational  `json:"fNumber"`
	FocalLength      Rational  `json:"focalLength"`
	ISO              Number    `json:"iso"`
	Orientation      Number    `json:"orientation"`
	ImageWidth       Number    `json:"imageWidth"`
	ImageHeight      Number    `json:"imageHeight"`
	GPS              GPS       `json:"gps"`
}

// Read extracts the metadata of the file at path. A file whose container is
// recognised but which carries no EXIF at all reads back as empty Fields
// rather than an error: a JPEG straight out of an editor is a normal thing to
// open, not a failure.
func Read(path string) (Fields, error) {
	data, err := readHeader(path)
	if err != nil {
		return Fields{}, err
	}
	return Parse(data)
}

// Parse extracts metadata from the head of a file already in memory.
func Parse(data []byte) (Fields, error) {
	block, ok := tiffBlock(data)
	if !ok {
		return Fields{}, ErrUnsupported
	}
	if block == nil {
		return Fields{}, nil // recognised container, no EXIF in it
	}
	r, ifd0Off, ok := newReader(block)
	if !ok {
		return Fields{}, nil
	}
	ifd0, ok := r.readIFD(ifd0Off)
	if !ok {
		return Fields{}, nil
	}
	return r.fields(ifd0), nil
}

// ShotTime is DateTimeOriginal as a whole time, for callers that want the one
// field and not the rest. It reports whether the file actually carried it, so
// a caller can fall back to the modification time rather than to the zero
// time — which would sort every unreadable frame to the year 1.
func ShotTime(path string) (time.Time, bool) {
	f, err := Read(path)
	if err != nil || !f.DateTimeOriginal.Present {
		return time.Time{}, false
	}
	return f.DateTimeOriginal.Value, true
}

// readHeader reads the front of a file, capped: metadata lives at the start of
// every container this package handles.
func readHeader(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Read until the budget or the end of the file, whichever comes first. A
	// single Read is not enough: it may return a short count for no reason at
	// all, and a RAW clipped below where its EXIF sits would read as a frame
	// with no metadata rather than as the read error it is.
	return io.ReadAll(io.LimitReader(f, headerBudget))
}

// fields pulls every extracted tag out of the three directories that carry
// them. A directory that is missing or unparseable simply contributes nothing.
func (r *reader) fields(ifd0 *directory) Fields {
	var f Fields

	exif, _ := r.readIFD(r.pointer(ifd0, tagExifIFD))
	gps, _ := r.readIFD(r.pointer(ifd0, tagGPSIFD))

	f.Make = r.textOf(ifd0, tagMake)
	f.Model = r.textOf(ifd0, tagModel)
	f.Artist = r.textOf(ifd0, tagArtist)
	f.Copyright = r.textOf(ifd0, tagCopyright)
	f.Orientation = r.numberOf(ifd0, tagOrientation)

	f.LensModel = r.textOf(exif, tagLensModel)
	f.ExposureTime = r.rationalOf(exif, tagExposureTime)
	f.FNumber = r.rationalOf(exif, tagFNumber)
	f.FocalLength = r.rationalOf(exif, tagFocalLength)
	f.ISO = r.numberOf(exif, tagISOSpeedRatings)

	// The EXIF IFD's pixel dimensions describe the image as encoded, which is
	// what the user is looking at; IFD0's are the fallback for the containers
	// that only write them there.
	f.ImageWidth = firstPresent(r.numberOf(exif, tagPixelXDimension), r.numberOf(ifd0, tagImageWidth))
	f.ImageHeight = firstPresent(r.numberOf(exif, tagPixelYDimension), r.numberOf(ifd0, tagImageLength))

	f.DateTimeOriginal = r.timestamp(exif)
	f.GPS = r.gps(gps)
	return f
}

func firstPresent(vals ...Number) Number {
	for _, v := range vals {
		if v.Present {
			return v
		}
	}
	return Number{}
}

func (r *reader) textOf(d *directory, tag uint16) Text {
	e, ok := d.get(tag)
	if !ok {
		return Text{}
	}
	s, ok := r.text(e)
	if !ok {
		return Text{}
	}
	return Text{Value: s, Present: true}
}

func (r *reader) numberOf(d *directory, tag uint16) Number {
	e, ok := d.get(tag)
	if !ok {
		return Number{}
	}
	vals := r.ints(e)
	if len(vals) == 0 {
		return Number{}
	}
	return Number{Value: vals[0], Present: true}
}

func (r *reader) rationalOf(d *directory, tag uint16) Rational {
	e, ok := d.get(tag)
	if !ok {
		return Rational{}
	}
	vals := r.rationals(e)
	if len(vals) == 0 {
		return Rational{}
	}
	return vals[0]
}

// timestamp assembles DateTimeOriginal from its three tags. The date is parsed
// in the zone the file names; with no offset tag it is parsed as a wall clock
// in UTC and HasOffset says the zone is unknown, so nobody mistakes the
// rendering for a claim.
func (r *reader) timestamp(exif *directory) Timestamp {
	raw := r.textOf(exif, tagDateTimeOriginal)
	if !raw.Present {
		return Timestamp{}
	}
	when, err := time.ParseInLocation(exifTimeLayout, strings.TrimSpace(raw.Value), time.UTC)
	if err != nil {
		return Timestamp{}
	}
	// A body whose clock was never set writes zeros, and a corrupt file can
	// land on the same instant by accident. Either way the zero time is what a
	// caller uses to mean "no time at all", so it cannot also mean a real one.
	if when.IsZero() {
		return Timestamp{}
	}

	ts := Timestamp{Value: when, Present: true}
	if off := r.textOf(exif, tagOffsetTimeOriginal); off.Present {
		if zone, ok := parseOffset(off.Value); ok {
			ts.Value = when.In(zone)
			// ParseInLocation read the wall clock as UTC; re-read it in the
			// zone the file names so the instant, not just the label, is right.
			ts.Value, _ = time.Parse(exifTimeLayout+" -07:00", strings.TrimSpace(raw.Value)+" "+off.Value)
			ts.HasOffset = true
			ts.Offset = off.Value
		}
	}
	if sub := r.textOf(exif, tagSubSecTimeOriginal); sub.Present {
		if frac, ok := parseSubSec(sub.Value); ok {
			ts.Value = ts.Value.Add(frac)
			ts.HasSubSec = true
		}
	}
	return ts
}

// exifTimeLayout is the fixed "YYYY:MM:DD HH:MM:SS" every body writes.
const exifTimeLayout = "2006:01:02 15:04:05"

// parseOffset turns "+02:00" into a fixed zone.
func parseOffset(s string) (*time.Location, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return nil, false
	}
	hours, err1 := strconv.Atoi(s[1:3])
	mins, err2 := strconv.Atoi(s[4:6])
	if err1 != nil || err2 != nil || hours > 23 || mins > 59 {
		return nil, false
	}
	secs := hours*3600 + mins*60
	if s[0] == '-' {
		secs = -secs
	}
	return time.FixedZone("", secs), true
}

// parseSubSec turns the digits after the decimal point into a duration. "37"
// is 370 milliseconds, not 37 of anything.
func parseSubSec(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 9 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	digits, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	scale := math.Pow10(9 - len(s))
	return time.Duration(float64(digits) * scale), true
}

// gps converts the degrees/minutes/seconds triples into signed decimal
// degrees. A position needs both coordinates and both hemisphere references;
// half a position is no position.
func (r *reader) gps(d *directory) GPS {
	if d == nil {
		return GPS{}
	}
	lat, latOK := r.coordinate(d, tagGPSLatitude, tagGPSLatitudeRef, "S")
	lon, lonOK := r.coordinate(d, tagGPSLongitude, tagGPSLongitudeRef, "W")
	if !latOK || !lonOK {
		return GPS{}
	}
	if math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return GPS{}
	}
	out := GPS{Latitude: lat, Longitude: lon, Present: true}

	if alt := r.rationalOf(d, tagGPSAltitude); alt.Present && alt.Den != 0 {
		out.Altitude = alt.Float()
		out.HasAltitude = true
		// Reference 1 means below sea level. It is a BYTE, not a string.
		if ref := r.numberOf(d, tagGPSAltitudeRef); ref.Present && ref.Value == 1 {
			out.Altitude = -out.Altitude
		}
	}
	return out
}

// coordinate reads one degrees/minutes/seconds triple and its reference,
// negating when the reference is the negative hemisphere.
func (r *reader) coordinate(d *directory, tag, refTag uint16, negative string) (float64, bool) {
	e, ok := d.get(tag)
	if !ok {
		return 0, false
	}
	parts := r.rationals(e)
	if len(parts) < 3 {
		return 0, false
	}
	ref := r.textOf(d, refTag)
	if !ref.Present {
		return 0, false
	}
	for _, p := range parts[:3] {
		if p.Den == 0 {
			return 0, false
		}
	}
	deg := parts[0].Float() + parts[1].Float()/60 + parts[2].Float()/3600
	if strings.EqualFold(ref.Value, negative) {
		deg = -deg
	}
	return deg, true
}

// --- containers -------------------------------------------------------------

// exifHeader is the six bytes that mark an APP1 segment as EXIF rather than
// XMP, which shares the marker.
const exifHeader = "Exif\x00\x00"

// Fixed RAF header slots holding the embedded JPEG's position, both big-endian
// regardless of the sensor data's byte order.
const (
	rafMagic            = "FUJIFILM"
	rafPreviewOffsetPos = 84
	rafPreviewLengthPos = 88
	minJPEGSize         = 4
)

// tiffBlock finds the TIFF block to walk. The second return says whether the
// container was recognised at all; a recognised container with no metadata
// yields a nil block and no error, which is a normal file rather than a bad
// one.
func tiffBlock(data []byte) ([]byte, bool) {
	switch {
	case hasSOI(data):
		return firstEXIFSegment(data), true
	case isRAF(data):
		inner, ok := rafJPEG(data)
		if !ok {
			return nil, true
		}
		return firstEXIFSegment(inner), true
	}
	if _, _, ok := newReader(data); ok {
		return data, true
	}
	return nil, false
}

func hasSOI(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8
}

func isRAF(data []byte) bool {
	return len(data) >= len(rafMagic) && string(data[:len(rafMagic)]) == rafMagic
}

func rafJPEG(data []byte) ([]byte, bool) {
	if len(data) < rafPreviewLengthPos+4 {
		return nil, false
	}
	off := uint64(binary.BigEndian.Uint32(data[rafPreviewOffsetPos:]))
	length := uint64(binary.BigEndian.Uint32(data[rafPreviewLengthPos:]))
	if length < minJPEGSize || off < rafPreviewLengthPos+4 || off+length > uint64(len(data)) {
		return nil, false
	}
	return data[off : off+length], true
}

// firstEXIFSegment returns the TIFF payload of a JPEG's EXIF APP1, or nil.
func firstEXIFSegment(data []byte) []byte {
	for _, seg := range segments(data) {
		if seg.marker != 0xE1 {
			continue
		}
		if len(seg.payload) > len(exifHeader) && string(seg.payload[:len(exifHeader)]) == exifHeader {
			return seg.payload[len(exifHeader):]
		}
	}
	return nil
}

// jpegSegment is one marker segment: where it starts in the file, its marker,
// and its payload after the two length bytes.
type jpegSegment struct {
	marker  byte
	at      int // offset of the 0xFF that opens the segment
	end     int // one past the last byte of the segment
	payload []byte
}

// How many marker segments are walked before the file is assumed corrupt.
const maxSegments = 4096

// segments walks a JPEG's marker structure up to the start of the entropy-
// coded stream, which is where metadata stops. A file that loses its marker
// structure stops the walk rather than guessing where the next one might be.
func segments(data []byte) []jpegSegment {
	if !hasSOI(data) {
		return nil
	}
	var out []jpegSegment
	for i := 2; i+4 <= len(data) && len(out) < maxSegments; {
		if data[i] != 0xFF {
			break
		}
		marker := data[i+1]
		switch {
		case marker == 0xFF:
			i++ // fill byte before the real marker
			continue
		case marker == 0x01 || marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2 // standalone marker, no payload
			continue
		case marker == 0xDA || marker == 0xD9:
			return out // entropy-coded data or end of image
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		if length < 2 || i+2+length > len(data) {
			break
		}
		out = append(out, jpegSegment{
			marker:  marker,
			at:      i,
			end:     i + 2 + length,
			payload: data[i+4 : i+2+length],
		})
		i += 2 + length
	}
	return out
}

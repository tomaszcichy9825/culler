package exif

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// app1Span returns the offsets of the EXIF APP1 segment, so a test can assert
// that everything either side of it came through untouched.
func app1Span(t *testing.T, data []byte) (start, end int) {
	t.Helper()
	for _, seg := range segments(data) {
		if seg.marker == 0xE1 && len(seg.payload) > len(exifHeader) &&
			string(seg.payload[:len(exifHeader)]) == exifHeader {
			return seg.at, seg.end
		}
	}
	t.Fatal("no EXIF APP1 segment")
	return 0, 0
}

func TestRewriteJPEGLeavesEveryOtherByteAlone(t *testing.T) {
	before := jpegWith(fullTIFF(binary.LittleEndian))
	after, err := RewriteJPEG(before, Changes{Artist: ptr("Tomasz Cichy")})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	bs, be := app1Span(t, before)
	as, ae := app1Span(t, after)
	if bs != as {
		t.Fatalf("APP1 moved: was at %d, now at %d", bs, as)
	}
	if !bytes.Equal(before[:bs], after[:as]) {
		t.Error("bytes before the APP1 segment changed")
	}
	if !bytes.Equal(before[be:], after[ae:]) {
		t.Error("bytes after the APP1 segment changed — the image stream or ICC profile was touched")
	}
	// The entropy-coded stream is the thing that must never move.
	if !bytes.Contains(after, []byte{0x12, 0x34, 0x56, 0x78, 0x9A}) {
		t.Error("the image data is gone")
	}
}

func TestRewriteJPEGRoundTripsEveryWritableTag(t *testing.T) {
	when := time.Date(2026, 8, 3, 19, 42, 7, 370000000, time.FixedZone("", 2*3600))
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{
		DateTimeOriginal: &CaptureTime{Value: when, HasOffset: true},
		Artist:           ptr("Tomasz Cichy"),
		Copyright:        ptr("© 2026 Tomasz Cichy"),
	})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Artist.Value != "Tomasz Cichy" {
		t.Errorf("Artist = %q", f.Artist.Value)
	}
	if f.Copyright.Value != "© 2026 Tomasz Cichy" {
		t.Errorf("Copyright = %q", f.Copyright.Value)
	}
	if !f.DateTimeOriginal.Value.Equal(when) {
		t.Errorf("DateTimeOriginal = %s, want %s", f.DateTimeOriginal.Value, when)
	}
	if !f.DateTimeOriginal.HasOffset || f.DateTimeOriginal.Offset != "+02:00" {
		t.Errorf("offset = %q (present %v), want +02:00", f.DateTimeOriginal.Offset, f.DateTimeOriginal.HasOffset)
	}
	if !f.DateTimeOriginal.HasSubSec {
		t.Error("sub-second should have been written alongside the time")
	}
	// Everything not named in the changes is still there.
	if f.Model.Value != "X-T5" || f.ISO.Value != 640 || !f.GPS.Present {
		t.Errorf("an unrelated tag was lost: %+v", f)
	}
}

// A frame whose file never recorded a zone must not gain one from an edit:
// writing a zone-unknown time writes no OffsetTimeOriginal at all, and the
// time reads back with the zone still honestly unknown.
func TestRewriteJPEGDoesNotInventAZone(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	exifIFD := b.ifd([]tag{ascii(tagDateTimeOriginal, "2020:01:01 00:00:00")}, 0)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM"), b.long(tagExifIFD, exifIFD)}, 0)
	before := jpegWith(b.done(ifd0))

	when := time.Date(2026, 8, 5, 10, 11, 12, 0, time.UTC)
	after, err := RewriteJPEG(before, Changes{DateTimeOriginal: &CaptureTime{Value: when}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.DateTimeOriginal.Value.Equal(when) {
		t.Errorf("DateTimeOriginal = %s, want %s", f.DateTimeOriginal.Value, when)
	}
	if f.DateTimeOriginal.HasOffset {
		t.Errorf("an offset %q was invented for a frame that never had one", f.DateTimeOriginal.Offset)
	}
	for _, e := range directoryOf(t, after, tagExifIFD).entries {
		if e.tag == tagOffsetTimeOriginal {
			t.Error("OffsetTimeOriginal was written for a zone-unknown time")
		}
	}
}

// The reverse hazard: a file that does carry an offset, edited to a time whose
// zone is unknown, must lose the old offset rather than keep a zone that now
// qualifies a time nobody stated it for.
func TestRewriteJPEGRemovesAStaleOffsetForAZoneUnknownTime(t *testing.T) {
	when := time.Date(2026, 8, 5, 10, 11, 12, 0, time.UTC)
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)),
		Changes{DateTimeOriginal: &CaptureTime{Value: when}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.DateTimeOriginal.HasOffset {
		t.Errorf("the old offset %q survived a zone-unknown edit", f.DateTimeOriginal.Offset)
	}
	if !f.DateTimeOriginal.Value.Equal(when) {
		t.Errorf("DateTimeOriginal = %s, want %s", f.DateTimeOriginal.Value, when)
	}
}

func TestRenderXMPWritesNoZoneForAZoneUnknownTime(t *testing.T) {
	when := time.Date(2026, 8, 5, 10, 11, 12, 0, time.UTC)
	doc := string(RenderXMP(Changes{DateTimeOriginal: &CaptureTime{Value: when}}))
	if !strings.Contains(doc, ">2026-08-05T10:11:12<") {
		t.Errorf("the wall clock is missing or carries a zone:\n%s", doc)
	}
	if strings.Contains(doc, "10:11:12Z") || strings.Contains(doc, "10:11:12+00:00") {
		t.Errorf("a zone was invented in the sidecar:\n%s", doc)
	}
}

// A MakerNote is a private blob full of offsets relative to the TIFF header.
// The rewrite must not relocate it, or every camera vendor's own tool stops
// being able to read it.
func TestRewriteJPEGDoesNotMoveTheMakerNote(t *testing.T) {
	before := jpegWith(fullTIFF(binary.LittleEndian))
	after, err := RewriteJPEG(before, Changes{Artist: ptr("a considerably longer artist string than before")})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	want := bytes.Index(before, []byte("MAKERNOTE-PRIVATE-BYTES"))
	got := bytes.Index(after, []byte("MAKERNOTE-PRIVATE-BYTES"))
	if want < 0 {
		t.Fatal("fixture has no MakerNote")
	}
	if got != want {
		t.Errorf("MakerNote moved from %d to %d", want, got)
	}
}

func TestRewriteJPEGAddsATagTheFileNeverHad(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	exif := b.ifd([]tag{ascii(tagLensModel, "XF23mmF2 R WR")}, 0)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM"), b.long(tagExifIFD, exif)}, 0)
	bare := jpegWith(b.done(ifd0))

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	after, err := RewriteJPEG(bare, Changes{
		Artist:           ptr("Someone"),
		Copyright:        ptr("Rights"),
		DateTimeOriginal: &CaptureTime{Value: when, HasOffset: true},
	})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Artist.Value != "Someone" || f.Copyright.Value != "Rights" {
		t.Errorf("added tags did not read back: %+v", f)
	}
	if !f.DateTimeOriginal.Present || !f.DateTimeOriginal.Value.Equal(when) {
		t.Errorf("DateTimeOriginal = %+v, want %s", f.DateTimeOriginal, when)
	}
	if f.Make.Value != "FUJIFILM" || f.LensModel.Value != "XF23mmF2 R WR" {
		t.Errorf("rebuilding an IFD lost a tag it already had: %+v", f)
	}
}

func TestRewriteJPEGClearsATagWithAnEmptyValue(t *testing.T) {
	after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{Artist: ptr("")})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Artist.Present {
		t.Errorf("Artist should be gone, got %q", f.Artist.Value)
	}
	if f.Copyright.Value != "Old Copyright" {
		t.Errorf("clearing one tag cleared another: %q", f.Copyright.Value)
	}
}

// Replacing a string must clear the old value's bytes whichever way the new
// one is stored — in the entry itself, over the old span, or appended at the
// end of the block. A name shortened to an initial that leaves the full name
// readable in a hex editor has not been replaced, the same way GPS that is
// merely unhooked has not been stripped.
func TestPatchClearsTheOldValueInEveryBranch(t *testing.T) {
	cases := []struct {
		name   string
		artist string // the replacement; "Old Artist" is what fullTIFF carries
	}{
		{name: "shrinks into the entry", artist: "T"},
		{name: "fits the old span", artist: "New Art"},
		{name: "grows past the old span", artist: "a considerably longer artist string than before"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after, err := RewriteJPEG(jpegWith(fullTIFF(binary.LittleEndian)), Changes{Artist: ptr(tc.artist)})
			if err != nil {
				t.Fatalf("RewriteJPEG: %v", err)
			}
			if bytes.Contains(after, []byte("Old Artist")) {
				t.Error("the replaced value is still sitting in the file")
			}
			f, err := Parse(after)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if f.Artist.Value != tc.artist {
				t.Errorf("Artist = %q, want %q", f.Artist.Value, tc.artist)
			}
			if f.Copyright.Value != "Old Copyright" || f.Model.Value != "X-T5" {
				t.Errorf("clearing the old value damaged an unrelated tag: %+v", f)
			}
		})
	}
}

func TestRewriteJPEGStripsGPSFromTheBytesNotJustThePointer(t *testing.T) {
	before := jpegWith(fullTIFF(binary.BigEndian))
	if !bytes.Contains(before, []byte("N\x00")) {
		t.Fatal("fixture has no latitude reference")
	}
	after, err := RewriteJPEG(before, Changes{StripGPS: true})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.GPS.Present {
		t.Errorf("GPS still reads back: %+v", f.GPS)
	}
	// 51°30' as big-endian rationals: the coordinate bytes themselves must be
	// gone, not merely unreachable.
	latitude := []byte{0, 0, 0, 51, 0, 0, 0, 1, 0, 0, 0, 30, 0, 0, 0, 1}
	if bytes.Contains(after, latitude) {
		t.Error("the coordinates are still sitting in the file")
	}
	if f.Model.Value != "X-T5" || f.ISO.Value != 640 {
		t.Errorf("stripping GPS damaged the rest: %+v", f)
	}
}

// A corrupt or hostile file can aim the Exif and GPS pointers at the same
// directory. Stripping GPS must unhook the pointer without shooting the
// still-reachable directory — and the capture time inside it — out from under
// the Exif chain.
func TestStripGPSLeavesAnAliasedDirectoryAlone(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	shared := b.ifd([]tag{ascii(tagDateTimeOriginal, "2026:08:03 19:42:07")}, 0)
	ifd0 := b.ifd([]tag{
		ascii(tagMake, "FUJIFILM"),
		b.long(tagExifIFD, shared),
		b.long(tagGPSIFD, shared),
	}, 0)

	after, err := RewriteJPEG(jpegWith(b.done(ifd0)), Changes{StripGPS: true})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	f, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.DateTimeOriginal.Present {
		t.Error("stripping GPS wiped the capture time out of the aliased directory")
	}
	if f.GPS.Present {
		t.Errorf("GPS still reads back: %+v", f.GPS)
	}
}

// A GPS pointer aimed into the middle of a MakerNote — corruption, or malice —
// must not have the erase wipe MakerNote bytes the Exif chain still references.
func TestStripGPSAimedIntoTheMakerNoteLeavesItAlone(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	// A private blob that happens to contain an IFD-shaped run at offset 4:
	// one SHORT entry and a zero next pointer.
	note := []byte{
		'P', 'R', 'I', 'V',
		1, 0, // one entry
		0x01, 0x00, 3, 0, 1, 0, 0, 0, 0x2A, 0, 0, 0, // tag 1, SHORT, count 1
		0, 0, 0, 0, // next pointer
		'T', 'A', 'I', 'L',
	}
	noteAt := b.blob(note)
	ptr := make([]byte, 4)
	binary.LittleEndian.PutUint32(ptr, noteAt)
	exifIFD := b.ifd([]tag{
		ascii(tagLensModel, "XF35mmF1.4 R"),
		{id: tagMakerNote, typ: typeUndefined, count: uint32(len(note)), data: ptr},
	}, 0)
	ifd0 := b.ifd([]tag{
		ascii(tagMake, "FUJIFILM"),
		b.long(tagExifIFD, exifIFD),
		b.long(tagGPSIFD, noteAt+4),
	}, 0)

	after, err := RewriteJPEG(jpegWith(b.done(ifd0)), Changes{StripGPS: true})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	if !bytes.Contains(after, note) {
		t.Error("stripping GPS wiped live MakerNote bytes")
	}
}

// patch's zero-fill has the same reachability guard, and it has to see the
// chains that hang off the Interoperability pointer and the SubIFDs list, or
// growing a value can wipe bytes those directories still reference.
func TestPatchLeavesInteropAndSubIFDValuesAlone(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	interop := b.ifd([]tag{ascii(1, "InteropIndexR98")}, 0)
	subA := b.ifd([]tag{ascii(tagModel, "First SubIFD Value")}, 0)
	subB := b.ifd([]tag{ascii(tagModel, "Second SubIFD Value")}, 0)
	exifIFD := b.ifd([]tag{
		ascii(tagLensModel, "XF35mmF1.4 R"),
		b.long(tagInteropIFD, interop),
	}, 0)
	ifd0 := b.ifd([]tag{
		ascii(tagMake, "FUJIFILM"),
		// Artist and Copyright deliberately point at the very bytes the second
		// sub-IFD's and the Interoperability directory's values occupy: entries
		// sharing a span, which the guard can only see by walking those chains.
		{id: tagArtist, typ: typeASCII, count: 20, data: mustPointAt(b, "Second SubIFD Value\x00")},
		{id: tagCopyright, typ: typeASCII, count: 16, data: mustPointAt(b, "InteropIndexR98\x00")},
		b.long(tagExifIFD, exifIFD),
		b.long(tagSubIFDs, subA, subB),
	}, 0)

	after, err := RewriteJPEG(jpegWith(b.done(ifd0)), Changes{
		Artist:    ptr("a considerably longer artist string than before"),
		Copyright: ptr("a considerably longer copyright string than before"),
	})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	if !bytes.Contains(after, []byte("Second SubIFD Value")) {
		t.Error("replacing the artist zeroed a value the SubIFDs chain still references")
	}
	if !bytes.Contains(after, []byte("InteropIndexR98")) {
		t.Error("replacing the copyright zeroed a value the Interoperability chain still references")
	}
}

// mustPointAt returns the 4-byte offset of a value already in the block, for
// building an entry that aliases another entry's bytes.
func mustPointAt(b *tiffBuilder, value string) []byte {
	at := bytes.Index(b.buf, []byte(value))
	if at < 0 {
		panic("fixture value not found: " + value)
	}
	out := make([]byte, 4)
	b.order.PutUint32(out, uint32(at))
	return out
}

// Stripping GPS from a TIFF whose IFD0 holds nothing but the GPS pointer would
// leave an empty directory, which no reader accepts. The refusal must say so
// rather than blame the 64 KB segment limit for a 44-byte block.
func TestStripGPSFromAPointerOnlyIFD0SaysWhyItRefuses(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	gps := b.ifd([]tag{
		ascii(tagGPSLatitudeRef, "N"),
		b.rational(tagGPSLatitude, [2]uint32{51, 1}, [2]uint32{30, 1}, [2]uint32{0, 1}),
	}, 0)
	ifd0 := b.ifd([]tag{b.long(tagGPSIFD, gps)}, 0)

	_, err := RewriteJPEG(jpegWith(b.done(ifd0)), Changes{StripGPS: true})
	if err == nil {
		t.Fatal("an IFD0 left with no entries at all cannot be written; the edit must be refused")
	}
	if errors.Is(err, errTooLarge) {
		t.Errorf("the refusal blames the segment size: %v", err)
	}
	if !errors.Is(err, errEmptyIFD) {
		t.Errorf("err = %v, want errEmptyIFD", err)
	}
}

// A deletion must take the old value's out-of-line bytes with it, exactly as a
// replacement does: an offset or a name that is merely unreachable is still
// sitting in the file for anyone with a hex editor.
func TestDeletingATagZeroesItsOldValueBytes(t *testing.T) {
	before := jpegWith(fullTIFF(binary.LittleEndian))

	t.Run("stale offset behind a zone-unknown time", func(t *testing.T) {
		when := time.Date(2026, 8, 5, 10, 11, 12, 0, time.UTC)
		after, err := RewriteJPEG(before, Changes{DateTimeOriginal: &CaptureTime{Value: when}})
		if err != nil {
			t.Fatalf("RewriteJPEG: %v", err)
		}
		if bytes.Contains(after, []byte("+02:00")) {
			t.Error("the deleted offset's bytes are still sitting in the file")
		}
		f, err := Parse(after)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if !f.DateTimeOriginal.Value.Equal(when) || f.DateTimeOriginal.HasOffset {
			t.Errorf("the edit itself went wrong: %+v", f.DateTimeOriginal)
		}
	})

	t.Run("cleared artist", func(t *testing.T) {
		after, err := RewriteJPEG(before, Changes{Artist: ptr("")})
		if err != nil {
			t.Fatalf("RewriteJPEG: %v", err)
		}
		if bytes.Contains(after, []byte("Old Artist")) {
			t.Error("the cleared artist's bytes are still sitting in the file")
		}
	})
}

// directoryOf re-reads a directory a rewrite left behind, straight from the
// written bytes, so a test can assert on what a stricter reader than ours
// would find there.
func directoryOf(t *testing.T, jpeg []byte, pointerTag uint16) *directory {
	t.Helper()
	block := firstEXIFSegment(jpeg)
	if block == nil {
		t.Fatal("no EXIF segment in the written file")
	}
	r, ifd0Off, ok := newReader(block)
	if !ok {
		t.Fatal("the written TIFF block has no readable header")
	}
	ifd0, ok := r.readIFD(ifd0Off)
	if !ok {
		t.Fatal("the written IFD0 is unreadable")
	}
	off := r.pointer(ifd0, pointerTag)
	if off == 0 {
		t.Fatalf("IFD0 carries no pointer 0x%04X", pointerTag)
	}
	d, ok := r.readIFD(off)
	if !ok {
		t.Fatalf("the directory 0x%04X points at is unreadable", pointerTag)
	}
	return d
}

// Creating a directory from nothing must not serialise the deletions that ride
// along with the additions — a location without an altitude deletes the two
// altitude tags, and in a directory that never existed there is nothing to
// delete. Serialising them anyway writes type-0, count-0 entries that a strict
// reader treats as corruption.
func TestCreatingAGPSIFDSerialisesNoDeletions(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM")}, 0)
	bare := jpegWith(b.done(ifd0))

	after, err := RewriteJPEG(bare, Changes{SetGPS: &GPSCoord{
		Latitude:    51.5066667,
		Longitude:   -0.1275,
		HasAltitude: false,
	}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	d := directoryOf(t, after, tagGPSIFD)
	// Version, both references, both coordinates — and nothing else.
	if len(d.entries) != 5 {
		t.Errorf("created GPS IFD has %d entries, want 5", len(d.entries))
	}
	for _, e := range d.entries {
		if e.typ == 0 || e.count == 0 {
			t.Errorf("entry 0x%04X was written with type %d count %d — a deletion was serialised", e.tag, e.typ, e.count)
		}
		if e.tag == tagGPSAltitudeRef || e.tag == tagGPSAltitude {
			t.Errorf("tag 0x%04X was written into a directory it was being deleted from", e.tag)
		}
	}
}

func TestCreatingAnExifIFDSerialisesNoDeletions(t *testing.T) {
	b := newTIFF(binary.LittleEndian)
	ifd0 := b.ifd([]tag{ascii(tagMake, "FUJIFILM")}, 0)
	bare := jpegWith(b.done(ifd0))

	// A whole second: the sub-second tag is a deletion, not a value.
	when := time.Date(2026, 8, 3, 19, 42, 7, 0, time.FixedZone("", 2*3600))
	after, err := RewriteJPEG(bare, Changes{DateTimeOriginal: &CaptureTime{Value: when, HasOffset: true}})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}

	d := directoryOf(t, after, tagExifIFD)
	for _, e := range d.entries {
		if e.typ == 0 || e.count == 0 {
			t.Errorf("entry 0x%04X was written with type %d count %d — a deletion was serialised", e.tag, e.typ, e.count)
		}
		if e.tag == tagSubSecTimeOriginal {
			t.Error("SubSecTimeOriginal was written into a directory it was being deleted from")
		}
	}
}

func TestRewriteJPEGWithNoEXIFSegment(t *testing.T) {
	_, err := RewriteJPEG(jpegWith(nil), Changes{Artist: ptr("Someone")})
	if err == nil {
		t.Fatal("a JPEG with no EXIF has nowhere to write; that must be refused rather than guessed at")
	}
}

func TestRewriteJPEGRefusesSomethingThatIsNotAJPEG(t *testing.T) {
	if _, err := RewriteJPEG(fullTIFF(binary.LittleEndian), Changes{Artist: ptr("x")}); err == nil {
		t.Fatal("a RAW header is not a JPEG and must never be rewritten")
	}
}

func TestRewriteJPEGWithNothingToDoReturnsTheSameBytes(t *testing.T) {
	before := jpegWith(fullTIFF(binary.LittleEndian))
	after, err := RewriteJPEG(before, Changes{})
	if err != nil {
		t.Fatalf("RewriteJPEG: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("an empty change set rewrote the file")
	}
}

func TestWriteJPEGReplacesInPlaceAndLeavesNoLitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(path, jpegWith(fullTIFF(binary.LittleEndian)), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := WriteJPEG(path, Changes{Artist: ptr("Tomasz Cichy")}); err != nil {
		t.Fatalf("WriteJPEG: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.Artist.Value != "Tomasz Cichy" {
		t.Errorf("Artist = %q", f.Artist.Value)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp file left behind: %v", names)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Only meaningful where the filesystem honours Unix modes at all; probe
	// rather than guess, so the assertion self-selects per platform.
	probe := filepath.Join(dir, "probe.tmp")
	if err := os.WriteFile(probe, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	pi, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(probe)
	if pi.Mode().Perm() == 0o640 && info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 640 — the file's permissions must survive the replacement", info.Mode().Perm())
	}
}

func TestRenderXMPIsWellFormedAndCarriesTheValues(t *testing.T) {
	when := time.Date(2026, 8, 3, 19, 42, 7, 370000000, time.FixedZone("", 2*3600))
	doc := RenderXMP(Changes{
		DateTimeOriginal: &CaptureTime{Value: when, HasOffset: true},
		Artist:           ptr("Tomasz Cichy"),
		Copyright:        ptr("© 2026"),
	})

	if err := xml.Unmarshal(doc, new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("the sidecar is not well-formed XML: %v", err)
	}
	for _, want := range []string{"2026-08-03T19:42:07.37+02:00", "Tomasz Cichy", "© 2026", "x:xmpmeta", "rdf:RDF"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("sidecar is missing %q:\n%s", want, doc)
		}
	}
}

func TestRenderXMPEscapesWhatTheUserTyped(t *testing.T) {
	doc := RenderXMP(Changes{Artist: ptr(`Ann & "Bob" <ann@example.com>`)})
	if err := xml.Unmarshal(doc, new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("an ampersand in a name broke the sidecar: %v", err)
	}
	if strings.Contains(string(doc), "Ann & \"") {
		t.Error("the ampersand was not escaped")
	}
}

func TestChangesEmpty(t *testing.T) {
	if !(Changes{}).Empty() {
		t.Error("no changes is empty")
	}
	if (Changes{StripGPS: true}).Empty() {
		t.Error("stripping GPS is a change")
	}
	if (Changes{Artist: ptr("")}).Empty() {
		t.Error("clearing a tag is a change, not an absence of one")
	}
}

// The writer is fed the same hostile bytes as the reader: it may refuse
// anything, but it must never panic and never produce a file that is no longer
// a JPEG.
func TestRewriteJPEGSurvivesTruncationAtEveryLength(t *testing.T) {
	full := jpegWith(fullTIFF(binary.LittleEndian))
	for n := 0; n <= len(full); n++ {
		out, err := RewriteJPEG(full[:n], Changes{Artist: ptr("x"), StripGPS: true})
		if err != nil {
			continue
		}
		if !hasSOI(out) {
			t.Fatalf("a %d byte prefix produced %d bytes that are not a JPEG", n, len(out))
		}
	}
}

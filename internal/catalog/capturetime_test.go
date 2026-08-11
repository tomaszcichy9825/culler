package catalog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/hash"
)

// A photograph's shot time is when it was taken, and the only thing that knows
// that is its EXIF. The file's mtime is when the file was last written, which
// on a library assembled by copying folders about is the day of the copy — so
// a shoot from May reads as a shoot from the day it reached the disk, and a
// folder copied in one go collapses into a "session" as long as the copy took.
//
// The catalogue therefore reads the capture time while it is already reading
// the file to hash it, and records where the answer came from so a row written
// before it did can be spotted and re-read.

// jpegWithCaptureTime builds the smallest JPEG that carries a
// DateTimeOriginal: SOI, an APP1 holding a TIFF block whose IFD0 points at an
// Exif IFD holding the one tag, then EOI. Offsets are relative to the start of
// the TIFF block, as the format requires.
func jpegWithCaptureTime(stamp string) []byte {
	const (
		tiffHeader = 8  // "II", 0x002A, offset of IFD0
		ifd0At     = 8  // IFD0 sits immediately after the header
		exifIFDAt  = 26 // after IFD0's count, one entry and its next-offset
		stampAt    = 44 // after the Exif IFD's count, one entry and its next-offset
	)
	tiff := make([]byte, stampAt+20)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 0x002A)
	binary.LittleEndian.PutUint32(tiff[4:], ifd0At)

	// IFD0: one entry, the pointer to the Exif IFD.
	binary.LittleEndian.PutUint16(tiff[ifd0At:], 1)
	binary.LittleEndian.PutUint16(tiff[ifd0At+2:], 0x8769) // ExifIFD
	binary.LittleEndian.PutUint16(tiff[ifd0At+4:], 4)      // LONG
	binary.LittleEndian.PutUint32(tiff[ifd0At+6:], 1)
	binary.LittleEndian.PutUint32(tiff[ifd0At+10:], exifIFDAt)
	binary.LittleEndian.PutUint32(tiff[ifd0At+14:], 0) // no IFD1

	// The Exif IFD: one entry, DateTimeOriginal, whose value is too big to sit
	// inline and so is an offset to the string.
	binary.LittleEndian.PutUint16(tiff[exifIFDAt:], 1)
	binary.LittleEndian.PutUint16(tiff[exifIFDAt+2:], 0x9003) // DateTimeOriginal
	binary.LittleEndian.PutUint16(tiff[exifIFDAt+4:], 2)      // ASCII
	binary.LittleEndian.PutUint32(tiff[exifIFDAt+6:], 20)
	binary.LittleEndian.PutUint32(tiff[exifIFDAt+10:], stampAt)
	binary.LittleEndian.PutUint32(tiff[exifIFDAt+14:], 0)
	copy(tiff[stampAt:], stamp+"\x00")

	payload := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	size := make([]byte, 2)
	binary.BigEndian.PutUint16(size, uint16(len(payload)+2))
	out = append(out, size...)
	out = append(out, payload...)
	return append(out, 0xFF, 0xD9)
}

// writeShotFrame writes a JPEG whose EXIF says it was taken at shot and whose
// mtime says it was written at written — the shape every copied library has.
func writeShotFrame(t *testing.T, dir, stem string, shot, written time.Time) string {
	t.Helper()
	path := filepath.Join(dir, stem+".JPG")
	if err := os.WriteFile(path, jpegWithCaptureTime(shot.Format("2006:01:02 15:04:05")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIndexRecordsTheCaptureTimeNotTheFileTime(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	shot := time.Date(2026, 8, 2, 11, 48, 13, 0, time.UTC)
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	writeShotFrame(t, root, "DSCF1201", shot, copied)

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	frames, err := s.frames(`SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict FROM frames`)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("%d frames indexed, want 1", len(frames))
	}
	if !frames[0].Shot.Equal(shot) {
		t.Errorf("shot = %v, want the capture time %v (the file time is %v)",
			frames[0].Shot, shot, copied)
	}
}

// A frame with no capture time at all — a JPEG out of an editor, a RAW the
// parser cannot read — still gets a time, because the grid has to sort it
// somewhere. The file's own is the only other answer there is.
func TestIndexFallsBackToTheFileTime(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	written := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	writeFrame(t, root, "PLAIN001", 0, 400, written)

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	frames, err := s.frames(`SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict FROM frames`)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || !frames[0].Shot.Equal(written) {
		t.Errorf("a frame with no capture time got %v, want the file time %v", frames[0].Shot, written)
	}
}

// Sessions are the reason this matters: a folder of one shoot copied in one go
// is one shoot, not a twenty-second burst on the day of the copy.
func TestSessionsFollowTheCaptureTime(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	// A morning shoot and an evening shoot on the same day, copied to the disk
	// one second apart a week later.
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	writeShotFrame(t, root, "MORN0001", time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC), copied)
	writeShotFrame(t, root, "MORN0002", time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC), copied.Add(time.Second))
	writeShotFrame(t, root, "EVEN0001", time.Date(2026, 8, 2, 19, 0, 0, 0, time.UTC), copied.Add(2*time.Second))
	writeShotFrame(t, root, "EVEN0002", time.Date(2026, 8, 2, 19, 20, 0, 0, time.UTC), copied.Add(3*time.Second))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatal(err)
	}
	// Two shoots on one day, ten hours apart: the whole point of a session is
	// that the day is not the unit.
	if len(sessions) != 2 {
		t.Fatalf("%d sessions, want the morning and the evening: %+v", len(sessions), sessions)
	}
	if sessions[0].Frames != 2 || sessions[1].Frames != 2 {
		t.Errorf("session sizes %d and %d, want 2 and 2", sessions[0].Frames, sessions[1].Frames)
	}
	if !sessions[1].Start.Equal(time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("the older session starts %v, want the morning", sessions[1].Start)
	}
	if sessions[1].Span() != 30*time.Minute {
		t.Errorf("the morning ran %v, want 30m — not the length of the copy", sessions[1].Span())
	}
}

// A row written before the catalogue read capture times holds the file's
// mtime with nothing saying so. Its files have not changed, so every other
// rule says leave it alone — but the time in it is wrong, and only the file
// can settle that. It is re-read exactly once, and not again after.
func TestIndexRepairsRowsWrittenBeforeCaptureTimesWereRead(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	shot := time.Date(2026, 8, 2, 11, 48, 13, 0, time.UTC)
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	writeShotFrame(t, root, "DSCF1201", shot, copied)

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	// Wind the row back to what an older build would have written: the file's
	// time, and no word on where it came from.
	if _, err := s.db.Exec(
		`UPDATE frames SET shot = ?, shot_source = ''`, copied.Unix()); err != nil {
		t.Fatal(err)
	}

	// The repair reads the file's head for its capture time and nothing more.
	// Re-hashing would mean pulling every photograph in the library down again
	// — on a network share or a cloud folder, the difference between a header
	// read and a whole-file read is the difference between seconds and an
	// afternoon — and the identity in the row is still good: the files have
	// not changed.
	hashes, captures := 0, 0
	opts := IndexOptions{
		hashFile: func(path string) (string, error) {
			hashes++
			return hash.Content(path)
		},
		captureTime: func(path string) (time.Time, bool) {
			captures++
			return captureTimeOf(path)
		},
	}
	if _, err := s.Index(root, opts); err != nil {
		t.Fatal(err)
	}
	if hashes != 0 {
		t.Errorf("the repair re-hashed %d files; the identity was already good", hashes)
	}
	if captures != 1 {
		t.Errorf("capture time was read %d times, want exactly 1", captures)
	}

	frames, err := s.frames(`SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict FROM frames`)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || !frames[0].Shot.Equal(shot) {
		t.Fatalf("the repaired row holds %v, want the capture time %v", frames[0].Shot, shot)
	}

	// And now that it says where its time came from, a later pass reads
	// nothing: the repair is once, not every launch.
	hashes, captures = 0, 0
	if _, err := s.Index(root, opts); err != nil {
		t.Fatal(err)
	}
	if hashes != 0 || captures != 0 {
		t.Errorf("a repaired row was read again: %d hashes, %d capture times", hashes, captures)
	}
}

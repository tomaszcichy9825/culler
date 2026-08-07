package app

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// jpegFixture builds a small but complete JPEG carrying an EXIF APP1 with
// Make, Artist and DateTimeOriginal, an ICC segment and an entropy-coded
// stream. The service tests need a real file to edit, and a real camera is not
// available to a unit test.
func jpegFixture() []byte {
	order := binary.LittleEndian
	tiff := make([]byte, 8)
	copy(tiff, "II")
	order.PutUint16(tiff[2:], 42)

	// Value blobs, then the two directories that point at them.
	blob := func(p []byte) uint32 {
		if len(tiff)%2 == 1 {
			tiff = append(tiff, 0)
		}
		at := uint32(len(tiff))
		tiff = append(tiff, p...)
		return at
	}
	makeAt := blob([]byte("FUJIFILM\x00"))
	artistAt := blob([]byte("Old Artist\x00"))
	dateAt := blob([]byte("2026:08:03 19:42:07\x00"))

	entry := func(tag, typ uint16, count, value uint32) []byte {
		e := make([]byte, 12)
		order.PutUint16(e[0:], tag)
		order.PutUint16(e[2:], typ)
		order.PutUint32(e[4:], count)
		order.PutUint32(e[8:], value)
		return e
	}
	ifd := func(entries ...[]byte) uint32 {
		if len(tiff)%2 == 1 {
			tiff = append(tiff, 0)
		}
		at := uint32(len(tiff))
		n := make([]byte, 2)
		order.PutUint16(n, uint16(len(entries)))
		tiff = append(tiff, n...)
		for _, e := range entries {
			tiff = append(tiff, e...)
		}
		tiff = append(tiff, 0, 0, 0, 0)
		return at
	}

	exifIFD := ifd(entry(0x9003, 2, 20, dateAt))
	ifd0 := ifd(
		entry(0x010F, 2, 9, makeAt),
		entry(0x013B, 2, 11, artistAt),
		entry(0x8769, 4, 1, exifIFD),
	)
	order.PutUint32(tiff[4:], ifd0)

	seg := func(marker byte, payload []byte) []byte {
		out := []byte{0xFF, marker, 0, 0}
		binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
		return append(out, payload...)
	}
	out := []byte{0xFF, 0xD8}
	out = append(out, seg(0xE1, append([]byte("Exif\x00\x00"), tiff...))...)
	out = append(out, seg(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), make([]byte, 64)...))...)
	out = append(out, seg(0xDA, []byte{1, 0, 0, 0, 63, 0})...)
	out = append(out, 0xAB, 0xCD, 0xEF, 0x01, 0x23)
	return append(out, 0xFF, 0xD9)
}

// frames writes a JPEG frame and a RAW-only frame and returns the directory.
func frames(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DSCF0001.JPG"), jpegFixture(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DSCF0002.RAF"), []byte("FUJIFILMCCD-RAW not a real sensor"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func str(s string) *string { return &s }

// groupFor scans dir and returns the frame with the given stem.
func groupFor(t *testing.T, dir, stem string) scan.PhotoGroup {
	t.Helper()
	groups, err := scan.ScanDir(dir, scan.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Stem == stem {
			return g
		}
	}
	t.Fatalf("no frame %q in %s", stem, dir)
	return scan.PhotoGroup{}
}

func TestExifReadReportsFieldsAndWritability(t *testing.T) {
	dir := frames(t)
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	raf := filepath.Join(dir, "DSCF0002.RAF")

	got, err := NewExifService(testApp(t)).Read([]string{jpg, raf})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("%d frames, want 2", len(got))
	}

	frame := got[jpg]
	if frame.Kind != "jpeg" || frame.Sidecar != "" {
		t.Errorf("JPEG frame = %s, sidecar %q; a JPEG is written in place", frame.Kind, frame.Sidecar)
	}
	byTag := map[string]ExifFieldDTO{}
	for _, f := range frame.Fields {
		byTag[f.Tag] = f
	}
	if byTag["Artist"].Value != "Old Artist" || !byTag["Artist"].Present {
		t.Errorf("Artist = %+v", byTag["Artist"])
	}
	if !byTag["Artist"].Writable || !byTag["DateTimeOriginal"].Writable {
		t.Error("Artist and DateTimeOriginal are writable on a JPEG")
	}
	if byTag["Model"].Present {
		t.Error("a tag the file does not carry must be absent, not empty-but-present")
	}
	if byTag["Make"].Writable {
		t.Error("Make is not a tag this app writes; its row is locked")
	}

	raw := got[raf]
	if raw.Kind != "raw" {
		t.Errorf("RAW frame kind = %q", raw.Kind)
	}
	if raw.Sidecar != raf+".xmp" {
		t.Errorf("sidecar = %q, want %s.xmp", raw.Sidecar, raf)
	}
	for _, f := range raw.Fields {
		if f.Tag == "Artist" && !f.Writable {
			t.Error("Artist reaches a RAW frame through its sidecar and is writable")
		}
		if f.Tag == "Orientation" && f.Writable {
			t.Error("a tag with no sidecar representation is locked on a RAW frame")
		}
	}
}

func TestExifReadOnAFileThatIsNotAPhotograph(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewExifService(testApp(t)).Read([]string{path})
	if err != nil {
		t.Fatalf("one unreadable frame must not fail the whole read: %v", err)
	}
	if got[path].Error == "" {
		t.Error("the frame should carry the reason it could not be read")
	}
}

func TestExifPlanDescribesWithoutTouchingAnything(t *testing.T) {
	dir := frames(t)
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	raf := filepath.Join(dir, "DSCF0002.RAF")
	before, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := NewExifService(testApp(t)).Plan([]ExifEditDTO{
		{Path: jpg, Artist: str("Tomasz Cichy"), StripGPS: true},
		{Path: raf, Copyright: str("© 2026")},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if plan.Frames != 2 {
		t.Errorf("frames = %d, want 2", plan.Frames)
	}
	if plan.Writes != 3 {
		t.Errorf("writes = %d, want 3 (artist, strip GPS, copyright)", plan.Writes)
	}
	if plan.BackupDir == "" {
		t.Error("the plan must say where the originals are backed up to")
	}
	if strings.HasPrefix(plan.BackupDir, dir) {
		t.Errorf("backups must not land on the card being edited: %s", plan.BackupDir)
	}

	methods := map[string]string{}
	for _, row := range plan.Rows {
		methods[row.Target] = row.Method
	}
	if methods["DSCF0001.JPG"] != "in place" {
		t.Errorf("JPEG method = %q, want in place", methods["DSCF0001.JPG"])
	}
	if methods["DSCF0002.RAF.xmp"] != "sidecar" {
		t.Errorf("RAW method = %q, want sidecar", methods["DSCF0002.RAF.xmp"])
	}

	after, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("planning wrote to the file")
	}
	if _, err := os.Lstat(raf + ".xmp"); err == nil {
		t.Error("planning created a sidecar")
	}
}

// The property the whole design of the write path exists to provide: applying
// an edit and then undoing it gives back the original file, byte for byte.
func TestExifApplyThenUndoRestoresByteIdenticalFiles(t *testing.T) {
	a := testApp(t)
	dir := frames(t)
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	original, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}

	batch, err := NewExifService(a).Apply([]ExifEditDTO{{Path: jpg, Artist: str("Tomasz Cichy")}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, act := range batch.Actions {
		if act.Outcome != journal.OutcomeOK {
			t.Fatalf("action failed: %+v", act)
		}
	}

	edited, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original, edited) {
		t.Fatal("the file was not edited at all")
	}
	if !bytes.Contains(edited, []byte("Tomasz Cichy")) {
		t.Error("the new artist is not in the file")
	}
	if !bytes.Contains(edited, []byte{0xAB, 0xCD, 0xEF, 0x01, 0x23}) {
		t.Error("the image data did not survive the write")
	}

	if err := NewApplyService(a).Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	restored, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, restored) {
		t.Errorf("undo did not restore the original: %d bytes vs %d", len(restored), len(original))
	}
}

func TestExifApplyWritesASidecarForARAWFrame(t *testing.T) {
	a := testApp(t)
	dir := frames(t)
	raf := filepath.Join(dir, "DSCF0002.RAF")
	before, err := os.ReadFile(raf)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewExifService(a).Apply([]ExifEditDTO{{Path: raf, Artist: str("Tomasz Cichy")}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	sidecar, err := os.ReadFile(raf + ".xmp")
	if err != nil {
		t.Fatalf("no sidecar was written: %v", err)
	}
	if !bytes.Contains(sidecar, []byte("Tomasz Cichy")) {
		t.Errorf("sidecar does not carry the value:\n%s", sidecar)
	}
	after, err := os.ReadFile(raf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the RAW itself was modified; it must never be")
	}

	if err := NewApplyService(a).Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := os.Lstat(raf + ".xmp"); err == nil {
		t.Error("undo left the sidecar behind")
	}
}

func TestExifSetGPSWritesLocationToJPEGAndUndoes(t *testing.T) {
	a := testApp(t)
	dir := frames(t)
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	original, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewExifService(a).Apply([]ExifEditDTO{{
		Path:   jpg,
		SetGPS: &GPSCoordDTO{Latitude: 51.5066667, Longitude: -0.1275, Altitude: 35.5, HasAltitude: true},
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	f, err := exif.Read(jpg)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !f.GPS.Present {
		t.Fatalf("no location read back after geotagging: %+v", f.GPS)
	}
	if math.Abs(f.GPS.Latitude-51.5066667) > 1e-5 || math.Abs(f.GPS.Longitude-(-0.1275)) > 1e-5 {
		t.Errorf("coordinates = %.6f, %.6f", f.GPS.Latitude, f.GPS.Longitude)
	}

	if err := NewApplyService(a).Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	restored, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, restored) {
		t.Error("undo did not restore the pre-geotag file byte-for-byte")
	}
}

func TestExifSetGPSWritesSidecarForRAW(t *testing.T) {
	a := testApp(t)
	dir := frames(t)
	raf := filepath.Join(dir, "DSCF0002.RAF")
	before, err := os.ReadFile(raf)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewExifService(a).Apply([]ExifEditDTO{{
		Path:   raf,
		SetGPS: &GPSCoordDTO{Latitude: 51.5066667, Longitude: -0.1275},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	sidecar, err := os.ReadFile(raf + ".xmp")
	if err != nil {
		t.Fatalf("no sidecar written for a geotagged RAW: %v", err)
	}
	if !bytes.Contains(sidecar, []byte("<exif:GPSLatitude>")) {
		t.Errorf("sidecar carries no location:\n%s", sidecar)
	}
	after, err := os.ReadFile(raf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the RAW itself was modified; it must never be")
	}
}

func TestExifSetGPSRejectsAnImpossibleLocation(t *testing.T) {
	jpg := filepath.Join(frames(t), "DSCF0001.JPG")
	_, err := NewExifService(testApp(t)).Apply([]ExifEditDTO{{
		Path:   jpg,
		SetGPS: &GPSCoordDTO{Latitude: 200, Longitude: 0},
	}})
	if err == nil {
		t.Error("a latitude off the earth was accepted")
	}
}

func TestExifSetGPSRejectsAnAbsurdAltitude(t *testing.T) {
	jpg := filepath.Join(frames(t), "DSCF0001.JPG")
	_, err := NewExifService(testApp(t)).Apply([]ExifEditDTO{{
		Path:   jpg,
		SetGPS: &GPSCoordDTO{Latitude: 51, Longitude: 0, Altitude: 99_999_999, HasAltitude: true},
	}})
	if err == nil {
		t.Error("an altitude that would wrap the uint32 encoder was accepted")
	}
}

func TestExifApplyLeavesNoStagingBehind(t *testing.T) {
	a := testApp(t)
	jpg := filepath.Join(frames(t), "DSCF0001.JPG")
	if _, err := NewExifService(a).Apply([]ExifEditDTO{{Path: jpg, Artist: str("Someone")}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	staging := filepath.Join(a.dataDir, stagingDir)
	entries, err := os.ReadDir(staging)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("staging directory still holds %d entries", len(entries))
	}
}

func TestExifApplyWithNothingToDo(t *testing.T) {
	jpg := filepath.Join(frames(t), "DSCF0001.JPG")
	batch, err := NewExifService(testApp(t)).Apply([]ExifEditDTO{{Path: jpg}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(batch.Actions) != 0 {
		t.Errorf("an empty edit produced %d actions", len(batch.Actions))
	}
}

func TestExifApplyRefusesAPathOutsideAnyOpenedFrame(t *testing.T) {
	if _, err := NewExifService(testApp(t)).Apply([]ExifEditDTO{{Path: "", Artist: str("x")}}); err == nil {
		t.Error("an empty path is not a frame")
	}
}

func TestShotTimePrefersEXIFOverModificationTime(t *testing.T) {
	dir := frames(t)
	group := groupFor(t, dir, "DSCF0001")
	shot := ShotTime(group)
	if shot.Year() != 2026 || shot.Month() != 8 || shot.Day() != 3 {
		t.Errorf("shot = %s, want the EXIF capture date 2026-08-03", shot)
	}

	// A frame with no EXIF keeps the modification time the scan gave it.
	raw := groupFor(t, dir, "DSCF0002")
	if !ShotTime(raw).Equal(raw.Shot) {
		t.Errorf("shot = %s, want the scanned mtime %s", ShotTime(raw), raw.Shot)
	}
}

// When the backup move fails, the edited copy must not be installed anyway:
// with the original still occupying its path, the collision policy would file
// the edit beside it under a numbered name — in the photo folder, which may be
// the card, where this app writes nothing it was not asked to.
func TestExifApplyFailedBackupSkipsTheInstall(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions do not bite")
	}
	a := testApp(t)
	dir := frames(t)
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	original, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	svc := NewExifService(a)
	// The backup directory exists but takes nothing, so the move into it fails
	// while the install's own destination folder stays perfectly writable.
	if err := os.MkdirAll(svc.backupDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(svc.backupDir(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(svc.backupDir(), 0o755) })

	batch, err := svc.Apply([]ExifEditDTO{{Path: jpg, Artist: str("Someone")}})
	if err == nil {
		failed := 0
		for _, act := range batch.Actions {
			if act.Outcome != journal.OutcomeOK {
				failed++
			}
		}
		if failed == 0 {
			t.Fatal("an apply whose backup could not be made reported every action ok")
		}
	}

	after, err := os.ReadFile(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Error("the original was altered although its backup failed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(before) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the failed apply left a stray file in the photo folder: %v", names)
	}
}

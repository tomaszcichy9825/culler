package app

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// dms is one coordinate as the camera wrote it: degrees, minutes and seconds,
// with the hemisphere reference that signs it.
type dms struct {
	deg, min uint32
	// sec is hundredths of a second, which is how a rational with denominator
	// 100 reaches the file.
	sec uint32
	ref string
}

// decimal is what the reader should make of the triple.
func (d dms) decimal() float64 {
	v := float64(d.deg) + float64(d.min)/60 + float64(d.sec)/100/3600
	if d.ref == "S" || d.ref == "W" {
		return -v
	}
	return v
}

// gpsJPEG builds a JPEG carrying a GPS IFD and a capture time. altitude is in
// centimetres so a fractional height survives the rational; a negative one is
// written with the below-sea-level reference the way a camera writes it.
//
// It is deliberately its own builder rather than a parameter on jpegFixture:
// the metadata editor's fixture is the shape that file has to keep, and a map
// test bending it would break the editor's tests instead.
func gpsJPEG(lat, lon dms, altCm int32, hasAlt bool, when string) []byte {
	order := binary.LittleEndian
	tiff := make([]byte, 8)
	copy(tiff, "II")
	order.PutUint16(tiff[2:], 42)

	blob := func(p []byte) uint32 {
		if len(tiff)%2 == 1 {
			tiff = append(tiff, 0)
		}
		at := uint32(len(tiff))
		tiff = append(tiff, p...)
		return at
	}
	rationals := func(pairs ...[2]uint32) uint32 {
		buf := make([]byte, 0, len(pairs)*8)
		for _, p := range pairs {
			cell := make([]byte, 8)
			order.PutUint32(cell[0:], p[0])
			order.PutUint32(cell[4:], p[1])
			buf = append(buf, cell...)
		}
		return blob(buf)
	}

	// A value of four bytes or fewer sits in the entry's own value field
	// rather than at an offset, which is exactly the case a one-letter
	// hemisphere reference falls into.
	inline := func(s string) uint32 {
		cell := make([]byte, 4)
		copy(cell, s)
		return order.Uint32(cell)
	}

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

	dateAt := blob([]byte(when + "\x00"))
	latAt := rationals([2]uint32{lat.deg, 1}, [2]uint32{lat.min, 1}, [2]uint32{lat.sec, 100})
	lonAt := rationals([2]uint32{lon.deg, 1}, [2]uint32{lon.min, 1}, [2]uint32{lon.sec, 100})

	// A single-byte value sits in the entry's own value field, left-aligned.
	altRef := uint32(0)
	magnitude := altCm
	if altCm < 0 {
		altRef = 1
		magnitude = -altCm
	}
	gpsEntries := [][]byte{
		entry(0x0001, 2, uint32(len(lat.ref)+1), inline(lat.ref)),
		entry(0x0002, 5, 3, latAt),
		entry(0x0003, 2, uint32(len(lon.ref)+1), inline(lon.ref)),
		entry(0x0004, 5, 3, lonAt),
	}
	if hasAlt {
		altAt := rationals([2]uint32{uint32(magnitude), 100})
		gpsEntries = append(gpsEntries,
			entry(0x0005, 1, 1, altRef),
			entry(0x0006, 5, 1, altAt),
		)
	}

	gpsIFD := ifd(gpsEntries...)
	exifIFD := ifd(entry(0x9003, 2, 20, dateAt))
	ifd0 := ifd(
		entry(0x8769, 4, 1, exifIFD),
		entry(0x8825, 4, 1, gpsIFD),
	)
	order.PutUint32(tiff[4:], ifd0)

	seg := func(marker byte, payload []byte) []byte {
		out := []byte{0xFF, marker, 0, 0}
		binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
		return append(out, payload...)
	}
	out := []byte{0xFF, 0xD8}
	out = append(out, seg(0xE1, append([]byte("Exif\x00\x00"), tiff...))...)
	out = append(out, seg(0xDA, []byte{1, 0, 0, 0, 63, 0})...)
	out = append(out, 0xAB, 0xCD, 0xEF)
	return append(out, 0xFF, 0xD9)
}

// Kraków's main square and the castle below it, as a camera would have written
// them, plus one frame with no GPS at all and one file that is not a JPEG.
var (
	rynekLat = dms{50, 3, 4205, "N"}
	rynekLon = dms{19, 56, 1320, "E"}
	wawelLat = dms{50, 3, 1584, "N"}
	wawelLon = dms{19, 56, 744, "E"}
)

// positioned writes a folder holding two frames with coordinates, one with a
// capture time but no position, and one file that cannot be read as a
// photograph at all.
func positioned(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		// Written out of capture order, so a test that expects them sorted is
		// testing the sort rather than the directory listing.
		"DSCF0002.JPG": gpsJPEG(wawelLat, wawelLon, 21_900, true, "2026:07:18 19:58:02"),
		"DSCF0001.JPG": gpsJPEG(rynekLat, rynekLon, 0, false, "2026:07:18 19:42:07"),
		"DSCF0003.JPG": jpegFixture(),
		"DSCF0004.RAF": []byte("not a readable raw"),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestPositionsReturnsCoordinatesInCaptureOrder(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := positioned(t)

	got, err := s.Positions(dir)
	if err != nil {
		t.Fatalf("Positions: %v", err)
	}

	if got.Total != 4 {
		t.Errorf("Total = %d, want 4", got.Total)
	}
	if got.Positioned != 2 {
		t.Errorf("Positioned = %d, want 2", got.Positioned)
	}
	if got.Unpositioned != 1 {
		t.Errorf("Unpositioned = %d, want 1 (the frame with EXIF but no GPS)", got.Unpositioned)
	}
	if got.Unreadable != 1 {
		t.Errorf("Unreadable = %d, want 1 (the file that is not a photograph)", got.Unreadable)
	}
	if sum := got.Positioned + got.Unpositioned + got.Unreadable; sum != got.Total {
		t.Errorf("the three counts sum to %d, want Total %d", sum, got.Total)
	}
	if len(got.Frames) != 2 {
		t.Fatalf("Frames = %d, want 2", len(got.Frames))
	}

	// Oldest first, which is the order the track draws.
	if got.Frames[0].Stem != "DSCF0001" || got.Frames[1].Stem != "DSCF0002" {
		t.Errorf("frames = %s, %s; want DSCF0001 then DSCF0002",
			got.Frames[0].Stem, got.Frames[1].Stem)
	}

	first := got.Frames[0]
	if !near(first.Latitude, rynekLat.decimal()) || !near(first.Longitude, rynekLon.decimal()) {
		t.Errorf("first position = %.6f, %.6f; want %.6f, %.6f",
			first.Latitude, first.Longitude, rynekLat.decimal(), rynekLon.decimal())
	}
	if first.HasAltitude {
		t.Errorf("first frame carried no altitude, got %v m", first.Altitude)
	}
	if first.Shot != "2026-07-18T19:42:07Z" {
		t.Errorf("first shot = %q, want the EXIF capture time", first.Shot)
	}
	if first.Kind != "jpeg-only" || !first.HasJpeg || first.HasRaw {
		t.Errorf("first frame kind = %q, hasJpeg %v, hasRaw %v", first.Kind, first.HasJpeg, first.HasRaw)
	}
	if first.JpegPath != filepath.Join(got.Dir, "DSCF0001.JPG") {
		t.Errorf("first jpegPath = %q", first.JpegPath)
	}

	second := got.Frames[1]
	if !second.HasAltitude || !near(second.Altitude, 219) {
		t.Errorf("second altitude = %v (has %v), want 219 m", second.Altitude, second.HasAltitude)
	}
}

func TestPositionsSignsTheSouthernAndWesternHemispheres(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := t.TempDir()
	south := dms{33, 51, 3600, "S"}
	west := dms{151, 12, 5400, "W"}
	if err := os.WriteFile(filepath.Join(dir, "DSCF0100.JPG"),
		gpsJPEG(south, west, -1250, true, "2026:01:04 06:00:00"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.Positions(dir)
	if err != nil {
		t.Fatalf("Positions: %v", err)
	}
	if len(got.Frames) != 1 {
		t.Fatalf("Frames = %d, want 1", len(got.Frames))
	}
	f := got.Frames[0]
	if !near(f.Latitude, south.decimal()) || f.Latitude >= 0 {
		t.Errorf("latitude = %.6f, want %.6f", f.Latitude, south.decimal())
	}
	if !near(f.Longitude, west.decimal()) || f.Longitude >= 0 {
		t.Errorf("longitude = %.6f, want %.6f", f.Longitude, west.decimal())
	}
	// Below sea level is a reference byte, not a sign on the rational.
	if !near(f.Altitude, -12.5) {
		t.Errorf("altitude = %v, want -12.5", f.Altitude)
	}
}

func TestPositionsReportsProgressToItsTotal(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := positioned(t)

	var mu sync.Mutex
	var reports []MapProgress
	s.onProgress = func(p MapProgress) {
		mu.Lock()
		defer mu.Unlock()
		reports = append(reports, p)
	}

	if _, err := s.Positions(dir); err != nil {
		t.Fatalf("Positions: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(reports) == 0 {
		t.Fatal("no progress reported")
	}
	last := reports[len(reports)-1]
	if last.Done != 4 || last.Total != 4 {
		t.Errorf("last report = %d/%d, want 4/4", last.Done, last.Total)
	}
	for _, r := range reports {
		if r.Dir != dir {
			t.Errorf("progress named %q, want %q", r.Dir, dir)
		}
		if r.Done > r.Total {
			t.Errorf("progress ran past its total: %d/%d", r.Done, r.Total)
		}
	}
}

func TestPositionsOnAFolderWithNoFrames(t *testing.T) {
	s := NewMapService(testApp(t))

	got, err := s.Positions(t.TempDir())
	if err != nil {
		t.Fatalf("Positions: %v", err)
	}
	if got.Total != 0 || got.Positioned != 0 {
		t.Errorf("empty folder reported %d frames, %d positioned", got.Total, got.Positioned)
	}
	// A null here would reach the frontend as a missing array rather than an
	// empty one, and every pane reading it would have to guard.
	if got.Frames == nil {
		t.Error("Frames is null, want an empty array")
	}
}

func TestPositionsRejectsWhatIsNotAFolder(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := positioned(t)

	if _, err := s.Positions(""); err == nil {
		t.Error("an empty path was accepted")
	}
	if _, err := s.Positions(filepath.Join(dir, "DSCF0001.JPG")); err == nil {
		t.Error("a file was accepted as a folder")
	}
	if _, err := s.Positions(filepath.Join(dir, "nowhere")); err == nil {
		t.Error("a missing folder was accepted")
	}
}

func TestPositionsScopePlotsAcrossFoldersInCaptureOrder(t *testing.T) {
	s := NewMapService(testApp(t))
	folderA := positioned(t) // DSCF0001 (Jul 19:42), DSCF0002 (Jul 19:58), + no-gps + unreadable

	folderB := t.TempDir()
	// An earlier shot in a second folder, so a scope spanning both draws one
	// track ordered by time and not by folder.
	south := dms{33, 51, 3600, "S"}
	west := dms{151, 12, 5400, "W"}
	if err := os.WriteFile(filepath.Join(folderB, "DSCF0100.JPG"),
		gpsJPEG(south, west, -1250, true, "2026:01:04 06:00:00"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The scope names one frame from folderA and the frame from folderB. It
	// deliberately leaves out folderA's DSCF0002: a session is a subset, and the
	// map must plot only what the scope holds.
	refs := []ScopeRef{
		{Dir: folderA, Stem: "DSCF0001"},
		{Dir: folderB, Stem: "DSCF0100"},
	}
	got, err := s.PositionsScope(refs)
	if err != nil {
		t.Fatalf("PositionsScope: %v", err)
	}

	if got.Total != 2 {
		t.Errorf("Total = %d, want 2 (only the frames the scope named)", got.Total)
	}
	if got.Positioned != 2 {
		t.Errorf("Positioned = %d, want 2", got.Positioned)
	}
	if len(got.Frames) != 2 {
		t.Fatalf("Frames = %d, want 2: %+v", len(got.Frames), got.Frames)
	}
	// January in folderB sorts before July in folderA, across folders.
	if got.Frames[0].Stem != "DSCF0100" || got.Frames[1].Stem != "DSCF0001" {
		t.Errorf("track order = %s, %s; want DSCF0100 then DSCF0001",
			got.Frames[0].Stem, got.Frames[1].Stem)
	}
	for _, f := range got.Frames {
		if f.Stem == "DSCF0002" {
			t.Error("a frame the scope did not name was plotted")
		}
	}
	// No single folder, so no folder name to show.
	if got.Dir != "" {
		t.Errorf("Dir = %q, want empty for a multi-folder scope", got.Dir)
	}
}

func TestPositionsScopeSingleFolderNamesItAndFilters(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := positioned(t)

	// Only DSCF0002 is in scope, so only it is plotted and the folder is named.
	got, err := s.PositionsScope([]ScopeRef{{Dir: dir, Stem: "DSCF0002"}})
	if err != nil {
		t.Fatalf("PositionsScope: %v", err)
	}
	if got.Total != 1 || got.Positioned != 1 {
		t.Errorf("Total/Positioned = %d/%d, want 1/1", got.Total, got.Positioned)
	}
	if len(got.Frames) != 1 || got.Frames[0].Stem != "DSCF0002" {
		t.Fatalf("Frames = %+v, want only DSCF0002", got.Frames)
	}
	if got.Dir != dir {
		t.Errorf("Dir = %q, want %q for a single-folder scope", got.Dir, dir)
	}
}

func TestPositionsScopeEmpty(t *testing.T) {
	s := NewMapService(testApp(t))
	got, err := s.PositionsScope(nil)
	if err != nil {
		t.Fatalf("PositionsScope(nil): %v", err)
	}
	if got.Total != 0 || len(got.Frames) != 0 {
		t.Errorf("empty scope reported %d frames", got.Total)
	}
	if got.Frames == nil {
		t.Error("Frames is null, want an empty array")
	}
}

func TestPositionsWritesNothingToTheFolder(t *testing.T) {
	s := NewMapService(testApp(t))
	dir := positioned(t)

	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Positions(dir); err != nil {
		t.Fatalf("Positions: %v", err)
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Errorf("the folder held %d entries and now holds %d", len(before), len(after))
	}
}

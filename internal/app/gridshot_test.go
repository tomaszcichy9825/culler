package app

import (
	"path/filepath"
	"testing"
	"time"
)

// The grid sorts by shot time, and an opened folder gets its frames from the
// walk — which reads no file and so can only offer the primary file's mtime.
// On a folder copied from a card that is the day of the copy, so the sheet
// orders by when files landed rather than by when photographs were taken.
//
// The identity pass already opens every file to hash it, so the capture time
// comes back with the hash and corrects the frame in place, the same way the
// verdict and the rating do.

// captureAt is a capture-time reader that answers for named stems, standing in
// for the EXIF of files a test does not want to construct.
func captureAt(times map[string]time.Time) func(string) (time.Time, bool) {
	return func(path string) (time.Time, bool) {
		stem := filepath.Base(path)
		if ext := filepath.Ext(stem); ext != "" {
			stem = stem[:len(stem)-len(ext)]
		}
		shot, ok := times[stem]
		return shot, ok
	}
}

// parseStamp reads a DTO timestamp back, so tests compare instants rather than
// the zone the machine happens to render them in.
func parseStamp(t *testing.T, stamp string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("parse %q: %v", stamp, err)
	}
	return at
}

func TestOpenFolderReportsTheCaptureTime(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	shoot(t, dir, "DSCF0001", copied)
	shoot(t, dir, "DSCF0002", copied.Add(time.Second))

	shot := time.Date(2026, 8, 2, 11, 48, 13, 0, time.UTC)
	s := NewLibraryService(a)
	s.captureFn = captureAt(map[string]time.Time{
		"DSCF0001": shot,
		// DSCF0002 says nothing, and keeps the file's own time.
	})

	folder, err := s.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(folder.Groups) != 2 {
		t.Fatalf("%d frames, want 2", len(folder.Groups))
	}
	byStem := map[string]GroupDTO{}
	for _, g := range folder.Groups {
		byStem[g.Stem] = g
	}
	// Compared as instants: the DTO renders in the machine's zone, and the
	// same moment written two ways is still the same moment.
	if got := parseStamp(t, byStem["DSCF0001"].Shot); !got.Equal(shot) {
		t.Errorf("DSCF0001 shot = %v, want the capture time %v", got, shot)
	}
	if got := parseStamp(t, byStem["DSCF0002"].Shot); !got.Equal(copied.Add(time.Second)) {
		t.Errorf("a frame with no capture time got %v, want the file's own time %v",
			got, copied.Add(time.Second))
	}
}

// The streamed open paints from the walk first and corrects each frame when
// its identity lands, so the capture time has to ride along on that patch or
// the sheet keeps the file time it was drawn with.
func TestStreamedOpenPatchesTheCaptureTime(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	shoot(t, dir, "DSCF0001", copied)

	shot := time.Date(2026, 8, 2, 11, 48, 13, 0, time.UTC)
	s := NewLibraryService(a)
	s.captureFn = captureAt(map[string]time.Time{"DSCF0001": shot})

	rec := &recorder{}
	s.emit = rec.emit

	ticket, err := s.OpenFolderStream(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	hashed := rec.hashed(ticket.Token)
	if len(hashed) != 1 {
		t.Fatalf("%d identities emitted, want 1", len(hashed))
	}
	if got := parseStamp(t, hashed[0].Shot); !got.Equal(shot) {
		t.Errorf("the identity carries shot %v, want the capture time %v", got, shot)
	}
}

// The sheet must not reorder itself while a folder loads. That means a frame
// has to arrive carrying the time it will be sorted by — the walk reads no
// file, so the capture time is read for each batch before that batch is
// painted, and a tile lands in its final position rather than being shuffled
// there when its identity turns up behind it.
func TestStreamedOpenPaintsFramesWithTheCaptureTime(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	copied := time.Date(2026, 8, 8, 23, 14, 13, 0, time.UTC)
	shoot(t, dir, "DSCF0001", copied)
	shoot(t, dir, "DSCF0002", copied.Add(time.Second))

	shot := time.Date(2026, 8, 2, 11, 48, 13, 0, time.UTC)
	s := NewLibraryService(a)
	s.captureFn = captureAt(map[string]time.Time{
		"DSCF0001": shot,
		"DSCF0002": shot.Add(time.Minute),
	})

	rec := &recorder{}
	s.emit = rec.emit
	// A hash that never returns would still let the frames paint; this one
	// simply proves the painted frames did not wait for it to say anything.
	s.hashFn = func(string) (string, error) { return "", nil }

	ticket, err := s.OpenFolderStream(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	painted := rec.frames(ticket.Token)
	if len(painted) != 2 {
		t.Fatalf("%d frames painted, want 2", len(painted))
	}
	for _, g := range painted {
		want := shot
		if g.Stem == "DSCF0002" {
			want = shot.Add(time.Minute)
		}
		if got := parseStamp(t, g.Shot); !got.Equal(want) {
			t.Errorf("%s painted with %v, want the capture time %v — the tile would move when its identity landed",
				g.Stem, got, want)
		}
	}
}

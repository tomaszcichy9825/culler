package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openStore opens a catalogue in a temp directory and closes it with the test.
func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenTwiceKeepsTheData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := first.AddRoot("/cards/FUJI_SD"); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	roots, err := second.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != "/cards/FUJI_SD" {
		t.Errorf("Roots after reopen = %+v, want the one added before the close", roots)
	}
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	s.Close()

	if _, err := Open(path); err == nil {
		t.Error("a catalogue written by a newer build opened without complaint")
	}
}

func TestAddRootIsIdempotentAndKeepsTheFirstAddedAt(t *testing.T) {
	s := openStore(t)

	first, err := s.AddRoot("/cards/FUJI_SD")
	if err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if first.AddedAt.IsZero() {
		t.Error("AddRoot returned no added-at time")
	}
	if !first.LastIndexedAt.IsZero() {
		t.Errorf("a root that has never been indexed reports LastIndexedAt = %v", first.LastIndexedAt)
	}

	again, err := s.AddRoot("/cards/FUJI_SD")
	if err != nil {
		t.Fatalf("AddRoot again: %v", err)
	}
	if !again.AddedAt.Equal(first.AddedAt) {
		t.Errorf("re-adding a root moved AddedAt from %v to %v", first.AddedAt, again.AddedAt)
	}

	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 {
		t.Errorf("%d roots after adding the same path twice, want 1", len(roots))
	}
}

func TestAddRootRejectsARelativePath(t *testing.T) {
	s := openStore(t)
	if _, err := s.AddRoot("cards/FUJI_SD"); err == nil {
		t.Error("a relative root was accepted; the catalogue's prefix maths needs absolute paths")
	}
	if _, err := s.AddRoot(""); err == nil {
		t.Error("an empty root was accepted")
	}
}

func TestRootsCarryTheirFrameCountsAndBytes(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "DSCF0001", 300, 100, shotAt(9, 0))
	writeFrame(t, root, "DSCF0002", 400, 0, shotAt(9, 1))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("%d roots, want 1", len(roots))
	}
	if roots[0].Frames != 2 {
		t.Errorf("root holds %d frames, want 2", roots[0].Frames)
	}
	if roots[0].RawBytes != 700 || roots[0].JpegBytes != 100 {
		t.Errorf("root bytes = raw %d / jpeg %d, want 700 / 100", roots[0].RawBytes, roots[0].JpegBytes)
	}
	if roots[0].LastIndexedAt.IsZero() {
		t.Error("an indexed root still reports no LastIndexedAt")
	}
}

func TestRemoveRootPrunesItsFramesOnly(t *testing.T) {
	s := openStore(t)
	base := t.TempDir()
	kept := filepath.Join(base, "keep")
	gone := filepath.Join(base, "gone")
	mkdir(t, kept)
	mkdir(t, gone)
	writeFrame(t, kept, "KEEP0001", 100, 0, shotAt(9, 0))
	writeFrame(t, gone, "GONE0001", 100, 0, shotAt(9, 1))

	for _, root := range []string{kept, gone} {
		if _, err := s.Index(root, IndexOptions{}); err != nil {
			t.Fatalf("Index %s: %v", root, err)
		}
	}
	if err := s.RemoveRoot(gone); err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}

	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != kept {
		t.Fatalf("Roots after removal = %+v, want only %s", roots, kept)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Frames[0].Stem != "KEEP0001" {
		t.Errorf("frames after removal = %+v, want the surviving root's frame only", res.Frames)
	}
}

// A root inside another root keeps its frames when the outer one goes: the
// frames are still catalogued, because a root the user has asked for still
// covers them.
func TestRemoveRootKeepsFramesCoveredByANestedRoot(t *testing.T) {
	s := openStore(t)
	outer := t.TempDir()
	inner := filepath.Join(outer, "2026-05-01")
	mkdir(t, inner)
	writeFrame(t, outer, "OUTER001", 100, 0, shotAt(9, 0))
	writeFrame(t, inner, "INNER001", 100, 0, shotAt(9, 1))

	if _, err := s.Index(outer, IndexOptions{}); err != nil {
		t.Fatalf("Index outer: %v", err)
	}
	if _, err := s.AddRoot(inner); err != nil {
		t.Fatalf("AddRoot inner: %v", err)
	}
	if err := s.RemoveRoot(outer); err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Frames[0].Stem != "INNER001" {
		t.Errorf("frames = %+v, want only the frame the nested root still covers", res.Frames)
	}
}

func TestRemoveRootOfAnUnknownPathIsNotAnError(t *testing.T) {
	s := openStore(t)
	if err := s.RemoveRoot("/cards/never-added"); err != nil {
		t.Errorf("removing a root that was never added: %v", err)
	}
}

func TestVolumeOf(t *testing.T) {
	tests := []struct{ path, want string }{
		{"/Volumes/FUJI_SD/DCIM/100_FUJI", "/Volumes/FUJI_SD"},
		{"/Volumes/FUJI_SD", "/Volumes/FUJI_SD"},
		{"/media/tomasz/CARD/DCIM", "/media/tomasz/CARD"},
		{"/mnt/photos/2026", "/mnt/photos"},
		{"/Users/tomasz/Pictures", "/"},
		{"/", "/"},
	}
	for _, tt := range tests {
		if got := volumeOf(tt.path); got != tt.want {
			t.Errorf("volumeOf(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// --- helpers -----------------------------------------------------------

// shotAt is a fixed date at the given clock, so a test can lay frames out in
// time without caring which day they land on.
func shotAt(hour, minute int) time.Time {
	return time.Date(2026, 5, 1, hour, minute, 0, 0, time.UTC)
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// writeFrame lays one frame down in dir: a RAW of rawBytes and, when
// jpegBytes is above zero, a JPEG beside it. Both carry shot as their mtime,
// which is what the scan reads as the shot time. Contents differ per file so
// that every frame gets its own identity hash.
func writeFrame(t *testing.T, dir, stem string, rawBytes, jpegBytes int, shot time.Time) {
	t.Helper()
	write := func(name string, size int) {
		path := filepath.Join(dir, name)
		body := make([]byte, size)
		copy(body, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, shot, shot); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}
	if rawBytes > 0 {
		write(stem+".RAF", rawBytes)
	}
	if jpegBytes > 0 {
		write(stem+".JPG", jpegBytes)
	}
}

package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fix makes a fixture path absolute for the running platform: drive-prefixed
// and native-separated on Windows, unchanged on Unix. The catalogue's prefix
// maths runs on native separators and absolute paths, so fixtures must too.
// Nothing here has to exist on disk.
func fix(p string) string {
	return filepath.FromSlash(filepath.VolumeName(os.TempDir()) + p)
}

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
	if _, err := first.AddRoot(fix("/cards/FUJI_SD")); err != nil {
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
	if len(roots) != 1 || roots[0].Path != fix("/cards/FUJI_SD") {
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

	first, err := s.AddRoot(fix("/cards/FUJI_SD"))
	if err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if first.AddedAt.IsZero() {
		t.Error("AddRoot returned no added-at time")
	}
	if !first.LastIndexedAt.IsZero() {
		t.Errorf("a root that has never been indexed reports LastIndexedAt = %v", first.LastIndexedAt)
	}

	again, err := s.AddRoot(fix("/cards/FUJI_SD"))
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

func TestRemoveRootOfAnUnknownPathIsNotAnError(t *testing.T) {
	s := openStore(t)
	if err := s.RemoveRoot(fix("/cards/never-added")); err != nil {
		t.Errorf("removing a root that was never added: %v", err)
	}
}

// The filesystem root is a legal root, and it is the one path whose child
// prefix is not itself plus a separator. Both prefix helpers must agree on it,
// or / would register with totals of nothing and RemoveRoot("/") would orphan
// every row.
func TestUnderRootOnTheFilesystemRoot(t *testing.T) {
	fsRoot := fix("/") // the drive root on Windows, / everywhere else
	trips := fix("/photos/trips")
	if !under(trips, fsRoot) {
		t.Errorf("under says %s does not sit under %s", trips, fsRoot)
	}
	if !under(fsRoot, fsRoot) {
		t.Errorf("under says %s does not sit under itself", fsRoot)
	}

	s := openStore(t)
	if _, err := s.db.Exec(upsertFrameSQL,
		"hash-under-root", trips, "DSCF0001", "raw-only", int64(0),
		filepath.Join(trips, "DSCF0001.RAF"), "", int64(100), int64(0),
		int64(0), int64(0), sourceFileTime, 0, "", int64(0)); err != nil {
		t.Fatal(err)
	}
	where, args := underRoot(fsRoot)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatalf("count under %s: %v", fsRoot, err)
	}
	if n != 1 {
		t.Errorf("underRoot(%q) matched %d frames, want the 1 catalogued", fsRoot, n)
	}
}

func TestVolumeOf(t *testing.T) {
	// Unix paths take the mount-parent heuristics; drive-prefixed paths take
	// the drive-letter branch, whose contract is the volume name plus the
	// separator. Each shape is only constructible on its own platform, because
	// filepath.VolumeName sees no volume in C:\ on Unix and no separators in a
	// slashed path on Windows.
	var tests []struct{ path, want string }
	if filepath.Separator == '/' {
		tests = []struct{ path, want string }{
			{"/Volumes/FUJI_SD/DCIM/100_FUJI", "/Volumes/FUJI_SD"},
			{"/Volumes/FUJI_SD", "/Volumes/FUJI_SD"},
			{"/media/tomasz/CARD/DCIM", "/media/tomasz/CARD"},
			{"/mnt/photos/2026", "/mnt/photos"},
			{"/Users/tomasz/Pictures", "/"},
			{"/", "/"},
		}
	} else {
		tests = []struct{ path, want string }{
			{`C:\Users\tomasz\Pictures`, `C:\`},
			{`D:\DCIM\100_FUJI`, `D:\`},
			{`D:\`, `D:\`},
			{`\\server\photos\2026`, `\\server\photos\`},
		}
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

// under and underRoot special-case a root that already ends in the path
// separator — the filesystem root here, a drive root like C:\ on Windows —
// and the two must agree on it: doubling the separator would build a prefix
// no path carries.
func TestUnderAndUnderRootAgreeOnASeparatorTerminatedRoot(t *testing.T) {
	sep := string(filepath.Separator)
	if !under(sep, sep) {
		t.Error("the root is not under itself")
	}
	if !under(filepath.Join(sep, "photos"), sep) {
		t.Error("a top-level folder is not under the root")
	}

	where, args := underRoot(sep)
	if len(args) != 3 {
		t.Fatalf("underRoot(%q) built %d args, want 3 for %q", sep, len(args), where)
	}
	if args[2] != sep {
		t.Errorf("underRoot(%q) matches prefix %q, want the root itself", sep, args[2])
	}
	if args[1] != 1 {
		t.Errorf("underRoot(%q) measures the prefix as %v runes, want 1", sep, args[1])
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

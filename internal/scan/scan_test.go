package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// touch creates an empty file at dir/name.
func touch(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func groupByStem(groups []PhotoGroup) map[string]PhotoGroup {
	m := make(map[string]PhotoGroup)
	for _, g := range groups {
		m[g.Stem] = g
	}
	return m
}

func TestGrouping(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("raw plus jpeg pairs", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "DSCF1234.RAF")
		touch(t, dir, "DSCF1234.JPG")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("want 1 group, got %d", len(groups))
		}
		g := groups[0]
		if g.Kind != KindPaired {
			t.Errorf("want KindPaired, got %v", g.Kind)
		}
		if g.Raw == nil || filepath.Base(g.Raw.Path) != "DSCF1234.RAF" {
			t.Errorf("raw not attached: %+v", g.Raw)
		}
		if g.Jpeg == nil || filepath.Base(g.Jpeg.Path) != "DSCF1234.JPG" {
			t.Errorf("jpeg not attached: %+v", g.Jpeg)
		}
		if g.Stem != "DSCF1234" {
			t.Errorf("stem should preserve case, got %q", g.Stem)
		}
	})

	t.Run("case-insensitive stem matching", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "dscf1234.raf")
		touch(t, dir, "DSCF1234.JPG")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("case-insensitive key must pair these; got %d groups", len(groups))
		}
		if groups[0].Kind != KindPaired {
			t.Errorf("want KindPaired, got %v", groups[0].Kind)
		}
	})

	t.Run("different stems never pair", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "_DSF1234.RAF")
		touch(t, dir, "DSCF1234.JPG")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 2 {
			t.Fatalf("want 2 groups, got %d", len(groups))
		}
		m := groupByStem(groups)
		if m["_DSF1234"].Kind != KindRAWOnly {
			t.Errorf("_DSF1234 should be RAW-only")
		}
		if m["DSCF1234"].Kind != KindJPEGOnly {
			t.Errorf("DSCF1234 should be JPEG-only")
		}
	})

	t.Run("jpeg only and raw only kinds", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "a.jpg")
		touch(t, dir, "b.arw")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		m := groupByStem(groups)
		if m["a"].Kind != KindJPEGOnly || m["a"].Jpeg == nil || m["a"].Raw != nil {
			t.Errorf("a: %+v", m["a"])
		}
		if m["b"].Kind != KindRAWOnly || m["b"].Raw == nil || m["b"].Jpeg != nil {
			t.Errorf("b: %+v", m["b"])
		}
	})

	t.Run("sidecar attaches to raw group", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "DSCF1234.RAF")
		touch(t, dir, "DSCF1234.JPG")
		touch(t, dir, "DSCF1234.RAF.xmp")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("want 1 group, got %d", len(groups))
		}
		g := groups[0]
		if len(g.Sidecars) != 1 || filepath.Base(g.Sidecars[0].Path) != "DSCF1234.RAF.xmp" {
			t.Errorf("sidecar not attached: %+v", g.Sidecars)
		}
	})

	t.Run("plain-stem sidecar attaches too", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "DSCF1234.RAF")
		touch(t, dir, "DSCF1234.xmp")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("want 1 group, got %d", len(groups))
		}
		if len(groups[0].Sidecars) != 1 {
			t.Errorf("sidecar not attached: %+v", groups[0].Sidecars)
		}
	})

	t.Run("two jpeg-class files same stem", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "IMG_1234.JPG")
		touch(t, dir, "IMG_1234.HEIC")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("want 1 group, got %d", len(groups))
		}
		g := groups[0]
		if g.Kind != KindJPEGOnly || g.Raw != nil {
			t.Errorf("RAW slot must stay empty: %+v", g)
		}
		// .jpg precedes .heic in the default priority list
		if g.Jpeg == nil || filepath.Base(g.Jpeg.Path) != "IMG_1234.JPG" {
			t.Errorf("primary should be the JPG: %+v", g.Jpeg)
		}
		if len(g.Warnings) == 0 {
			t.Errorf("expected a warning badge for duplicate jpeg-class files")
		}
	})

	t.Run("unrecognised extensions ignored", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "notes.txt")
		touch(t, dir, "movie.mov")
		touch(t, dir, "a.jpg")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatalf("want only the jpg group, got %d", len(groups))
		}
	})

	t.Run("subdirectories are not merged", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "DSCF1234.RAF")
		touch(t, dir, filepath.Join("sub", "DSCF1234.JPG"))

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// non-recursive: only the top-level RAF
		if len(groups) != 1 || groups[0].Kind != KindRAWOnly {
			t.Fatalf("scan must not descend or merge across directories: %+v", groups)
		}
	})

	t.Run("groups sorted by stem", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "c.jpg")
		touch(t, dir, "a.jpg")
		touch(t, dir, "b.jpg")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, g := range groups {
			got = append(got, g.Stem)
		}
		want := []string{"a", "b", "c"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order: got %v want %v", got, want)
			}
		}
	})

	t.Run("shot falls back to mtime", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "a.jpg")

		groups, err := ScanDir(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if groups[0].Shot.IsZero() {
			t.Errorf("Shot must fall back to file mtime, got zero time")
		}
	})

	t.Run("missing directory returns error", func(t *testing.T) {
		_, err := ScanDir(filepath.Join(t.TempDir(), "nope"), cfg)
		if err == nil {
			t.Fatal("want error for missing directory")
		}
	})
}

// A symlinked frame is the target file wearing another name. The size and
// modification time must be the target's: they are what the catalogue compares
// on a rerun, and the link's own never change however the file is edited.
func TestSymlinkedFileCarriesTheTargetsMetadata(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "master.jpg")
	if err := os.WriteFile(target, make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "LINK0001.JPG")); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}

	groups, err := ScanDir(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Jpeg == nil {
		t.Fatalf("want the linked frame as one jpeg group, got %+v", groups)
	}
	if got := groups[0].Jpeg.Size; got != 1234 {
		t.Errorf("size = %d, want the target's 1234, not the link's own", got)
	}
}

// A symlink to a directory is not a directory by lstat, so it slips past the
// IsDir check whatever it points at. Named like an image it must still not
// become a phantom frame.
func TestSymlinkToADirectoryIsNotAFrame(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "REAL0001.JPG")
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "FAKE.JPG")); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}

	groups, err := ScanDir(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Stem != "REAL0001" {
		t.Errorf("groups = %+v, want REAL0001 alone — a link to a folder is not a frame", groups)
	}
}

func TestHiddenFilesAreIgnored(t *testing.T) {
	// macOS writes AppleDouble companions (._NAME.RAF) onto SMB and exFAT
	// volumes; they carry real image extensions but are not images.
	dir := t.TempDir()
	touch(t, dir, "DSCF1234.RAF")
	touch(t, dir, "._DSCF1234.RAF")
	touch(t, dir, ".DS_Store")
	touch(t, dir, ".hidden.jpg")

	groups, err := ScanDir(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("hidden files must not become frames: got %d groups", len(groups))
	}
	if groups[0].Stem != "DSCF1234" {
		t.Fatalf("wrong group survived: %q", groups[0].Stem)
	}
}

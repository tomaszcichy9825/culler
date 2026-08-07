package catalog

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func rootList(t *testing.T, s *Store) []string {
	t.Helper()
	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Path)
	}
	return out
}

func frameCount(t *testing.T, s *Store) int {
	t.Helper()
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return res.Total
}

func TestAddRootUnderAnExistingRootIsANoOp(t *testing.T) {
	s := openStore(t)
	if _, err := s.AddRoot(fix("/Volumes/shared/photos")); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}

	covering, err := s.AddRoot(fix("/Volumes/shared/photos/2026"))
	if err != nil {
		t.Fatalf("AddRoot a folder already covered: %v", err)
	}
	if covering.Path != fix("/Volumes/shared/photos") {
		t.Errorf("adding a covered folder returned %s, want the root that already covers it", covering.Path)
	}
	if got := rootList(t, s); len(got) != 1 || got[0] != fix("/Volumes/shared/photos") {
		t.Errorf("roots = %v, want the one that was registered", got)
	}
}

func TestAddRootAboveExistingRootsAbsorbsThem(t *testing.T) {
	s := openStore(t)
	base := t.TempDir()
	photos := filepath.Join(base, "photos")
	y2025 := filepath.Join(photos, "2025")
	y2026 := filepath.Join(photos, "2026")
	mkdir(t, y2025)
	mkdir(t, y2026)
	writeFrame(t, y2025, "OLD00001", 100, 0, shotAt(9, 0))
	writeFrame(t, y2026, "NEW00001", 100, 0, shotAt(10, 0))
	writeFrame(t, y2026, "NEW00002", 100, 0, shotAt(10, 1))

	// The state the user was in: each year added and indexed on its own.
	for _, root := range []string{y2025, y2026} {
		if _, err := s.Index(root, IndexOptions{}); err != nil {
			t.Fatalf("Index %s: %v", root, err)
		}
	}
	before := frameCount(t, s)
	if before != 3 {
		t.Fatalf("setup catalogued %d frames, want 3", before)
	}

	if _, err := s.AddRoot(photos); err != nil {
		t.Fatalf("AddRoot the parent: %v", err)
	}
	got := rootList(t, s)
	if len(got) != 1 || got[0] != photos {
		t.Fatalf("roots after adding their parent = %v, want %s alone", got, photos)
	}
	if after := frameCount(t, s); after != before {
		t.Errorf("absorbing the roots moved the frame count from %d to %d", before, after)
	}

	// Indexing the new root must not find the same frames again: they are keyed
	// on content, and their files have not moved.
	stats, err := s.Index(photos, IndexOptions{})
	if err != nil {
		t.Fatalf("Index the absorbing root: %v", err)
	}
	if stats.Changed != 0 {
		t.Errorf("indexing the parent re-read %d frames, want none — nothing on disk moved", stats.Changed)
	}
	if after := frameCount(t, s); after != before {
		t.Errorf("indexing the parent moved the frame count from %d to %d", before, after)
	}
}

func TestAddRootDoesNotAbsorbASiblingSharingAPrefix(t *testing.T) {
	s := openStore(t)
	if _, err := s.AddRoot(fix("/Volumes/CardTwo")); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	// Whole path segments: CardTwo does not live inside Card, and adding Card
	// must neither absorb it nor be turned away by it.
	if _, err := s.AddRoot(fix("/Volumes/Card")); err != nil {
		t.Fatalf("AddRoot a sibling: %v", err)
	}
	got := rootList(t, s)
	if len(got) != 2 {
		t.Errorf("roots = %v, want both — neither is inside the other", got)
	}
}

func TestAddRootAbsorbsEverythingBelowIt(t *testing.T) {
	s := openStore(t)
	for _, path := range []string{fix("/a/b/c"), fix("/a/b/d/e"), fix("/a/other")} {
		if _, err := s.AddRoot(path); err != nil {
			t.Fatalf("AddRoot %s: %v", path, err)
		}
	}
	if _, err := s.AddRoot(fix("/a")); err != nil {
		t.Fatalf("AddRoot the parent of them all: %v", err)
	}
	if got := rootList(t, s); len(got) != 1 || got[0] != fix("/a") {
		t.Errorf("roots = %v, want /a alone", got)
	}
}

func TestRemoveRootDropsTheFramesUnderIt(t *testing.T) {
	s := openStore(t)
	outer := t.TempDir()
	inner := filepath.Join(outer, "2026-05-01")
	mkdir(t, inner)
	writeFrame(t, outer, "OUTER001", 100, 0, shotAt(9, 0))
	writeFrame(t, inner, "INNER001", 100, 0, shotAt(9, 1))

	if _, err := s.Index(outer, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	// Asking for a folder the catalogue already covers changes nothing, so
	// there is no second root left holding the frames up.
	if _, err := s.AddRoot(inner); err != nil {
		t.Fatalf("AddRoot inner: %v", err)
	}
	if got := rootList(t, s); len(got) != 1 || got[0] != outer {
		t.Fatalf("roots = %v, want %s alone", got, outer)
	}

	if err := s.RemoveRoot(outer); err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if after := frameCount(t, s); after != 0 {
		t.Errorf("%d frames survive a root nothing else covers, want none", after)
	}
}

// A catalogue written before roots were kept apart can hold one root inside
// another. Opening it collapses them, because a user who is already in that
// state should not have to work out which rows to forget.
func TestOpenCollapsesNestingLeftByAnOlderBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{fix("/photos"), fix("/photos/2025"), fix("/photos/2026"), fix("/elsewhere")} {
		if _, err := raw.Exec(
			`INSERT INTO roots (path, added_at, last_indexed_at) VALUES (?, 1, 0)`, root); err != nil {
			t.Fatalf("seed %s: %v", root, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got := rootList(t, s)
	if len(got) != 2 {
		t.Fatalf("roots after opening = %v, want /photos and /elsewhere", got)
	}
	held := map[string]bool{}
	for _, path := range got {
		held[path] = true
	}
	for _, want := range []string{fix("/photos"), fix("/elsewhere")} {
		if !held[want] {
			t.Errorf("%s did not survive the collapse: %v", want, got)
		}
	}
	for _, gone := range []string{fix("/photos/2025"), fix("/photos/2026")} {
		if held[gone] {
			t.Errorf("%s is still a root, though /photos contains it", gone)
		}
	}
}

func TestUnderComparesWholePathSegments(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{"/Volumes/Card", "/Volumes/Card", true},
		{"/Volumes/Card/DCIM", "/Volumes/Card", true},
		{"/Volumes/Card/DCIM/100_FUJI", "/Volumes/Card", true},
		{"/Volumes/CardTwo", "/Volumes/Card", false},
		{"/Volumes/Card", "/Volumes/Card/DCIM", false},
		{"/Volumes", "/Volumes/Card", false},
		{"/anything", "/", true},
		{"/", "/", true},
	}
	for _, c := range cases {
		// The comparison runs on native separators, so the fixtures do too.
		if got := under(fix(c.path), fix(c.root)); got != c.want {
			t.Errorf("under(%q, %q) = %v, want %v", fix(c.path), fix(c.root), got, c.want)
		}
	}
}

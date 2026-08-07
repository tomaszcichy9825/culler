package app

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/catalog"
	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
)

// catalogued lays a small archive down, registers it and indexes it, returning
// the service and the root.
func catalogued(t *testing.T) (*LibraryIndexService, string) {
	t.Helper()
	s := indexService(t)
	root := t.TempDir()
	may := filepath.Join(root, "2026-05")
	june := filepath.Join(root, "2026-06")
	if err := os.MkdirAll(may, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(june, 0o755); err != nil {
		t.Fatal(err)
	}
	shoot(t, may, "DSCF0001", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))
	shoot(t, may, "DSCF0002", time.Date(2026, 5, 1, 9, 5, 0, 0, time.UTC))
	shoot(t, june, "DSCF0100", time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC))

	if _, err := s.RegisterRoot(root); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if _, err := s.reindex(root); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	return s, root
}

// mark records a verdict against the frame whose stem is given, the way CULL
// would, and returns its hash.
func mark(t *testing.T, s *LibraryIndexService, stem string, v decide.Verdict, rating int) string {
	t.Helper()
	res, err := s.Search(stem, FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search %s: %v", stem, err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("search for %s found %d frames, want 1", stem, len(res.Frames))
	}
	f := res.Frames[0]
	store, err := s.app.decisions()
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	if err := store.SetVerdict(f.Hash, f.Dir, f.Stem, v, decide.MaskBoth); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if rating > 0 {
		if err := store.SetRating(f.Hash, f.Dir, f.Stem, rating); err != nil {
			t.Fatalf("SetRating: %v", err)
		}
	}
	return f.Hash
}

func TestSearchShowsDecisionsMadeSinceTheIndexPass(t *testing.T) {
	s, _ := catalogued(t)
	mark(t, s, "DSCF0001", decide.Cut, 2)

	res, err := s.Search("DSCF0001", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("search found %d frames, want 1", len(res.Frames))
	}
	if res.Frames[0].Verdict != "cut" || res.Frames[0].Rating != 2 {
		t.Errorf("frame carries %q/%d, want the cut/2 recorded after the index pass",
			res.Frames[0].Verdict, res.Frames[0].Rating)
	}
}

func TestSearchDropsAVerdictTakenBackSinceTheIndexPass(t *testing.T) {
	s, root := catalogued(t)
	hash := mark(t, s, "DSCF0001", decide.Keep, 0)
	if _, err := s.reindex(root); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	store, err := s.app.decisions()
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	// Taking a verdict back names the same place it was given: decisions are
	// keyed on content and place, so a clear aimed elsewhere would miss.
	if err := store.SetVerdict(hash, filepath.Join(root, "2026-05"), "DSCF0001", decide.Undecided, decide.MaskBoth); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}

	res, err := s.Search("DSCF0001", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Frames[0].Verdict != "" {
		t.Errorf("frame still carries %q after the verdict was taken back", res.Frames[0].Verdict)
	}
}

func TestSearchWritesTheOverlayBackSoTheFacetCountsFollow(t *testing.T) {
	s, _ := catalogued(t)
	mark(t, s, "DSCF0001", decide.Cut, 0)

	// The facet counts are totalled in SQL over what the catalogue holds, so
	// they only follow the decision store because the overlay writes what it
	// found back into the row it read.
	if _, err := s.Search("", FacetsDTO{}, 0, 0); err != nil {
		t.Fatalf("Search: %v", err)
	}
	counts, err := s.Counts("", FacetsDTO{})
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	for _, row := range counts.Verdicts {
		want := 0
		switch row.Value {
		case "cut":
			want = 1
		case "undecided":
			want = 2
		}
		if row.Frames != want {
			t.Errorf("%s counts %d frames, want %d", row.Value, row.Frames, want)
		}
	}
}

func TestSessionsCountTheDecisionsAsTheyStandNow(t *testing.T) {
	s, _ := catalogued(t)
	mark(t, s, "DSCF0001", decide.Keep, 0)
	mark(t, s, "DSCF0002", decide.Cut, 0)

	sessions, err := s.Sessions(4)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	var may SessionDTO
	for _, sess := range sessions {
		if sess.Source == "2026-05" {
			may = sess
		}
	}
	if may.Frames != 2 {
		t.Fatalf("the May session holds %d frames, want 2: %+v", may.Frames, sessions)
	}
	if may.Kept != 1 || may.Cut != 1 || may.Undecided != 0 {
		t.Errorf("May session = %d kept, %d cut, %d undecided; want 1/1/0",
			may.Kept, may.Cut, may.Undecided)
	}
}

func TestTreeRootsCarryWhatIsUnderThem(t *testing.T) {
	s, root := catalogued(t)
	mark(t, s, "DSCF0001", decide.Keep, 0)

	nodes, err := s.TreeRoots()
	if err != nil {
		t.Fatalf("TreeRoots: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("%d root nodes, want 1: %+v", len(nodes), nodes)
	}
	if nodes[0].Path != root || nodes[0].Name != filepath.Base(root) {
		t.Errorf("root node = %+v, want %s", nodes[0], root)
	}
	if nodes[0].Frames != 3 {
		t.Errorf("root node holds %d frames, want 3", nodes[0].Frames)
	}
	if nodes[0].Undecided != 2 {
		t.Errorf("root node reports %d undecided, want the 2 nobody has judged", nodes[0].Undecided)
	}
	if !nodes[0].HasDirs {
		t.Error("root node says it has no subfolders")
	}
	if !nodes[0].IsRoot {
		t.Error("a root node does not say it is a root")
	}
}

func TestTreeChildrenAreTheFoldersUnderANode(t *testing.T) {
	s, root := catalogued(t)
	mark(t, s, "DSCF0001", decide.Cut, 0)

	children, err := s.TreeChildren(root)
	if err != nil {
		t.Fatalf("TreeChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("%d children, want the 2 month folders: %+v", len(children), children)
	}
	if children[0].Name != "2026-05" || children[1].Name != "2026-06" {
		t.Errorf("children = %s, %s, want name order", children[0].Name, children[1].Name)
	}
	if children[0].Frames != 2 || children[0].Undecided != 1 {
		t.Errorf("2026-05 = %d frames, %d undecided; want 2 and 1",
			children[0].Frames, children[0].Undecided)
	}
	if children[0].HasDirs || children[0].IsRoot {
		t.Errorf("2026-05 = %+v, want a leaf that is not a root", children[0])
	}
	if children[1].Undecided != 1 {
		t.Errorf("2026-06 reports %d undecided, want 1", children[1].Undecided)
	}
}

// The reported bug: a folder registered on top of folders already registered
// left all three at the top of the tree, the parent a superset of the others.
func TestRegisterRootAbsorbsTheRootsItContains(t *testing.T) {
	s := indexService(t)
	photos := t.TempDir()
	y2025 := filepath.Join(photos, "2025")
	y2026 := filepath.Join(photos, "2026")
	if err := os.MkdirAll(y2025, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(y2026, 0o755); err != nil {
		t.Fatal(err)
	}
	shoot(t, y2025, "OLD00001", time.Date(2025, 5, 1, 9, 0, 0, 0, time.UTC))
	shoot(t, y2026, "NEW00001", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))

	for _, root := range []string{y2025, y2026} {
		if _, err := s.RegisterRoot(root); err != nil {
			t.Fatalf("RegisterRoot %s: %v", root, err)
		}
		if _, err := s.reindex(root); err != nil {
			t.Fatalf("reindex %s: %v", root, err)
		}
	}
	before, err := s.Search("", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	roots, err := s.RegisterRoot(photos)
	if err != nil {
		t.Fatalf("RegisterRoot the parent: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != photos {
		t.Fatalf("roots after adding their parent = %+v, want %s alone", roots, photos)
	}

	nodes, err := s.TreeRoots()
	if err != nil {
		t.Fatalf("TreeRoots: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Path != photos {
		t.Fatalf("the tree has %d top-level nodes: %+v", len(nodes), nodes)
	}
	if nodes[0].Frames != before.Total {
		t.Errorf("the absorbing root holds %d frames, want the %d already catalogued",
			nodes[0].Frames, before.Total)
	}

	if _, err := s.reindex(photos); err != nil {
		t.Fatalf("reindex the parent: %v", err)
	}
	after, err := s.Search("", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if after.Total != before.Total {
		t.Errorf("indexing the parent moved the frame count from %d to %d", before.Total, after.Total)
	}

	// The years are reachable by opening the root, and only there.
	children, err := s.TreeChildren(photos)
	if err != nil {
		t.Fatalf("TreeChildren: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("%d children under the root, want the 2 years: %+v", len(children), children)
	}
}

func TestRegisterRootUnderAnExistingRootChangesNothing(t *testing.T) {
	s, root := catalogued(t)

	roots, err := s.RegisterRoot(filepath.Join(root, "2026-05"))
	if err != nil {
		t.Fatalf("RegisterRoot a covered folder: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != root {
		t.Fatalf("roots = %+v, want only %s", roots, root)
	}
	nodes, err := s.TreeRoots()
	if err != nil {
		t.Fatalf("TreeRoots: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("the tree has %d top-level nodes after adding a covered folder", len(nodes))
	}
}

func TestTreeChildrenOfSomethingUncataloguedIsEmpty(t *testing.T) {
	s, _ := catalogued(t)
	children, err := s.TreeChildren(t.TempDir())
	if err != nil {
		t.Fatalf("TreeChildren: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("an uncatalogued folder has %d children", len(children))
	}
}

func TestTreeLeavesTheUndecidedCountUnknownOnAHugeFolder(t *testing.T) {
	s, _ := catalogued(t)
	s.undecidedLimit = 1

	nodes, err := s.TreeRoots()
	if err != nil {
		t.Fatalf("TreeRoots: %v", err)
	}
	// Above the limit the count is not guessed at and not zero: it is unknown,
	// and the row draws without a badge rather than with a wrong one.
	if nodes[0].Undecided != UndecidedUnknown {
		t.Errorf("a folder past the limit reports %d undecided, want %d",
			nodes[0].Undecided, UndecidedUnknown)
	}
}

func TestPruneAppliedForgetsTheFramesAnApplyTrashed(t *testing.T) {
	s, _ := catalogued(t)
	hash := mark(t, s, "DSCF0001", decide.Cut, 0)

	if err := s.PruneApplied([]string{hash}); err != nil {
		t.Fatalf("PruneApplied: %v", err)
	}
	res, err := s.Search("", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("catalogue holds %d frames after the apply, want 2", res.Total)
	}
	for _, f := range res.Frames {
		if f.Hash == hash {
			t.Error("a frame that was trashed is still in the results")
		}
	}
	if err := s.PruneApplied(nil); err != nil {
		t.Errorf("PruneApplied(nil): %v", err)
	}
}

func TestPruneAppliedDoesNotCreateACatalogueThatWasNeverOpened(t *testing.T) {
	s := NewLibraryIndexService(testApp(t))
	t.Cleanup(func() { s.Close() })

	if err := s.PruneApplied([]string{"a hash"}); err != nil {
		t.Fatalf("PruneApplied without a catalogue: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.app.dataDir, catalogFile)); !os.IsNotExist(err) {
		t.Error("an apply created the catalogue file in an app that never visited LIBRARY")
	}
}

func TestUpsertDirCataloguesAFolderThatHasJustBeenCulled(t *testing.T) {
	s, root := catalogued(t)
	may := filepath.Join(root, "2026-05")
	shoot(t, may, "DSCF0003", time.Date(2026, 5, 1, 9, 10, 0, 0, time.UTC))

	if err := s.UpsertDir(may); err != nil {
		t.Fatalf("UpsertDir: %v", err)
	}
	res, err := s.Search("DSCF0003", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("the folder open catalogued %d new frames, want 1", res.Total)
	}
}

func TestUpsertDirOutsideEveryRootIsANoOp(t *testing.T) {
	s, _ := catalogued(t)
	elsewhere := t.TempDir()
	shoot(t, elsewhere, "OTHER001", time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC))

	if err := s.UpsertDir(elsewhere); err != nil {
		t.Fatalf("UpsertDir on an uncatalogued folder: %v", err)
	}
	res, err := s.Search("OTHER", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 {
		t.Error("opening a folder no root covers catalogued it anyway")
	}
}

// BenchmarkOverlay measures what the live overlay costs: one point query into
// the decision store per frame. The numbers quoted in the overlay and tree doc
// comments come from here, so a machine that disagrees can say so.
func BenchmarkOverlay(b *testing.B) {
	a := newAt(filepath.Join(b.TempDir(), "config.json"), b.TempDir(), config.Default())
	b.Cleanup(func() { a.Close() })
	s := NewLibraryIndexService(a)
	b.Cleanup(func() { s.Close() })

	dir := b.TempDir()
	const frames = 240
	for i := range frames {
		path := filepath.Join(dir, "DSCF"+strconv.Itoa(1000+i)+".JPG")
		if err := os.WriteFile(path, []byte("jpeg "+strconv.Itoa(i)), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := s.RegisterRoot(dir); err != nil {
		b.Fatal(err)
	}
	if _, err := s.reindex(dir); err != nil {
		b.Fatal(err)
	}
	store, err := s.catalogue()
	if err != nil {
		b.Fatal(err)
	}
	page, err := store.Search("", catalog.Facets{}, catalog.Page{})
	if err != nil {
		b.Fatal(err)
	}
	if len(page.Frames) != frames {
		b.Fatalf("indexed %d frames, want %d", len(page.Frames), frames)
	}

	b.ResetTimer()
	for b.Loop() {
		if err := s.overlay(store, page.Frames); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*frames), "ns/frame")
}

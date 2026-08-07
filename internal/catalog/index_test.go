package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// syntheticTree lays down the shape the indexer has to survive: nested
// directories, a hidden directory, a hidden file with a real image extension
// (macOS AppleDouble companions look exactly like this on exFAT), a file the
// scan does not recognise, and one frame of each kind.
func syntheticTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	day1 := filepath.Join(root, "2026-05-01")
	day2 := filepath.Join(root, "2026-05-02", "100_FUJI")
	hidden := filepath.Join(root, ".Trashes")
	mkdir(t, day1)
	mkdir(t, day2)
	mkdir(t, hidden)

	writeFrame(t, day1, "DSCF0001", 3000, 900, shotAt(9, 0)) // paired
	writeFrame(t, day1, "DSCF0002", 3100, 0, shotAt(9, 5))   // raw only
	writeFrame(t, day2, "DSCF0100", 0, 950, shotAt(14, 0))   // jpeg only
	writeFrame(t, hidden, "DSCF9999", 100, 0, shotAt(23, 0)) // inside a hidden dir
	writeFrame(t, day1, "._DSCF0001", 100, 0, shotAt(9, 0))  // hidden file
	if err := os.WriteFile(filepath.Join(day1, "notes.txt"), []byte("not a frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestIndexWalksTheTreeAndSkipsWhatIsNotAFrame(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	stats, err := s.Index(root, IndexOptions{Workers: 4})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if stats.Frames != 3 {
		t.Errorf("indexed %d frames, want the 3 real ones", stats.Frames)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("catalogue holds %d frames, want 3: %+v", res.Total, res.Frames)
	}

	byStem := map[string]Frame{}
	for _, f := range res.Frames {
		byStem[f.Stem] = f
	}
	for _, stem := range []string{"DSCF0001", "DSCF0002", "DSCF0100"} {
		if _, ok := byStem[stem]; !ok {
			t.Errorf("%s is missing from the catalogue", stem)
		}
	}
	for _, stem := range []string{"DSCF9999", "._DSCF0001", "notes"} {
		if _, ok := byStem[stem]; ok {
			t.Errorf("%s was indexed and should not have been", stem)
		}
	}

	paired := byStem["DSCF0001"]
	if paired.Kind != "paired" {
		t.Errorf("DSCF0001 kind = %q, want paired", paired.Kind)
	}
	if paired.RawBytes != 3000 || paired.JpegBytes != 900 {
		t.Errorf("DSCF0001 bytes = raw %d / jpeg %d, want 3000 / 900", paired.RawBytes, paired.JpegBytes)
	}
	if paired.Hash == "" {
		t.Error("DSCF0001 has no identity hash")
	}
	// The paths are what the preview route is keyed on, so a catalogued frame
	// that cannot name its files cannot be shown.
	if paired.RawPath != filepath.Join(paired.Dir, "DSCF0001.RAF") {
		t.Errorf("DSCF0001 raw path = %q", paired.RawPath)
	}
	if paired.JpegPath != filepath.Join(paired.Dir, "DSCF0001.JPG") {
		t.Errorf("DSCF0001 jpeg path = %q", paired.JpegPath)
	}
	if got := byStem["DSCF0002"]; got.JpegPath != "" || got.RawPath == "" {
		t.Errorf("a RAW-only frame reports jpeg %q / raw %q", got.JpegPath, got.RawPath)
	}
	if got := byStem["DSCF0100"]; got.RawPath != "" || got.JpegPath == "" {
		t.Errorf("a JPEG-only frame reports raw %q / jpeg %q", got.RawPath, got.JpegPath)
	}
	if !paired.Shot.Equal(shotAt(9, 0)) {
		t.Errorf("DSCF0001 shot = %v, want %v", paired.Shot, shotAt(9, 0))
	}
	if got := byStem["DSCF0100"].Kind; got != "jpeg-only" {
		t.Errorf("DSCF0100 kind = %q, want jpeg-only", got)
	}
	if got := byStem["DSCF0002"].Kind; got != "raw-only" {
		t.Errorf("DSCF0002 kind = %q, want raw-only", got)
	}
}

func TestIndexRegistersTheRootItWasGiven(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != root {
		t.Errorf("Roots = %+v, want the indexed root registered", roots)
	}
}

func TestIndexReportsProgressPerDirectory(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	var mu sync.Mutex
	var seen []Progress
	_, err := s.Index(root, IndexOptions{Progress: func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, p)
	}})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(seen) < 2 {
		t.Fatalf("%d progress reports, want one per directory plus the final one", len(seen))
	}

	last := seen[len(seen)-1]
	if !last.Done {
		t.Error("the last progress report is not marked done")
	}
	if last.Frames != 3 {
		t.Errorf("final progress reports %d frames, want 3", last.Frames)
	}
	if last.Root != root {
		t.Errorf("progress reports root %q, want %q", last.Root, root)
	}
	// Counts only ever climb, so a bar driven by them never goes backwards.
	for i := 1; i < len(seen); i++ {
		if seen[i].Frames < seen[i-1].Frames || seen[i].Dirs < seen[i-1].Dirs {
			t.Fatalf("progress went backwards: %+v then %+v", seen[i-1], seen[i])
		}
	}
}

func TestReindexIsIdempotent(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	first, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	second, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if first.Frames != second.Frames {
		t.Errorf("reindex found %d frames, first pass found %d", second.Frames, first.Frames)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("catalogue holds %d frames after two passes, want 3", res.Total)
	}
	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 1 {
		t.Errorf("%d roots after two passes, want 1", len(roots))
	}
}

func TestReindexDropsFramesThatLeftTheDisk(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	day := filepath.Join(root, "2026-05-01")
	other := filepath.Join(root, "2026-05-02")
	mkdir(t, day)
	mkdir(t, other)
	writeFrame(t, day, "DSCF0001", 100, 0, shotAt(9, 0))
	writeFrame(t, day, "DSCF0002", 100, 0, shotAt(9, 1))
	writeFrame(t, other, "DSCF0003", 100, 0, shotAt(10, 0))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("first index: %v", err)
	}
	if err := os.Remove(filepath.Join(day, "DSCF0002.RAF")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("second index: %v", err)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Frames[0].Stem != "DSCF0001" {
		t.Errorf("frames after the deletions = %+v, want DSCF0001 alone", res.Frames)
	}
}

// A directory that cannot be listed is not a directory that has left the
// disk. Its frames must survive the pass: a permissions hiccup or a card
// mid-unplug looks exactly like this, and forgetting a folder of photographs
// on the strength of one failed listing would be the wrong guess.
func TestReindexKeepsFramesOfAnUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions do not bite")
	}
	s := openStore(t)
	root := t.TempDir()
	open := filepath.Join(root, "open")
	locked := filepath.Join(root, "locked")
	mkdir(t, open)
	mkdir(t, locked)
	writeFrame(t, open, "OPEN0001", 100, 0, shotAt(9, 0))
	writeFrame(t, locked, "LOCK0001", 100, 0, shotAt(9, 1))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("first index: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("second index: %v", err)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("catalogue holds %d frames after walking past an unreadable folder, want both: %v", res.Total, stems(res))
	}
}

// The worst case of the same mistake: the root itself stops being readable,
// the walk reaches nothing, and a prune keyed on what was reached would empty
// the whole catalogue under it.
func TestReindexKeepsEverythingWhenTheRootIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions do not bite")
	}
	s := openStore(t)
	root := t.TempDir()
	day := filepath.Join(root, "2026-05-01")
	mkdir(t, day)
	writeFrame(t, day, "DSCF0001", 100, 0, shotAt(9, 0))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("first index: %v", err)
	}
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("second index: %v", err)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("catalogue holds %d frames after a walk that could not start, want the 1 it held before", res.Total)
	}
}

// seedDir files n synthetic rows under dir, bypassing the scanner: the prune
// tests care about rows, not files on disk.
func seedDir(t *testing.T, s *Store, dir string, n int) []FrameKey {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	keys := make([]FrameKey, 0, n)
	for i := 0; i < n; i++ {
		h := fmt.Sprintf("seed-%05d", i)
		stem := fmt.Sprintf("SEED%05d", i)
		if _, err := tx.Exec(upsertFrameSQL,
			h, dir, stem, "raw-only", int64(i),
			filepath.Join(dir, stem+".RAF"), "", int64(100), int64(0),
			int64(0), int64(0), 0, "", int64(0)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
		keys = append(keys, FrameKey{Hash: h, Dir: dir, Stem: stem})
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return keys
}

// A flat folder can hold more frames than the driver has parameter slots in
// one statement, and the keep list must not be bound as one.
func TestPruneDirSurvivesAKeepListPastTheParameterCeiling(t *testing.T) {
	s := openStore(t)
	dir := "/photos/flat"
	keep := seedDir(t, s, dir, 40)
	for i := len(keep); i < 33000; i++ {
		keep = append(keep, FrameKey{
			Hash: fmt.Sprintf("ghost-%05d", i), Dir: dir, Stem: fmt.Sprintf("GHOST%05d", i),
		})
	}

	removed, err := s.pruneDir(dir, keep)
	if err != nil {
		t.Fatalf("pruneDir with %d survivors: %v", len(keep), err)
	}
	if removed != 0 {
		t.Errorf("pruned %d frames, every one was in the keep list", removed)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE dir = ?`, dir).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 40 {
		t.Errorf("%d rows survive, want all 40", n)
	}
}

func TestPruneDirDropsAcrossChunks(t *testing.T) {
	s := openStore(t)
	dir := "/photos/flat"
	hashes := seedDir(t, s, dir, 1200)
	keep := hashes[:699] // dooms one full delete batch and a remainder

	removed, err := s.pruneDir(dir, keep)
	if err != nil {
		t.Fatalf("pruneDir: %v", err)
	}
	if removed != 501 {
		t.Errorf("pruned %d frames, want the 501 outside the keep list", removed)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE dir = ?`, dir).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 699 {
		t.Errorf("%d rows survive, want the 699 kept", n)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE dir = ? AND hash = ?`, dir, keep[0].Hash).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("a kept hash did not survive the prune")
	}
}

func TestIndexAllCoversEveryRoot(t *testing.T) {
	s := openStore(t)
	base := t.TempDir()
	one := filepath.Join(base, "one")
	two := filepath.Join(base, "two")
	mkdir(t, one)
	mkdir(t, two)
	writeFrame(t, one, "ONE00001", 100, 0, shotAt(9, 0))
	writeFrame(t, two, "TWO00001", 100, 0, shotAt(9, 1))
	writeFrame(t, two, "TWO00002", 100, 0, shotAt(9, 2))

	stats, err := s.IndexAll([]string{one, two}, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if stats.Frames != 3 {
		t.Errorf("IndexAll found %d frames, want 3", stats.Frames)
	}
	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if len(roots) != 2 {
		t.Errorf("%d roots after IndexAll, want 2", len(roots))
	}
}

// IndexAll with no argument indexes the roots the user has already
// registered, which is what a startup refresh does.
func TestIndexAllWithNoRootsUsesTheRegisteredOnes(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "DSCF0001", 100, 0, shotAt(9, 0))
	if _, err := s.AddRoot(root); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}

	stats, err := s.IndexAll(nil, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if stats.Frames != 1 {
		t.Errorf("IndexAll over the registered roots found %d frames, want 1", stats.Frames)
	}
}

func TestIndexCarriesTheDecisionsTheLookupReturns(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "DSCF0001", 100, 0, shotAt(9, 0))
	writeFrame(t, root, "DSCF0002", 100, 0, shotAt(9, 1))

	// Every frame the lookup is asked about gets the same answer, so the test
	// does not have to know the hashes in advance.
	_, err := s.Index(root, IndexOptions{Lookup: func(hash, dir, stem string) (string, int) {
		return "cut", 3
	}})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, f := range res.Frames {
		if f.Verdict != "cut" || f.Rating != 3 {
			t.Errorf("%s carries %q/%d, want the looked-up cut/3", f.Stem, f.Verdict, f.Rating)
		}
	}
}

func TestIndexRejectsSomethingThatIsNotADirectory(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	file := filepath.Join(root, "DSCF0001.RAF")
	if err := os.WriteFile(file, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Index(file, IndexOptions{}); err == nil {
		t.Error("indexing a file was accepted")
	}
	if _, err := s.Index(filepath.Join(root, "missing"), IndexOptions{}); err == nil {
		t.Error("indexing a path that is not there was accepted")
	}
}

package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/hash"
)

// countingHash wraps the real content hasher and counts how many files were
// actually read, which is the whole question the two-phase index answers.
func countingHash(reads *atomic.Int64) func(string) (string, error) {
	return func(path string) (string, error) {
		reads.Add(1)
		return hash.Content(path)
	}
}

// failingHash refuses every read, which is what a cloud placeholder that will
// not download looks like — and what an interrupted second phase leaves behind.
func failingHash(reads *atomic.Int64) func(string) (string, error) {
	return func(path string) (string, error) {
		reads.Add(1)
		return "", errors.New("no content today")
	}
}

func TestIndexWritesListingRowsBeforeAnyContentRead(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	var reads atomic.Int64
	var mu sync.Mutex
	var atListing int64 = -1
	var listed Results
	var searchErr error

	_, err := s.Index(root, IndexOptions{
		hashFile: countingHash(&reads),
		Progress: func(p Progress) {
			mu.Lock()
			defer mu.Unlock()
			if p.Phase != PhaseHashing || atListing >= 0 {
				return
			}
			// The first hashing report means the listing has landed. Everything
			// the tree and the search need must already be rows, and not one
			// file may have been read for them.
			atListing = reads.Load()
			listed, searchErr = s.Search("", Facets{}, Page{})
		},
	})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if searchErr != nil {
		t.Fatalf("Search during the pass: %v", searchErr)
	}
	if atListing < 0 {
		t.Fatal("no hashing report arrived — the pass never announced its second phase")
	}
	if atListing != 0 {
		t.Errorf("%d files were read before the listing landed, want none", atListing)
	}
	if listed.Total != 3 {
		t.Fatalf("the listing catalogued %d frames, want 3: %v", listed.Total, stems(listed))
	}
	byStem := map[string]Frame{}
	for _, f := range listed.Frames {
		byStem[f.Stem] = f
		if f.Hash != "" {
			t.Errorf("%s carries hash %q before anything was read", f.Stem, f.Hash)
		}
	}
	paired := byStem["DSCF0001"]
	if paired.Kind != "paired" || paired.RawBytes != 3000 || paired.JpegBytes != 900 {
		t.Errorf("the listing row lost the listing: %+v", paired)
	}
	if paired.RawPath == "" || paired.JpegPath == "" {
		t.Errorf("the listing row cannot name its files: %+v", paired)
	}
	if paired.Shot.IsZero() {
		t.Error("the listing row has no shot time — mtime was there for the taking")
	}
}

func TestIndexFillsIdentitiesBehindTheListing(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	var reads atomic.Int64
	stats, err := s.Index(root, IndexOptions{
		hashFile: countingHash(&reads),
		Lookup: func(h, dir, stem string) (string, int) {
			if h == "" {
				return "", 0
			}
			return VerdictKeep, 2
		},
	})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if stats.Frames != 3 || stats.Changed != 3 {
		t.Errorf("stats = %+v, want 3 frames all read", stats)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("%d content reads for 3 frames, want exactly 3", got)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("catalogue holds %d frames, want 3 — a filled row must replace its listing row, not join it: %v",
			res.Total, stems(res))
	}
	for _, f := range res.Frames {
		if f.Hash == "" {
			t.Errorf("%s never got its identity", f.Stem)
		}
		if f.Verdict != VerdictKeep || f.Rating != 2 {
			t.Errorf("%s carries verdict %q rating %d, want the lookup's keep/2", f.Stem, f.Verdict, f.Rating)
		}
	}
}

func TestReindexOfAnUnchangedRootReadsNoContent(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)
	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("first index: %v", err)
	}

	var reads atomic.Int64
	stats, err := s.Index(root, IndexOptions{hashFile: countingHash(&reads)})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if got := reads.Load(); got != 0 {
		t.Errorf("a rerun over an untouched root read %d files, want none", got)
	}
	if stats.Changed != 0 || stats.Frames != 3 {
		t.Errorf("stats = %+v, want 3 frames none rewritten", stats)
	}
}

func TestPendingRowsFillInOnTheNextPass(t *testing.T) {
	s := openStore(t)
	root := syntheticTree(t)

	// The first pass lists everything and can read nothing, so every row is
	// left pending — exactly what an interrupted second phase leaves behind.
	var refused atomic.Int64
	stats, err := s.Index(root, IndexOptions{hashFile: failingHash(&refused)})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	if stats.Unreadable != 3 {
		t.Errorf("stats report %d unreadable frames, want all 3", stats.Unreadable)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("the listing survived as %d rows, want 3 — pending rows must not be pruned: %v",
			res.Total, stems(res))
	}

	// The next pass can read again. The pending rows fill in, in place.
	stats, err = s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Changed != 3 {
		t.Errorf("second pass filled %d rows, want all 3", stats.Changed)
	}
	res, err = s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("catalogue holds %d frames after filling in, want 3: %v", res.Total, stems(res))
	}
	for _, f := range res.Frames {
		if f.Hash == "" {
			t.Errorf("%s is still pending after a pass that could read", f.Stem)
		}
	}
}

func TestPendingRowOfADeletedFrameIsPruned(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "DSCF0001", 100, 0, shotAt(9, 0))
	writeFrame(t, root, "DSCF0002", 120, 0, shotAt(9, 5))

	var refused atomic.Int64
	if _, err := s.Index(root, IndexOptions{hashFile: failingHash(&refused)}); err != nil {
		t.Fatalf("first index: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "DSCF0002.RAF")); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Removed != 1 {
		t.Errorf("second pass dropped %d rows, want the deleted frame's pending row", stats.Removed)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Frames[0].Stem != "DSCF0001" {
		t.Errorf("catalogue holds %v, want only DSCF0001", stems(res))
	}
	if res.Frames[0].Hash == "" {
		t.Error("the surviving frame is still pending")
	}
}

func TestIndexReportsBothPhases(t *testing.T) {
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

	var listing, hashing int
	lastHashed, pending := -1, 0
	for _, p := range seen {
		switch p.Phase {
		case PhaseListing:
			listing++
			if hashing > 0 && !p.Done {
				t.Errorf("a listing report arrived after hashing began: %+v", p)
			}
		case PhaseHashing:
			hashing++
			if p.Hashed < lastHashed {
				t.Errorf("hashed count went backwards: %d then %d", lastHashed, p.Hashed)
			}
			lastHashed = p.Hashed
			pending = p.Pending
		}
	}
	if listing == 0 {
		t.Error("no listing reports")
	}
	if hashing == 0 {
		t.Fatal("no hashing reports")
	}
	if pending != 3 {
		t.Errorf("hashing reports a total of %d frames to read, want 3", pending)
	}
	if lastHashed != 3 {
		t.Errorf("the last hashing report has read %d of them, want all 3", lastHashed)
	}
	last := seen[len(seen)-1]
	if !last.Done {
		t.Error("the last report is not marked done")
	}
}

func TestRemoveFramesDropsAPendingRow(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "DSCF0001", 100, 0, shotAt(9, 0))

	var refused atomic.Int64
	if _, err := s.Index(root, IndexOptions{hashFile: failingHash(&refused)}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// An apply knows the frame by its real identity — it hashed the file from
	// disk at plan time — while the catalogue still holds the pending row. The
	// removal must take the pending row with it, or a trashed frame lingers in
	// search until the next pass.
	real, err := hash.Content(filepath.Join(root, "DSCF0001.RAF"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFrames([]FrameKey{{Hash: real, Dir: root, Stem: "DSCF0001"}}); err != nil {
		t.Fatalf("RemoveFrames: %v", err)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("the pending row survived the removal: %v", stems(res))
	}
}

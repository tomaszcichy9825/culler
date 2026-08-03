package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
)

// indexService returns a service over a temporary app, closed with the test.
func indexService(t *testing.T) *LibraryIndexService {
	t.Helper()
	s := NewLibraryIndexService(testApp(t))
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close index service: %v", err)
		}
	})
	return s
}

// shoot writes a frame with a controlled shot time, since the catalogue reads
// the mtime as the time the frame was taken.
func shoot(t *testing.T, dir, stem string, shot time.Time) {
	t.Helper()
	path := filepath.Join(dir, stem+".JPG")
	if err := os.WriteFile(path, []byte("jpeg bytes for "+stem), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, shot, shot); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRootIndexesAndReports(t *testing.T) {
	s := indexService(t)
	dir := card(t)

	roots, err := s.RegisterRoot(dir)
	if err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != dir {
		t.Fatalf("RegisterRoot returned %+v, want the one root", roots)
	}
	// Registering does not index — that is a goroutine the UI starts — so the
	// root arrives empty and says so.
	if roots[0].Frames != 0 {
		t.Errorf("a freshly registered root already reports %d frames", roots[0].Frames)
	}
	if roots[0].LastIndexed != "" {
		t.Errorf("a never-indexed root reports LastIndexed = %q", roots[0].LastIndexed)
	}

	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	roots, err = s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if roots[0].Frames != 2 {
		t.Errorf("root holds %d frames after indexing, want the card's 2", roots[0].Frames)
	}
	if roots[0].LastIndexed == "" {
		t.Error("an indexed root still reports no LastIndexed")
	}
	if roots[0].Volume == "" {
		t.Error("root reports no volume")
	}
}

func TestRegisterRootExpandsTheUsersPath(t *testing.T) {
	s := indexService(t)
	if _, err := s.RegisterRoot(""); err == nil {
		t.Error("an empty root was accepted")
	}
	// A relative path is resolved rather than rejected: the picker hands over
	// whatever the user typed.
	dir := card(t)
	roots, err := s.RegisterRoot(dir + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if len(roots) != 1 || roots[0].Path != dir {
		t.Errorf("RegisterRoot(%q) recorded %+v, want the cleaned path", dir+"/.", roots)
	}
}

func TestRemoveRootDropsItsFrames(t *testing.T) {
	s := indexService(t)
	dir := card(t)

	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	roots, err := s.RemoveRoot(dir)
	if err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("Roots after removal = %+v, want none", roots)
	}
	res, err := s.Search("", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("%d frames survive the root that held them", res.Total)
	}
}

func TestSearchReturnsFramesWithTheirDecisions(t *testing.T) {
	s := indexService(t)
	dir := card(t)

	// The catalogue reads the decision store, so a frame marked in CULL comes
	// back marked in LIBRARY.
	folder, err := NewLibraryService(s.app).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	store, err := s.app.decisions()
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	target := folder.Groups[0]
	if err := store.SetVerdict(target.Hash, target.Dir, target.Stem, decide.Cut, decide.MaskBoth); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if err := store.SetRating(target.Hash, target.Dir, target.Stem, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	res, err := s.Search(target.Stem, FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("search for %s found %d frames", target.Stem, res.Total)
	}
	got := res.Frames[0]
	if got.Verdict != "cut" || got.Rating != 4 {
		t.Errorf("frame carries %q/%d, want the recorded cut/4", got.Verdict, got.Rating)
	}
	if got.Shot == "" {
		t.Error("frame has no shot timestamp")
	}
	if got.Bytes != got.RawBytes+got.JpegBytes {
		t.Errorf("Bytes = %d, want raw %d + jpeg %d", got.Bytes, got.RawBytes, got.JpegBytes)
	}
	if res.Elapsed < 0 {
		t.Errorf("Elapsed = %d ms", res.Elapsed)
	}
}

func TestSearchFacetsAndPaging(t *testing.T) {
	s := indexService(t)
	dir := t.TempDir()
	for i, stem := range []string{"DSCF0001", "DSCF0002", "DSCF0003"} {
		shoot(t, dir, stem, time.Date(2026, 5, 1, 9+i, 0, 0, 0, time.UTC))
	}
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	page, err := s.Search("", FacetsDTO{}, 2, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Frames) != 2 || page.Total != 3 {
		t.Errorf("page holds %d of %d, want 2 of 3", len(page.Frames), page.Total)
	}

	kinds, err := s.Search("", FacetsDTO{Kind: "jpeg-only"}, 0, 0)
	if err != nil {
		t.Fatalf("Search by kind: %v", err)
	}
	if kinds.Total != 3 {
		t.Errorf("kind facet found %d frames, want 3", kinds.Total)
	}

	// The date range comes over as RFC3339 text, because that is what the
	// frontend has.
	ranged, err := s.Search("", FacetsDTO{
		From: "2026-05-01T10:00:00Z",
		To:   "2026-05-01T11:00:00Z",
	}, 0, 0)
	if err != nil {
		t.Fatalf("Search by date: %v", err)
	}
	if ranged.Total != 1 {
		t.Errorf("date range found %d frames, want 1", ranged.Total)
	}
	if _, err := s.Search("", FacetsDTO{From: "yesterday"}, 0, 0); err == nil {
		t.Error("an unparseable date was accepted")
	}
}

func TestCountsDTO(t *testing.T) {
	s := indexService(t)
	dir := card(t)
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	counts, err := s.Counts("", FacetsDTO{})
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counts.Total != 2 {
		t.Errorf("Total = %d, want 2", counts.Total)
	}
	// The card holds one paired frame and one JPEG-only one, and neither has
	// been judged.
	kinds := map[string]int{}
	for _, c := range counts.Kinds {
		kinds[c.Value] = c.Frames
	}
	if kinds["paired"] != 1 || kinds["jpeg-only"] != 1 {
		t.Errorf("kind counts = %+v", counts.Kinds)
	}
	// Every facet value is listed, including the ones holding nothing, so the
	// list does not reflow as the user narrows it.
	if len(counts.Kinds) != 3 || len(counts.Verdicts) != 3 || len(counts.Ratings) != 5 {
		t.Errorf("facet lists = %d kinds / %d verdicts / %d ratings, want 3/3/5",
			len(counts.Kinds), len(counts.Verdicts), len(counts.Ratings))
	}
	if _, err := s.Counts("", FacetsDTO{Kind: "banana"}); err == nil {
		t.Error("an unknown kind facet was accepted")
	}
}

func TestSessionsDTO(t *testing.T) {
	s := indexService(t)
	dir := t.TempDir()
	shoot(t, dir, "MORN0001", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))
	shoot(t, dir, "MORN0002", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	shoot(t, dir, "EVEN0001", time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC))
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("%d sessions, want 2: %+v", len(sessions), sessions)
	}
	// Newest first, and the span is minutes the UI can print without maths.
	if sessions[0].Frames != 1 || sessions[1].Frames != 2 {
		t.Errorf("session frame counts = %d, %d, want 1 then 2", sessions[0].Frames, sessions[1].Frames)
	}
	if sessions[1].SpanMinutes != 60 {
		t.Errorf("morning span = %d minutes, want 60", sessions[1].SpanMinutes)
	}
	if sessions[1].Start == "" || sessions[1].End == "" {
		t.Errorf("session carries no timestamps: %+v", sessions[1])
	}
	if sessions[1].Undecided != 2 {
		t.Errorf("morning session reports %d undecided, want 2", sessions[1].Undecided)
	}
	if sessions[1].Source == "" {
		t.Error("session names no source")
	}

	// A gap in hours, the way the setting is written.
	wide, err := s.Sessions(12)
	if err != nil {
		t.Fatalf("Sessions(12): %v", err)
	}
	if len(wide) != 1 {
		t.Errorf("%d sessions at a twelve-hour gap, want 1", len(wide))
	}
}

func TestStorageDTO(t *testing.T) {
	s := indexService(t)
	dir := card(t)
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	storage, err := s.Storage()
	if err != nil {
		t.Fatalf("Storage: %v", err)
	}
	if storage.Frames != 2 {
		t.Errorf("storage counts %d frames, want 2", storage.Frames)
	}
	if len(storage.Roots) != 1 || storage.Roots[0].Root != dir {
		t.Fatalf("storage roots = %+v", storage.Roots)
	}
	if storage.Roots[0].Bytes != storage.Roots[0].RawBytes+storage.Roots[0].JpegBytes {
		t.Error("root Bytes does not add up")
	}
	if len(storage.Volumes) != 1 {
		t.Fatalf("%d volumes, want 1", len(storage.Volumes))
	}
	if storage.Volumes[0].Frames != 2 || len(storage.Volumes[0].Roots) != 1 {
		t.Errorf("volume rollup = %+v", storage.Volumes[0])
	}
	if storage.Bytes == 0 {
		t.Error("storage reports no bytes at all")
	}
}

// Reindex hands the walk to a goroutine and returns at once; the caller learns
// how it went from the progress event.
func TestReindexRunsInTheBackgroundAndReportsProgress(t *testing.T) {
	s := indexService(t)
	dir := card(t)
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}

	done := make(chan CatalogProgress, 8)
	s.onProgress = func(p CatalogProgress) {
		if p.Done {
			done <- p
		}
	}
	if err := s.Reindex(dir); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	select {
	case p := <-done:
		if p.Frames != 2 || p.Root != dir || p.Error != "" {
			t.Errorf("final progress = %+v, want 2 frames on %s with no error", p, dir)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the background index never reported that it had finished")
	}
	if s.Indexing() {
		t.Error("the service still reports itself as indexing")
	}
}

func TestReindexReportsAFailureThroughTheEvent(t *testing.T) {
	s := indexService(t)
	missing := filepath.Join(t.TempDir(), "unplugged")

	done := make(chan CatalogProgress, 4)
	s.onProgress = func(p CatalogProgress) {
		if p.Done {
			done <- p
		}
	}
	if err := s.Reindex(missing); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	select {
	case p := <-done:
		if p.Error == "" {
			t.Errorf("indexing an unplugged root finished clean: %+v", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the failed index never reported back")
	}
}

// A second Reindex while one is running is refused rather than queued: two
// walks over the same card would only make both slower.
func TestReindexRefusesToRunTwice(t *testing.T) {
	s := indexService(t)
	dir := card(t)

	release := make(chan struct{})
	s.onProgress = func(p CatalogProgress) {
		if !p.Done {
			<-release
		}
	}
	if err := s.Reindex(dir); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !s.Indexing() {
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("the background index never started")
		}
		time.Sleep(time.Millisecond)
	}
	err := s.Reindex(dir)
	close(release)
	if err == nil {
		t.Error("a second index started while the first was still running")
	}
}

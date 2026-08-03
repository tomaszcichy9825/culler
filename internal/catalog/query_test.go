package catalog

import (
	"path/filepath"
	"testing"
	"time"
)

// searchTree is a catalogue with enough variety to exercise every facet: two
// roots, three kinds, both verdicts, ratings from nothing to four stars, and
// frames a day apart.
func searchTree(t *testing.T) (*Store, string, string) {
	t.Helper()
	s := openStore(t)
	base := t.TempDir()
	cards := filepath.Join(base, "cards")
	archive := filepath.Join(base, "archive")
	mkdir(t, cards)
	mkdir(t, archive)

	writeFrame(t, cards, "DSCF0001", 3000, 900, shotAt(9, 0))
	writeFrame(t, cards, "DSCF0002", 3000, 0, shotAt(9, 30))
	writeFrame(t, cards, "PANO0003", 0, 900, shotAt(10, 0))
	writeFrame(t, archive, "OLD00001", 2000, 0, day(2026, 4, 1, 12, 0))
	writeFrame(t, archive, "OLD00002", 2000, 0, day(2026, 4, 2, 12, 0))

	marks := map[string]struct {
		verdict string
		rating  int
	}{
		"DSCF0001": {"keep", 4},
		"DSCF0002": {"cut", 0},
		"PANO0003": {"keep", 2},
		"OLD00001": {"", 5},
		"OLD00002": {"", 0},
	}
	// The lookup is keyed on the hash, which the test does not know, so it
	// resolves through the path the hash was taken from instead.
	byHash := map[string]string{}
	opts := IndexOptions{Lookup: func(hash string) (string, int) {
		m := marks[byHash[hash]]
		return m.verdict, m.rating
	}}
	// A first pass to learn the hashes, a second to attach the decisions.
	for _, root := range []string{cards, archive} {
		if _, err := s.Index(root, IndexOptions{}); err != nil {
			t.Fatalf("Index %s: %v", root, err)
		}
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, f := range res.Frames {
		byHash[f.Hash] = f.Stem
	}
	for _, root := range []string{cards, archive} {
		if _, err := s.Index(root, opts); err != nil {
			t.Fatalf("reindex %s: %v", root, err)
		}
	}
	return s, cards, archive
}

func day(y int, m time.Month, d, hour, minute int) time.Time {
	return time.Date(y, m, d, hour, minute, 0, 0, time.UTC)
}

func stems(res Results) []string {
	out := make([]string, 0, len(res.Frames))
	for _, f := range res.Frames {
		out = append(out, f.Stem)
	}
	return out
}

func TestSearchNewestFirst(t *testing.T) {
	s, _, _ := searchTree(t)

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 5 {
		t.Fatalf("Total = %d, want 5", res.Total)
	}
	for i := 1; i < len(res.Frames); i++ {
		if res.Frames[i].Shot.After(res.Frames[i-1].Shot) {
			t.Fatalf("results are not newest first: %v", stems(res))
		}
	}
}

func TestSearchMatchesStemAndFolder(t *testing.T) {
	s, _, _ := searchTree(t)

	res, err := s.Search("pano", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := stems(res); len(got) != 1 || got[0] != "PANO0003" {
		t.Errorf("search for pano = %v, want PANO0003 — the match is case-insensitive", got)
	}

	res, err = s.Search("archive", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("search for a folder name found %d frames, want the archive's 2", res.Total)
	}
}

// A path holding a LIKE wildcard must be matched literally, not treated as a
// pattern that swallows the whole catalogue. Both wildcards are put in one
// folder name, so a broken escape shows up as the plain folder matching too.
func TestSearchTreatsWildcardsAsText(t *testing.T) {
	s := openStore(t)
	base := t.TempDir()
	odd := filepath.Join(base, "100%_CARD")
	plain := filepath.Join(base, "plain")
	mkdir(t, odd)
	mkdir(t, plain)
	writeFrame(t, odd, "ODD00001", 100, 0, shotAt(9, 0))
	writeFrame(t, plain, "PLAIN001", 100, 0, shotAt(9, 1))
	if _, err := s.Index(base, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	for _, query := range []string{"%_", "100%", "%_CARD"} {
		res, err := s.Search(query, Facets{}, Page{})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if got := stems(res); len(got) != 1 || got[0] != "ODD00001" {
			t.Errorf("search for %q = %v, want the one folder whose name holds it", query, got)
		}
	}
}

func TestSearchFacets(t *testing.T) {
	s, cards, _ := searchTree(t)

	tests := []struct {
		name   string
		facets Facets
		want   []string
	}{
		{"kind", Facets{Kind: "raw-only"}, []string{"DSCF0002", "OLD00002", "OLD00001"}},
		{"verdict keep", Facets{Verdict: VerdictKeep}, []string{"PANO0003", "DSCF0001"}},
		{"verdict cut", Facets{Verdict: VerdictCut}, []string{"DSCF0002"}},
		{"verdict undecided", Facets{Verdict: VerdictNone}, []string{"OLD00002", "OLD00001"}},
		{"min rating", Facets{MinRating: 4}, []string{"DSCF0001", "OLD00001"}},
		{"root", Facets{Root: cards}, []string{"PANO0003", "DSCF0002", "DSCF0001"}},
		{
			"date range",
			Facets{From: day(2026, 4, 2, 0, 0), To: day(2026, 4, 3, 0, 0)},
			[]string{"OLD00002"},
		},
		{"combined", Facets{Root: cards, Verdict: VerdictKeep, MinRating: 3}, []string{"DSCF0001"}},
	}
	for _, tt := range tests {
		res, err := s.Search("", tt.facets, Page{})
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		got := stems(res)
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestSearchPaging(t *testing.T) {
	s, _, _ := searchTree(t)

	first, err := s.Search("", Facets{}, Page{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(first.Frames) != 2 {
		t.Fatalf("first page holds %d frames, want 2", len(first.Frames))
	}
	// Total is the size of the whole result, not of the page, or the count in
	// the title bar would change as the user scrolls.
	if first.Total != 5 {
		t.Errorf("Total on a limited page = %d, want the full 5", first.Total)
	}

	second, err := s.Search("", Facets{}, Page{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(second.Frames) != 2 || second.Offset != 2 {
		t.Fatalf("second page = %d frames at offset %d", len(second.Frames), second.Offset)
	}
	if second.Frames[0].Stem == first.Frames[0].Stem {
		t.Error("the second page repeats the first")
	}

	past, err := s.Search("", Facets{}, Page{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("Search past the end: %v", err)
	}
	if len(past.Frames) != 0 || past.Total != 5 {
		t.Errorf("past the end = %d frames, Total %d", len(past.Frames), past.Total)
	}
}

func TestParseQuery(t *testing.T) {
	text, f := ParseQuery("  kind:raw-only  verdict:keep  rating:3  seascape  ")
	if text != "seascape" {
		t.Errorf("free text = %q, want seascape", text)
	}
	if f.Kind != "raw-only" || f.Verdict != VerdictKeep || f.MinRating != 3 {
		t.Errorf("parsed facets = %+v", f)
	}

	// An unknown key is not a facet, so it stays in the text where the user can
	// see that it did nothing.
	if text, f := ParseQuery("lens:35mm"); text != "lens:35mm" || f != (Facets{}) {
		t.Errorf("unknown key parsed as %q / %+v, want it left alone", text, f)
	}
	// So is a value the facet cannot take.
	if text, _ := ParseQuery("kind:banana"); text != "kind:banana" {
		t.Errorf("unknown kind parsed as %q, want it left alone", text)
	}
	if text, f := ParseQuery(""); text != "" || f != (Facets{}) {
		t.Errorf("empty query parsed as %q / %+v", text, f)
	}
}

func TestSearchReadsFacetsOutOfTheQuery(t *testing.T) {
	s, _, _ := searchTree(t)

	res, err := s.Search("kind:jpeg-only", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := stems(res); len(got) != 1 || got[0] != "PANO0003" {
		t.Errorf("kind:jpeg-only found %v, want PANO0003", got)
	}
}

// The chips are the explicit control, so what they say wins over a token left
// behind in the query field.
func TestFacetChipsBeatQueryTokens(t *testing.T) {
	s, _, _ := searchTree(t)

	res, err := s.Search("kind:jpeg-only", Facets{Kind: "raw-only"}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("found %d frames, want the 3 raw-only ones the chip asked for", res.Total)
	}
}

func TestCountsCountEveryValueOfEachFacet(t *testing.T) {
	s, _, _ := searchTree(t)

	counts, err := s.Counts("", Facets{})
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counts.Total != 5 {
		t.Errorf("Total = %d, want 5", counts.Total)
	}
	for kind, want := range map[string]int{"paired": 1, "raw-only": 3, "jpeg-only": 1} {
		if counts.Kinds[kind] != want {
			t.Errorf("kind %s counted %d, want %d", kind, counts.Kinds[kind], want)
		}
	}
	for verdict, want := range map[string]int{VerdictKeep: 2, VerdictCut: 1, VerdictNone: 2} {
		if counts.Verdicts[verdict] != want {
			t.Errorf("verdict %s counted %d, want %d", verdict, counts.Verdicts[verdict], want)
		}
	}
	// Ratings are counted as "this many stars and up", which is what the facet
	// filters on, so a five-star frame is inside the three-star count.
	for rating, want := range map[int]int{1: 3, 2: 3, 4: 2, 5: 1} {
		if counts.Ratings[rating] != want {
			t.Errorf("rating %d+ counted %d, want %d", rating, counts.Ratings[rating], want)
		}
	}
}

// A facet does not count itself out of existence: the kind list still shows
// every kind while a kind is selected, or there would be no way back.
func TestCountsExcludeTheirOwnFacet(t *testing.T) {
	s, _, _ := searchTree(t)

	counts, err := s.Counts("", Facets{Kind: "raw-only"})
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counts.Total != 3 {
		t.Errorf("Total under the kind facet = %d, want the 3 raw-only frames", counts.Total)
	}
	if counts.Kinds["paired"] != 1 || counts.Kinds["jpeg-only"] != 1 {
		t.Errorf("the kind list narrowed itself: %+v", counts.Kinds)
	}
	// Every other facet is counted inside the selection.
	if counts.Verdicts[VerdictCut] != 1 || counts.Verdicts[VerdictNone] != 2 {
		t.Errorf("verdicts under the kind facet = %+v, want them counted within raw-only", counts.Verdicts)
	}
	if counts.Verdicts[VerdictKeep] != 0 {
		t.Errorf("keep counted %d among the raw-only frames, want 0", counts.Verdicts[VerdictKeep])
	}
}

func TestSessionsClusterByShotTime(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	// Morning and evening on the same day, five hours apart, plus a frame the
	// next day: three sessions at the four-hour default.
	writeFrame(t, root, "MORN0001", 100, 0, day(2026, 5, 1, 9, 0))
	writeFrame(t, root, "MORN0002", 100, 0, day(2026, 5, 1, 10, 30))
	writeFrame(t, root, "EVEN0001", 200, 0, day(2026, 5, 1, 15, 30))
	writeFrame(t, root, "NEXT0001", 300, 0, day(2026, 5, 2, 11, 0))
	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("%d sessions at the default gap, want 3: %+v", len(sessions), sessions)
	}
	// Newest first.
	if !sessions[0].Start.Equal(day(2026, 5, 2, 11, 0)) {
		t.Errorf("first session starts %v, want the newest", sessions[0].Start)
	}
	morning := sessions[2]
	if morning.Frames != 2 {
		t.Errorf("the morning session holds %d frames, want 2", morning.Frames)
	}
	if !morning.Start.Equal(day(2026, 5, 1, 9, 0)) || !morning.End.Equal(day(2026, 5, 1, 10, 30)) {
		t.Errorf("morning session spans %v to %v", morning.Start, morning.End)
	}
	if morning.Span() != 90*time.Minute {
		t.Errorf("morning span = %v, want 90m", morning.Span())
	}
	if morning.RawBytes != 200 {
		t.Errorf("morning raw bytes = %d, want 200", morning.RawBytes)
	}
	if morning.Source != filepath.Base(root) {
		t.Errorf("session source = %q, want the folder the frames came from", morning.Source)
	}

	// A wider gap swallows the whole first day.
	wide, err := s.Sessions(8 * time.Hour)
	if err != nil {
		t.Fatalf("Sessions(8h): %v", err)
	}
	if len(wide) != 2 {
		t.Errorf("%d sessions at an eight-hour gap, want 2", len(wide))
	}
}

func TestSessionsCountKeptAndCut(t *testing.T) {
	s, _, _ := searchTree(t)

	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	var kept, cut, undecided int
	for _, sess := range sessions {
		kept += sess.Kept
		cut += sess.Cut
		undecided += sess.Undecided
	}
	if kept != 2 || cut != 1 || undecided != 2 {
		t.Errorf("sessions total kept/cut/undecided = %d/%d/%d, want 2/1/2", kept, cut, undecided)
	}
}

func TestSessionsOnAnEmptyCatalogue(t *testing.T) {
	s := openStore(t)
	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("%d sessions in an empty catalogue", len(sessions))
	}
}

func TestStorageSummary(t *testing.T) {
	s, cards, archive := searchTree(t)

	storage, err := s.StorageSummary()
	if err != nil {
		t.Fatalf("StorageSummary: %v", err)
	}
	if storage.Frames != 5 {
		t.Errorf("summary counts %d frames, want 5", storage.Frames)
	}
	if storage.RawBytes != 10000 || storage.JpegBytes != 1800 {
		t.Errorf("summary bytes = raw %d / jpeg %d, want 10000 / 1800", storage.RawBytes, storage.JpegBytes)
	}
	if len(storage.Roots) != 2 {
		t.Fatalf("%d roots in the summary, want 2", len(storage.Roots))
	}

	byPath := map[string]RootStorage{}
	for _, r := range storage.Roots {
		byPath[r.Root] = r
	}
	if got := byPath[cards]; got.Frames != 3 || got.RawBytes != 6000 || got.JpegBytes != 1800 {
		t.Errorf("cards root = %+v, want 3 frames / 6000 raw / 1800 jpeg", got)
	}
	if got := byPath[archive]; got.Frames != 2 || got.RawBytes != 4000 {
		t.Errorf("archive root = %+v, want 2 frames / 4000 raw", got)
	}

	// Both roots are under the same temp directory, so they roll up into one
	// volume carrying every frame.
	if len(storage.Volumes) != 1 {
		t.Fatalf("%d volumes, want the 1 both roots live on: %+v", len(storage.Volumes), storage.Volumes)
	}
	vol := storage.Volumes[0]
	if vol.Frames != 5 || vol.RawBytes != 10000 || vol.JpegBytes != 1800 {
		t.Errorf("volume rollup = %+v, want every frame", vol)
	}
	if len(vol.Roots) != 2 {
		t.Errorf("volume names %d roots, want 2", len(vol.Roots))
	}
}

// A registered root that has never been indexed is still a row in the storage
// view: it is the one place the user finds out it is empty.
func TestStorageSummaryIncludesAnUnindexedRoot(t *testing.T) {
	s := openStore(t)
	if _, err := s.AddRoot("/Volumes/FUJI_SD"); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	storage, err := s.StorageSummary()
	if err != nil {
		t.Fatalf("StorageSummary: %v", err)
	}
	if len(storage.Roots) != 1 || storage.Roots[0].Frames != 0 {
		t.Errorf("summary = %+v, want the empty root listed", storage.Roots)
	}
	if len(storage.Volumes) != 1 || storage.Volumes[0].Volume != "/Volumes/FUJI_SD" {
		t.Errorf("volumes = %+v, want the card's own volume", storage.Volumes)
	}
}

package catalog

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// schemaOne is the frames table as version 1 wrote it, without the
// modification times a rerun now compares against.
const schemaOne = `
CREATE TABLE frames (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	kind       TEXT NOT NULL,
	shot       INTEGER NOT NULL,
	raw_path   TEXT NOT NULL,
	jpeg_path  TEXT NOT NULL,
	raw_bytes  INTEGER NOT NULL,
	jpeg_bytes INTEGER NOT NULL,
	rating     INTEGER NOT NULL,
	verdict    TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	indexed_at INTEGER NOT NULL
);
CREATE TABLE roots (
	path            TEXT PRIMARY KEY,
	added_at        INTEGER NOT NULL,
	last_indexed_at INTEGER NOT NULL
);
PRAGMA user_version = 1;
`

func TestOpenUpgradesACatalogueWrittenBeforeModificationTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(schemaOne); err != nil {
		t.Fatalf("write the old schema: %v", err)
	}
	if _, err := old.Exec(
		`INSERT INTO frames VALUES ('h1','/cards','DSCF0001','raw-only',0,'/cards/DSCF0001.RAF','',100,0,0,'keep',0)`,
	); err != nil {
		t.Fatalf("seed the old catalogue: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open a version 1 catalogue: %v", err)
	}
	defer s.Close()

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Frames[0].Stem != "DSCF0001" {
		t.Errorf("the upgrade lost the row: %+v", res.Frames)
	}
	if res.Frames[0].Verdict != VerdictKeep {
		t.Errorf("the upgraded row carries verdict %q, want the keep it was written with", res.Frames[0].Verdict)
	}
}

// touch moves a file's mtime without changing its size, which is what an
// editor writing the same bytes back does and what the incremental pass has to
// notice.
func touch(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// twoDayCard is three frames over two directories, indexed once, ready for a
// second pass to be measured against.
func twoDayCard(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s := openStore(t)
	root := t.TempDir()
	day1 := filepath.Join(root, "2026-05-01")
	day2 := filepath.Join(root, "2026-05-02")
	mkdir(t, day1)
	mkdir(t, day2)
	writeFrame(t, day1, "DSCF0001", 300, 0, shotAt(9, 0))
	writeFrame(t, day1, "DSCF0002", 300, 0, shotAt(9, 5))
	writeFrame(t, day2, "DSCF0100", 300, 0, shotAt(14, 0))

	stats, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	if stats.Frames != 3 || stats.Changed != 3 {
		t.Fatalf("first pass = %+v, want 3 frames all changed", stats)
	}
	return s, root, day1, day2
}

func TestReindexSkipsFilesThatHaveNotChanged(t *testing.T) {
	s, root, _, _ := twoDayCard(t)

	stats, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Frames != 3 {
		t.Errorf("second pass accounted for %d frames, want the same 3", stats.Frames)
	}
	if stats.Changed != 0 {
		t.Errorf("second pass re-read %d frames, want none — nothing on disk moved", stats.Changed)
	}
	if stats.Removed != 0 {
		t.Errorf("second pass dropped %d rows, want none", stats.Removed)
	}
}

func TestReindexRereadsOnlyTheTouchedFrameAndDropsOnlyTheDeletedOne(t *testing.T) {
	s, root, day1, day2 := twoDayCard(t)

	before, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	hashes := map[string]string{}
	for _, f := range before.Frames {
		hashes[f.Stem] = f.Hash
	}

	// One frame rewritten in place, one taken away, one left alone.
	writeFrame(t, day1, "DSCF0001", 480, 0, shotAt(9, 0))
	touch(t, filepath.Join(day1, "DSCF0001.RAF"), shotAt(11, 30))
	if err := os.Remove(filepath.Join(day2, "DSCF0100.RAF")); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Changed != 1 {
		t.Errorf("second pass re-read %d frames, want only the rewritten one", stats.Changed)
	}
	if stats.Removed != 1 {
		t.Errorf("second pass dropped %d rows, want only the deleted one", stats.Removed)
	}
	if stats.Frames != 2 {
		t.Errorf("second pass accounted for %d frames, want the 2 still there", stats.Frames)
	}

	after, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if after.Total != 2 {
		t.Fatalf("catalogue holds %d frames, want 2: %+v", after.Total, stems(after))
	}
	now := map[string]string{}
	for _, f := range after.Frames {
		now[f.Stem] = f.Hash
	}
	if _, gone := now["DSCF0100"]; gone {
		t.Error("the deleted frame is still catalogued")
	}
	if now["DSCF0002"] != hashes["DSCF0002"] {
		t.Error("the untouched frame's row was rewritten")
	}
	if now["DSCF0001"] == hashes["DSCF0001"] {
		t.Error("the rewritten frame kept its old identity — it was not re-read")
	}
}

func TestReindexRefreshesDecisionsWithoutRereadingFiles(t *testing.T) {
	s, root, _, _ := twoDayCard(t)

	stats, err := s.Index(root, IndexOptions{
		Lookup: func(string) (string, int) { return VerdictKeep, 3 },
	})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Changed != 0 {
		t.Errorf("a decision change re-read %d frames, want none — no file moved", stats.Changed)
	}

	res, err := s.Search("", Facets{Verdict: VerdictKeep}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("%d frames carry the new verdict, want all 3", res.Total)
	}
	for _, f := range res.Frames {
		if f.Rating != 3 {
			t.Errorf("%s carries %d stars, want the 3 the lookup returned", f.Stem, f.Rating)
		}
	}
}

func TestReindexRebuildsAfterTheCatalogueIsEmptied(t *testing.T) {
	s, root, _, _ := twoDayCard(t)

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var hashes []string
	for _, f := range res.Frames {
		hashes = append(hashes, f.Hash)
	}
	if err := s.RemoveByHash(hashes); err != nil {
		t.Fatalf("RemoveByHash: %v", err)
	}

	// A pass over a catalogue that has lost its rows has to put them back:
	// nothing on disk changed, but the index no longer describes it.
	stats, err := s.Index(root, IndexOptions{})
	if err != nil {
		t.Fatalf("third index: %v", err)
	}
	if stats.Changed != 3 {
		t.Errorf("the rebuild re-read %d frames, want all 3", stats.Changed)
	}
	res, err = s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("catalogue holds %d frames after the rebuild, want 3", res.Total)
	}
}

func TestUpsertDirIndexesOneDirectoryAndNotItsChildren(t *testing.T) {
	s, _, day1, _ := twoDayCard(t)
	deeper := filepath.Join(day1, "100_FUJI")
	mkdir(t, deeper)

	writeFrame(t, day1, "DSCF0003", 300, 0, shotAt(9, 10))
	writeFrame(t, deeper, "DSCF0400", 300, 0, shotAt(9, 20))

	stats, err := s.UpsertDir(day1, IndexOptions{})
	if err != nil {
		t.Fatalf("UpsertDir: %v", err)
	}
	if stats.Changed != 1 {
		t.Errorf("UpsertDir read %d frames, want the one new frame in the folder itself", stats.Changed)
	}

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := map[string]bool{}
	for _, f := range res.Frames {
		got[f.Stem] = true
	}
	if !got["DSCF0003"] {
		t.Error("the new frame in the opened folder was not catalogued")
	}
	if got["DSCF0400"] {
		t.Error("UpsertDir descended into a subdirectory")
	}
}

func TestUpsertDirDropsWhatLeftTheDirectory(t *testing.T) {
	s, _, day1, _ := twoDayCard(t)

	if err := os.Remove(filepath.Join(day1, "DSCF0002.RAF")); err != nil {
		t.Fatal(err)
	}
	stats, err := s.UpsertDir(day1, IndexOptions{})
	if err != nil {
		t.Fatalf("UpsertDir: %v", err)
	}
	if stats.Removed != 1 {
		t.Errorf("UpsertDir dropped %d rows, want the one whose file is gone", stats.Removed)
	}
}

func TestUpsertDirOutsideEveryRootDoesNothing(t *testing.T) {
	s, _, _, _ := twoDayCard(t)
	elsewhere := t.TempDir()
	writeFrame(t, elsewhere, "OTHER001", 300, 0, shotAt(9, 0))

	stats, err := s.UpsertDir(elsewhere, IndexOptions{})
	if err != nil {
		t.Fatalf("UpsertDir on an uncatalogued folder: %v", err)
	}
	if stats != (Stats{}) {
		t.Errorf("UpsertDir outside every root reported %+v, want nothing done", stats)
	}
	res, err := s.Search("OTHER", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 {
		t.Error("a folder no root covers was catalogued anyway")
	}
}

func TestRemoveByHashForgetsOnlyThoseFrames(t *testing.T) {
	s, _, _, _ := twoDayCard(t)

	res, err := s.Search("DSCF0001", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("setup found %d frames for DSCF0001, want 1", len(res.Frames))
	}
	if err := s.RemoveByHash([]string{res.Frames[0].Hash, "a hash nothing carries"}); err != nil {
		t.Fatalf("RemoveByHash: %v", err)
	}

	after, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if after.Total != 2 {
		t.Errorf("catalogue holds %d frames, want the 2 that were not removed", after.Total)
	}
	for _, f := range after.Frames {
		if f.Stem == "DSCF0001" {
			t.Error("the removed frame is still catalogued")
		}
	}
}

func TestRemoveByHashOfNothingIsNotAnError(t *testing.T) {
	s := openStore(t)
	if err := s.RemoveByHash(nil); err != nil {
		t.Errorf("RemoveByHash(nil): %v", err)
	}
}

func TestSetDecisionsOverwritesWhatTheIndexRecorded(t *testing.T) {
	s, _, _, _ := twoDayCard(t)

	res, err := s.Search("DSCF0001", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	hash := res.Frames[0].Hash
	if err := s.SetDecisions([]Decision{{Hash: hash, Verdict: VerdictCut, Rating: 5}}); err != nil {
		t.Fatalf("SetDecisions: %v", err)
	}

	cut, err := s.Search("", Facets{Verdict: VerdictCut}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if cut.Total != 1 || cut.Frames[0].Hash != hash {
		t.Fatalf("frames marked cut = %+v, want the one that was written", stems(cut))
	}
	if cut.Frames[0].Rating != 5 {
		t.Errorf("the written rating is %d, want 5", cut.Frames[0].Rating)
	}
}

func TestSetDecisionsRejectsAVerdictTheCatalogueCannotHold(t *testing.T) {
	s, _, _, _ := twoDayCard(t)
	if err := s.SetDecisions([]Decision{{Hash: "x", Verdict: "maybe"}}); err == nil {
		t.Error("a verdict outside the vocabulary was accepted")
	}
}

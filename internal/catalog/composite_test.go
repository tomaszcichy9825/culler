package catalog

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// The catalogue keys frames on (hash, dir, stem) for the same reason the
// decision store does: the same bytes in two places are two photographs. Under
// a hash-only key the second twin had no row — invisible to search, the tree
// and the sessions — and a decision written against the hash landed on
// whichever twin held the row.
func TestTwinsInTwoFoldersAreTwoRows(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	cardA := filepath.Join(root, "card-a")
	cardB := filepath.Join(root, "card-b")
	mkdir(t, cardA)
	mkdir(t, cardB)
	// Same stem, same size, same content: byte-identical twins.
	writeFrame(t, cardA, "DSCF0001", 3000, 0, shotAt(9, 0))
	writeFrame(t, cardB, "DSCF0001", 3000, 0, shotAt(9, 0))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("the catalogue holds %d rows for two byte-identical files, want 2", res.Total)
	}

	// A decision on one twin stays on that twin.
	hash := res.Frames[0].Hash
	if err := s.SetDecisions([]Decision{{Hash: hash, Dir: cardA, Stem: "DSCF0001", Verdict: VerdictCut}}); err != nil {
		t.Fatalf("SetDecisions: %v", err)
	}
	res, err = s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[string]string{}
	for _, f := range res.Frames {
		verdicts[f.Dir] = f.Verdict
	}
	if verdicts[cardA] != VerdictCut {
		t.Errorf("twin A verdict = %q, want the cut written to it", verdicts[cardA])
	}
	if verdicts[cardB] != "" {
		t.Errorf("twin B verdict = %q, want untouched", verdicts[cardB])
	}

	// Forgetting one twin's frame — an apply took its files — leaves the other.
	if err := s.RemoveFrames([]FrameKey{{Hash: hash, Dir: cardA, Stem: "DSCF0001"}}); err != nil {
		t.Fatalf("RemoveFrames: %v", err)
	}
	res, err = s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Frames[0].Dir != cardB {
		t.Errorf("forgetting twin A took twin B with it: %+v", res.Frames)
	}
}

// A catalogue written while the hash was the whole key must come through the
// migration with its rows intact and twin-independence working from then on.
func TestFramesMigrateFromTheHashKeyedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := `
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
	raw_mtime  INTEGER NOT NULL DEFAULT 0,
	jpeg_mtime INTEGER NOT NULL DEFAULT 0,
	rating     INTEGER NOT NULL,
	verdict    TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	indexed_at INTEGER NOT NULL
);
CREATE INDEX frames_dir  ON frames(dir);
CREATE INDEX frames_shot ON frames(shot);
INSERT INTO frames VALUES
	('h1', '/photos', 'DSCF0001', 'raw-only', 100, '/photos/DSCF0001.RAF', '', 10, 0, 0, 0, 0, 'keep', 1),
	('h2', '/photos', 'DSCF0002', 'raw-only', 101, '/photos/DSCF0002.RAF', '', 10, 0, 0, 0, 3, '', 1);
`
	if _, err := db.Exec(old); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open over a hash-keyed catalogue: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	res, err := s.Search("", Facets{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("migrated %d rows, want 2", res.Total)
	}
	byStem := map[string]Frame{}
	for _, f := range res.Frames {
		byStem[f.Stem] = f
	}
	if byStem["DSCF0001"].Verdict != VerdictKeep || byStem["DSCF0002"].Rating != 3 {
		t.Errorf("migration lost a judgement: %+v", byStem)
	}
}

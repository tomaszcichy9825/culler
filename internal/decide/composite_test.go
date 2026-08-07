package decide

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// The reason decisions are keyed on (hash, dir, stem) and not the hash alone:
// the same bytes in two places are two photographs as far as the user is
// concerned, and each must be able to carry its own fate. Under a hash-only
// key, cutting one twin silently cut the other — in an app that deletes
// photographs, the one mistake the store must make impossible.
func TestTwinsInDifferentFoldersHoldSeparateVerdicts(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/cards/a", "DSCF0001", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/cards/b", "DSCF0001", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}

	a, ok := mustGet(t, s, "h1", "/cards/a", "DSCF0001")
	if !ok || a.Verdict != Cut {
		t.Errorf("twin A = %q (ok=%v), want its own cut", a.Verdict, ok)
	}
	b, ok := mustGet(t, s, "h1", "/cards/b", "DSCF0001")
	if !ok || b.Verdict != Keep {
		t.Errorf("twin B = %q (ok=%v), want its own keep", b.Verdict, ok)
	}
}

func TestTwinsInTheSameFolderHoldSeparateVerdicts(t *testing.T) {
	s := openStore(t)

	// An in-camera duplicate, or a copy made beside the original: same bytes,
	// same folder, different stems.
	if err := s.SetVerdict("h1", "/photos", "DSCF0001", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/photos", "DSCF0001-copy", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}

	cut, ok := mustGet(t, s, "h1", "/photos", "DSCF0001")
	if !ok || cut.Verdict != Cut {
		t.Errorf("original = %q (ok=%v), want cut", cut.Verdict, ok)
	}
	copied, ok := mustGet(t, s, "h1", "/photos", "DSCF0001-copy")
	if !ok || copied.Verdict != Keep {
		t.Errorf("copy = %q (ok=%v), want keep", copied.Verdict, ok)
	}

}

// A database written while the hash was the whole key must come through the
// composite-key migration with every row intact and independence working from
// then on.
func TestMigrationFromTheHashKeyedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := `
CREATE TABLE decisions (
	hash        TEXT PRIMARY KEY,
	dir         TEXT NOT NULL,
	stem        TEXT NOT NULL,
	verdict     TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	mask        TEXT NOT NULL CHECK (mask IN ('rj','r','j')),
	rating      INTEGER NOT NULL CHECK (rating BETWEEN 0 AND 5),
	destination TEXT NOT NULL DEFAULT '',
	updated_at  INTEGER NOT NULL
);
CREATE INDEX decisions_dir ON decisions(dir);
INSERT INTO decisions VALUES
	('h1', '/photos', 'DSCF0001', 'keep', 'rj', 3, '', 100),
	('h2', '/photos', 'DSCF0002', 'cut',  'rj', 0, '', 101);
`
	if _, err := db.Exec(old); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open over a hash-keyed database: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	r, ok := mustGet(t, s, "h1", "/photos", "DSCF0001")
	if !ok || r.Verdict != Keep || r.Rating != 3 {
		t.Errorf("h1 = %+v (ok=%v), want the migrated keep with its rating", r, ok)
	}
	if r, ok := mustGet(t, s, "h2", "/photos", "DSCF0002"); !ok || r.Verdict != Cut {
		t.Errorf("h2 = %+v (ok=%v), want the migrated cut", r, ok)
	}

	// The whole point of the migration: a twin decided elsewhere no longer
	// steals the row.
	if err := s.SetVerdict("h1", "/cards/b", "DSCF0001", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if r, ok := mustGet(t, s, "h1", "/photos", "DSCF0001"); !ok || r.Verdict != Keep {
		t.Errorf("the original lost its keep to a twin: %+v (ok=%v)", r, ok)
	}
}

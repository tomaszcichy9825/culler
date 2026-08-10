package decide

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// The verb is the difference between "put a copy of this in the library" and
// "take this off the card", and the user says which by pressing c or m. It is
// therefore part of the routing decision, stored beside the destination, and
// not a setting read at apply time.

func TestSetDestinationRemembersTheVerb(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", VerbMove); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001")
	if !ok {
		t.Fatal("the routed frame has no record")
	}
	if r.Destination != "2026/portraits" || r.Verb != VerbMove {
		t.Errorf("routed to %+v, want a move to 2026/portraits", r)
	}
}

// A route recorded without a verb is one the apply resolves against the
// configuration, which is what every route written before this existed is.
func TestSetDestinationAcceptsNoVerb(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", VerbDefault); err != nil {
		t.Fatal(err)
	}
	if r, _ := mustGet(t, s, "h1", "/cardA", "DSCF0001"); r.Verb != VerbDefault {
		t.Errorf("verb %q, want the configured default", r.Verb)
	}
}

func TestSetDestinationRefusesAnUnknownVerb(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", Verb("delete")); err == nil {
		t.Fatal("an unknown verb was accepted")
	}
	if _, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001"); ok {
		t.Error("the refused route was written anyway")
	}
}

// Clearing the routing clears the verb with it: a frame going nowhere is
// neither being moved nor copied, and a verb left behind would attach itself
// to whatever destination is set next.
func TestClearingTheDestinationClearsTheVerb(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", VerbMove); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "", VerbDefault); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001")
	if !ok {
		t.Fatal("clearing the routing took the whole record with it")
	}
	if r.Destination != "" || r.Verb != VerbDefault {
		t.Errorf("after clearing: %+v", r)
	}
}

// Clearing the verdict already takes the destination with it, and the verb
// belongs to the destination.
func TestClearingTheVerdictClearsTheVerb(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", VerbMove); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/cardA", "DSCF0001", Undecided, ""); err != nil {
		t.Fatal(err)
	}
	if r, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001"); ok {
		t.Errorf("the emptied row survived: %+v", r)
	}
}

func TestSetDestinationBatchRemembersEachVerb(t *testing.T) {
	s := openStore(t)

	err := s.SetDestinationBatch([]DestinationItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Destination: "2026/keepers", Verb: VerbMove},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Destination: "2026/keepers", Verb: VerbCopy},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := mustGet(t, s, "h1", "/cardA", "DSCF0001"); r.Verb != VerbMove {
		t.Errorf("first frame: %+v", r)
	}
	if r, _ := mustGet(t, s, "h2", "/cardA", "DSCF0002"); r.Verb != VerbCopy {
		t.Errorf("second frame: %+v", r)
	}
}

// A batch is all or nothing, so one bad verb must not leave half a selection
// routed.
func TestSetDestinationBatchRefusesAnUnknownVerb(t *testing.T) {
	s := openStore(t)

	err := s.SetDestinationBatch([]DestinationItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Destination: "2026/keepers", Verb: VerbMove},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Destination: "2026/keepers", Verb: Verb("teleport")},
	})
	if err == nil {
		t.Fatal("a batch with an unknown verb was accepted")
	}
	if _, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001"); ok {
		t.Error("the good half of the batch landed")
	}
}

// A database written before routes carried a verb opens, keeps its routes, and
// reads them back as verbless — the configured default, which is exactly what
// those routes meant when they were written.
func TestMigrationFromAVerblessDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	verbless := `
CREATE TABLE decisions (
	hash        TEXT NOT NULL,
	dir         TEXT NOT NULL,
	stem        TEXT NOT NULL,
	verdict     TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	mask        TEXT NOT NULL CHECK (mask IN ('rj','r','j')),
	rating      INTEGER NOT NULL CHECK (rating BETWEEN 0 AND 5),
	destination TEXT NOT NULL DEFAULT '',
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (hash, dir, stem)
);`
	if _, err := db.Exec(verbless); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, destination, updated_at)
		 VALUES (?, ?, ?, 'keep', 'rj', 0, ?, 1700000000)`,
		"h1", "/cardA", "DSCF0001", "2026/portraits")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open must migrate a verbless database: %v", err)
	}
	defer s.Close()

	r, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001")
	if !ok {
		t.Fatal("the migration lost the routed frame")
	}
	if r.Destination != "2026/portraits" || r.Verdict != Keep || r.Verb != VerbDefault {
		t.Errorf("migrated to %+v", r)
	}
	// And the migrated row takes a verb like any other.
	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "2026/portraits", VerbMove); err != nil {
		t.Fatalf("write to a migrated store: %v", err)
	}
	if r, _ := mustGet(t, s, "h1", "/cardA", "DSCF0001"); r.Verb != VerbMove {
		t.Errorf("after writing a verb: %+v", r)
	}
}

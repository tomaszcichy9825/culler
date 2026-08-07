package decide

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "decisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustGet(t *testing.T, s *Store, hash, dir, stem string) (Record, bool) {
	t.Helper()
	r, ok, err := s.Get(hash, dir, stem)
	if err != nil {
		t.Fatalf("Get(%s, %s, %s): %v", hash, dir, stem, err)
	}
	return r, ok
}

// recordsIn reads every row filed under dir, keyed by stem. A test-only view:
// production reads answer to the full (hash, dir, stem) identity through Get,
// but batch and migration tests want to see a whole directory at once.
func recordsIn(t *testing.T, s *Store, dir string) map[string]Record {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT stem, verdict, mask, rating, destination FROM decisions WHERE dir = ?`, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	out := map[string]Record{}
	for rows.Next() {
		var stem string
		var r Record
		if err := rows.Scan(&stem, &r.Verdict, &r.Mask, &r.Rating, &r.Destination); err != nil {
			t.Fatal(err)
		}
		out[stem] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSetVerdictRoundTrip(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Keep, MaskJPEG); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234")
	if !ok {
		t.Fatal("record not found after SetVerdict")
	}
	if r.Verdict != Keep || r.Mask != MaskJPEG {
		t.Errorf("want keep/%q, got %q/%q", MaskJPEG, r.Verdict, r.Mask)
	}
	if r.Rating != 0 {
		t.Errorf("a verdict must not invent a rating, got %d", r.Rating)
	}
}

func TestGetUnknownHash(t *testing.T) {
	s := openStore(t)

	r, ok := mustGet(t, s, "never-seen", "/photos", "DSCF1234")
	if ok {
		t.Errorf("want ok=false for an unknown hash, got record %+v", r)
	}
	if r.Verdict != Undecided {
		t.Errorf("want an undecided record for an unknown hash, got %q", r.Verdict)
	}
}

func TestUpsertOverwrites(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	// The same frame decided again replaces its own row.
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234")
	if !ok || r.Verdict != Keep {
		t.Fatalf("want keep, got %q (ok=%v)", r.Verdict, ok)
	}

	// The same content seen at a new path is a new row, not a steal: the old
	// place keeps its own verdict. This is the composite key doing its job.
	if err := s.SetVerdict("h1", "/photos/keep", "RENAMED", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234"); !ok || r.Verdict != Keep {
		t.Errorf("the original place lost its keep to a twin: %+v (ok=%v)", r, ok)
	}
	if r, ok := mustGet(t, s, "h1", "/photos/keep", "RENAMED"); !ok || r.Verdict != Cut {
		t.Errorf("want RENAMED->cut in the new dir, got %+v (ok=%v)", r, ok)
	}
}

func TestClearingVerdictDeletesAnUnratedFrame(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Undecided, MaskBoth); err != nil {
		t.Fatal(err)
	}

	if r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234"); ok {
		t.Errorf("clearing the verdict of an unrated frame must delete the row, still got %+v", r)
	}
	// Undeciding a frame that was never decided is not an error.
	if err := s.SetVerdict("h2", "/photos", "DSCF9999", Undecided, MaskBoth); err != nil {
		t.Errorf("clearing an unknown frame: %v", err)
	}
	if _, ok := mustGet(t, s, "h2", "/photos", "DSCF9999"); ok {
		t.Error("clearing an unknown frame created a row")
	}
}

// A rating is a judgement about the photograph, not about the cull, so it
// outlives the verdict it was made alongside.
func TestRatingSurvivesAClearedVerdict(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Keep, MaskRAW); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h1", "/photos", "DSCF1234", 4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Undecided, MaskBoth); err != nil {
		t.Fatal(err)
	}

	r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234")
	if !ok {
		t.Fatal("a rated frame must keep its row when the verdict is cleared")
	}
	if r.Verdict != Undecided {
		t.Errorf("verdict = %q, want cleared", r.Verdict)
	}
	if r.Rating != 4 {
		t.Errorf("rating = %d, want 4", r.Rating)
	}
}

func TestRatingIsIndependentOfVerdict(t *testing.T) {
	s := openStore(t)

	// Rating an undecided frame is legal and creates the row on its own.
	if err := s.SetRating("h1", "/photos", "DSCF1234", 5); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234")
	if !ok {
		t.Fatal("rating an undecided frame must store it")
	}
	if r.Rating != 5 || r.Verdict != Undecided {
		t.Errorf("want rating 5 and no verdict, got %+v", r)
	}

	// A later verdict leaves the rating alone.
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if r, _ := mustGet(t, s, "h1", "/photos", "DSCF1234"); r.Rating != 5 || r.Verdict != Cut {
		t.Errorf("want rating 5 and a cut, got %+v", r)
	}
}

func TestClearingRatingDeletesAnUndecidedFrame(t *testing.T) {
	s := openStore(t)

	if err := s.SetRating("h1", "/photos", "DSCF1234", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h1", "/photos", "DSCF1234", 0); err != nil {
		t.Fatal(err)
	}
	if r, ok := mustGet(t, s, "h1", "/photos", "DSCF1234"); ok {
		t.Errorf("an undecided, unrated frame must not keep a row, got %+v", r)
	}

	// With a verdict on it the row stays, minus the rating.
	if err := s.SetVerdict("h2", "/photos", "DSCF9999", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h2", "/photos", "DSCF9999", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h2", "/photos", "DSCF9999", 0); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h2", "/photos", "DSCF9999")
	if !ok {
		t.Fatal("clearing a rating must not discard the verdict")
	}
	if r.Rating != 0 || r.Verdict != Keep {
		t.Errorf("want a keep with no rating, got %+v", r)
	}
}

func TestSetVerdictRejectsInvalidValues(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Verdict("maybe"), MaskBoth); err == nil {
		t.Error("want an error for an unknown verdict")
	}
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Keep, Mask("rjx")); err == nil {
		t.Error("want an error for an unknown mask")
	}
	if err := s.SetVerdict("h1", "/photos", "DSCF1234", Keep, Mask("")); err == nil {
		t.Error("want an error for a keep with no mask")
	}
	if _, ok := mustGet(t, s, "h1", "/photos", "DSCF1234"); ok {
		t.Error("a rejected verdict must not be stored")
	}
}

func TestSetRatingRejectsValuesOffTheScale(t *testing.T) {
	s := openStore(t)

	for _, n := range []int{-1, 6} {
		if err := s.SetRating("h1", "/photos", "DSCF1234", n); err == nil {
			t.Errorf("rating %d accepted, want an error", n)
		}
	}
	if _, ok := mustGet(t, s, "h1", "/photos", "DSCF1234"); ok {
		t.Error("a rejected rating must not be stored")
	}
}

func TestSetVerdictBatch(t *testing.T) {
	s := openStore(t)

	items := []VerdictItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Verdict: Keep, Mask: MaskJPEG},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Verdict: Keep, Mask: MaskRAW},
		{Hash: "h3", Dir: "/cardA", Stem: "DSCF0003", Verdict: Cut, Mask: MaskBoth},
	}
	if err := s.SetVerdictBatch(items); err != nil {
		t.Fatal(err)
	}

	got := recordsIn(t, s, "/cardA")
	if len(got) != 3 {
		t.Fatalf("want 3 records, got %d: %v", len(got), got)
	}
	if got["DSCF0002"].Mask != MaskRAW {
		t.Errorf("want DSCF0002 masked to the RAW, got %+v", got["DSCF0002"])
	}

	// Clearing inside a batch deletes, so unmarking in the UI reaches the store.
	if err := s.SetVerdictBatch([]VerdictItem{
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Verdict: Undecided, Mask: MaskBoth},
	}); err != nil {
		t.Fatal(err)
	}
	if r, ok := mustGet(t, s, "h2", "/cardA", "DSCF0002"); ok {
		t.Errorf("clearing in a batch must delete, still got %+v", r)
	}

	if err := s.SetVerdictBatch(nil); err != nil {
		t.Errorf("an empty batch must be a no-op, got %v", err)
	}
}

func TestSetRatingBatch(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdictBatch([]VerdictItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Verdict: Cut, Mask: MaskBoth},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRatingBatch([]RatingItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Rating: 1},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Rating: 5},
	}); err != nil {
		t.Fatal(err)
	}

	got := recordsIn(t, s, "/cardA")
	if got["DSCF0001"].Rating != 1 || got["DSCF0001"].Verdict != Cut {
		t.Errorf("rating a decided frame disturbed its verdict: %+v", got["DSCF0001"])
	}
	if got["DSCF0002"].Rating != 5 || got["DSCF0002"].Verdict != Undecided {
		t.Errorf("want a rated but undecided frame, got %+v", got["DSCF0002"])
	}

	if err := s.SetRatingBatch(nil); err != nil {
		t.Errorf("an empty batch must be a no-op, got %v", err)
	}
}

func TestSetVerdictBatchIsAtomic(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h0", "/cardA", "DSCF0000", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}

	// The bad item sits in the middle: the writes before it have already been
	// executed inside the transaction and must be rolled back with it.
	items := []VerdictItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Verdict: Keep, Mask: MaskJPEG},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Verdict: Verdict("nonsense"), Mask: MaskBoth},
		{Hash: "h3", Dir: "/cardA", Stem: "DSCF0003", Verdict: Keep, Mask: MaskBoth},
		{Hash: "h0", Dir: "/cardA", Stem: "DSCF0000", Verdict: Undecided, Mask: MaskBoth},
	}
	if err := s.SetVerdictBatch(items); err == nil {
		t.Fatal("want an error for a batch containing an invalid verdict")
	}

	got := recordsIn(t, s, "/cardA")
	if len(got) != 1 {
		t.Fatalf("batch must roll back entirely, got %v", got)
	}
	if got["DSCF0000"].Verdict != Keep {
		t.Errorf("pre-existing verdict was disturbed by the failed batch: %v", got)
	}
}

func TestSetRatingBatchIsAtomic(t *testing.T) {
	s := openStore(t)

	items := []RatingItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Rating: 2},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Rating: 9},
	}
	if err := s.SetRatingBatch(items); err == nil {
		t.Fatal("want an error for a batch containing a rating off the scale")
	}
	got := recordsIn(t, s, "/cardA")
	if len(got) != 0 {
		t.Errorf("batch must roll back entirely, got %v", got)
	}
}

func TestBatchOnClosedStoreFails(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "decisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if err := s.SetVerdictBatch([]VerdictItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Verdict: Keep, Mask: MaskBoth},
	}); err == nil {
		t.Error("want an error writing a verdict to a closed store")
	}
	if err := s.SetRatingBatch([]RatingItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Rating: 3},
	}); err == nil {
		t.Error("want an error writing a rating to a closed store")
	}
}

func TestClear(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/cardA", "DSCF0001", Keep, MaskJPEG); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h2", "/cardB", "DSCF0002", 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}

	if _, ok := mustGet(t, s, "h1", "/cardA", "DSCF0001"); ok {
		t.Error("Clear left h1 behind")
	}
	got := recordsIn(t, s, "/cardB")
	if len(got) != 0 {
		t.Errorf("Clear left %v behind", got)
	}
	// The store stays usable afterwards.
	if err := s.SetVerdict("h3", "/cardA", "DSCF0003", Keep, MaskBoth); err != nil {
		t.Fatal(err)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/cardA", "DSCF0001", Keep, MaskRAW); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating("h1", "/cardA", "DSCF0001", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	r, ok := mustGet(t, s2, "h1", "/cardA", "DSCF0001")
	if !ok || r.Verdict != Keep || r.Mask != MaskRAW || r.Rating != 2 {
		t.Fatalf("record lost across reopen: %+v (ok=%v)", r, ok)
	}
	got := recordsIn(t, s2, "/cardA")
	if got["DSCF0001"].Rating != 2 {
		t.Errorf("the record lost its rating across reopen: %v", got)
	}
}

// oldSchema is the schema this package shipped before verdicts, reproduced
// verbatim so the migration is tested against a real database rather than
// against its own idea of the old format.
const oldSchema = `
CREATE TABLE IF NOT EXISTS decisions (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	decision   TEXT NOT NULL CHECK (decision IN ('keep_all','drop_raw','drop_jpeg','drop_all')),
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_dir ON decisions(dir);
`

// writeOldDatabase creates a database at path with the pre-verdict schema and
// the given decisions, then closes it.
func writeOldDatabase(t *testing.T, path, ddl string, decisions map[string]string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	for stem, d := range decisions {
		_, err := db.Exec(
			`INSERT INTO decisions (hash, dir, stem, decision, updated_at) VALUES (?, ?, ?, ?, ?)`,
			"hash-"+stem, "/cardA", stem, d, 1700000000)
		if err != nil {
			t.Fatalf("insert %s=%s: %v", stem, d, err)
		}
	}
}

func TestMigrationFromTheOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	writeOldDatabase(t, path, oldSchema, map[string]string{
		"DSCF0001": "keep_all",
		"DSCF0002": "drop_raw",
		"DSCF0003": "drop_jpeg",
		"DSCF0004": "drop_all",
	})

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open must migrate the old schema: %v", err)
	}
	defer s.Close()

	want := map[string]Record{
		"DSCF0001": {Verdict: Keep, Mask: MaskBoth},
		"DSCF0002": {Verdict: Keep, Mask: MaskJPEG},
		"DSCF0003": {Verdict: Keep, Mask: MaskRAW},
		"DSCF0004": {Verdict: Cut, Mask: MaskBoth},
	}
	got := recordsIn(t, s, "/cardA")
	if len(got) != len(want) {
		t.Fatalf("migrated %d rows, want %d: %v", len(got), len(want), got)
	}
	for stem, w := range want {
		if got[stem] != w {
			t.Errorf("%s migrated to %+v, want %+v", stem, got[stem], w)
		}
	}

	// The migrated rows answer to their content and place, and are writable.
	if r, ok := mustGet(t, s, "hash-DSCF0002", "/cardA", "DSCF0002"); !ok || r.Verdict != Keep || r.Mask != MaskJPEG {
		t.Errorf("Get after migration: %+v (ok=%v)", r, ok)
	}
	if err := s.SetRating("hash-DSCF0002", "/cardA", "DSCF0002", 5); err != nil {
		t.Fatalf("write to a migrated store: %v", err)
	}
	if r, _ := mustGet(t, s, "hash-DSCF0002", "/cardA", "DSCF0002"); r.Rating != 5 || r.Verdict != Keep {
		t.Errorf("rating a migrated row: %+v", r)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	writeOldDatabase(t, path, oldSchema, map[string]string{"DSCF0001": "drop_raw"})

	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetRating("hash-DSCF0001", "/cardA", "DSCF0001", 3); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a migrated store: %v", err)
	}
	defer second.Close()

	r, ok := mustGet(t, second, "hash-DSCF0001", "/cardA", "DSCF0001")
	if !ok {
		t.Fatal("the second Open lost the migrated row")
	}
	if r.Verdict != Keep || r.Mask != MaskJPEG || r.Rating != 3 {
		t.Errorf("the second Open re-ran the migration: %+v", r)
	}
}

// A row the old CHECK constraint could not hold — an undecided frame, or a
// value from a hand-edited database — has no verdict to migrate to.
func TestMigrationDropsUndecidedRows(t *testing.T) {
	const uncheckedOldSchema = `
CREATE TABLE IF NOT EXISTS decisions (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	decision   TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_dir ON decisions(dir);
`
	path := filepath.Join(t.TempDir(), "decisions.db")
	writeOldDatabase(t, path, uncheckedOldSchema, map[string]string{
		"DSCF0001": "none",
		"DSCF0002": "burn_it",
		"DSCF0003": "keep_all",
	})

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got := recordsIn(t, s, "/cardA")
	if len(got) != 1 {
		t.Fatalf("want only the decided frame to survive, got %v", got)
	}
	if got["DSCF0003"].Verdict != Keep {
		t.Errorf("the decided frame did not migrate: %+v", got["DSCF0003"])
	}
}

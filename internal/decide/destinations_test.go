package decide

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

/* ---- a destination on a frame ---- */

func TestDestinationImpliesAKeep(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "~/Pictures/2026"); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1")
	if !ok {
		t.Fatal("a destination on its own must record the frame")
	}
	if r.Destination != "~/Pictures/2026" {
		t.Errorf("destination round-tripped as %q", r.Destination)
	}
	if r.Verdict != Keep {
		t.Errorf("a destination means the frame is being kept, got verdict %q", r.Verdict)
	}
	if r.Mask != MaskBoth {
		t.Errorf("an implied keep starts on both halves, got %q", r.Mask)
	}
}

func TestDestinationLeavesAnExistingVerdictAlone(t *testing.T) {
	s := openStore(t)

	if err := s.SetVerdict("h1", "/cardA", "DSCF0001", Keep, MaskRAW); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "/library/keepers"); err != nil {
		t.Fatal(err)
	}
	r, _ := mustGet(t, s, "h1")
	if r.Mask != MaskRAW {
		t.Errorf("routing a frame must not widen its mask, got %q", r.Mask)
	}

	// A cut with a destination is the user changing their mind mid-keystroke,
	// not an implied keep. The verdict they typed wins until they retype it.
	if err := s.SetVerdict("h2", "/cardA", "DSCF0002", Cut, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDestination("h2", "/cardA", "DSCF0002", "/library/keepers"); err != nil {
		t.Fatal(err)
	}
	if r, _ := mustGet(t, s, "h2"); r.Verdict != Cut {
		t.Errorf("a destination must not overturn a cut, got %q", r.Verdict)
	}
}

func TestClearingADestinationKeepsTheVerdict(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "/library/keepers"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDestination("h1", "/cardA", "DSCF0001", ""); err != nil {
		t.Fatal(err)
	}
	r, ok := mustGet(t, s, "h1")
	if !ok {
		t.Fatal("clearing the destination must not delete a frame that is still kept")
	}
	if r.Destination != "" {
		t.Errorf("destination not cleared: %q", r.Destination)
	}
	if r.Verdict != Keep {
		t.Errorf("the implied keep survives its destination, got %q", r.Verdict)
	}
}

func TestClearingTheVerdictClearsTheDestination(t *testing.T) {
	s := openStore(t)

	if err := s.SetDestination("h1", "/cardA", "DSCF0001", "/library/keepers"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict("h1", "/cardA", "DSCF0001", Undecided, MaskBoth); err != nil {
		t.Fatal(err)
	}
	if _, ok := mustGet(t, s, "h1"); ok {
		t.Error("an undecided, unrated frame with no destination left must be pruned")
	}
}

func TestSetDestinationBatch(t *testing.T) {
	s := openStore(t)

	items := []DestinationItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Destination: "/library/a"},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", Destination: "/library/b"},
		{Hash: "h3", Dir: "/cardA", Stem: "DSCF0003", Destination: "/library/a"},
	}
	if err := s.SetDestinationBatch(items); err != nil {
		t.Fatal(err)
	}
	got, err := s.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"DSCF0001": "/library/a", "DSCF0002": "/library/b", "DSCF0003": "/library/a"}
	for stem, dest := range want {
		if got[stem].Destination != dest {
			t.Errorf("%s routed to %q, want %q", stem, got[stem].Destination, dest)
		}
		if got[stem].Verdict != Keep {
			t.Errorf("%s should be kept, got %q", stem, got[stem].Verdict)
		}
	}
}

func TestSetDestinationBatchIsAtomic(t *testing.T) {
	s := openStore(t)

	err := s.SetDestinationBatch([]DestinationItem{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", Destination: "/library/a"},
		{Hash: "", Dir: "/cardA", Stem: "DSCF0002", Destination: "/library/b"},
	})
	if err == nil {
		t.Fatal("a batch with an identity-less frame must fail")
	}
	if _, ok := mustGet(t, s, "h1"); ok {
		t.Error("the good half of a failed batch landed anyway")
	}
}

/* ---- the destinations table ---- */

func TestRememberDestinationCountsUses(t *testing.T) {
	s := openStore(t)

	for i := 0; i < 3; i++ {
		if err := s.UseDestination("/library/keepers", "Keepers"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("one destination used three times is one row, got %d", len(rows))
	}
	if rows[0].UseCount != 3 {
		t.Errorf("use count %d, want 3", rows[0].UseCount)
	}
	if rows[0].Label != "Keepers" {
		t.Errorf("label %q, want Keepers", rows[0].Label)
	}
	if rows[0].LastUsedAt.IsZero() {
		t.Error("a used destination must carry when it was used")
	}
}

// touch writes a destination with an explicit last-used time, so recency can be
// asserted without sleeping.
func touch(t *testing.T, s *Store, path string, at time.Time) {
	t.Helper()
	if err := s.UseDestination(path, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE destinations SET last_used_at = ? WHERE path = ?`, at.Unix(), path); err != nil {
		t.Fatal(err)
	}
}

func paths(rows []Destination) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Path)
	}
	return out
}

func TestDestinationsListPinnedFirstThenMostRecent(t *testing.T) {
	s := openStore(t)
	base := time.Unix(1_700_000_000, 0)

	touch(t, s, "/library/old", base)
	touch(t, s, "/library/middle", base.Add(time.Hour))
	touch(t, s, "/library/new", base.Add(2*time.Hour))
	if err := s.PinDestination("/library/old", true); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/library/old", "/library/new", "/library/middle"}
	got := paths(rows)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func TestTheNineMostRecentBindToDigits(t *testing.T) {
	s := openStore(t)
	base := time.Unix(1_700_000_000, 0)

	// Eleven destinations, newest last.
	for i := 0; i < 11; i++ {
		touch(t, s, filepath.Join("/library", string(rune('a'+i))), base.Add(time.Duration(i)*time.Hour))
	}
	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 11 {
		t.Fatalf("got %d destinations, want 11", len(rows))
	}
	for i, r := range rows {
		want := i + 1
		if want > MaxSlots {
			want = 0
		}
		if r.Digit != want {
			t.Errorf("%s (position %d) bound to digit %d, want %d", r.Path, i, r.Digit, want)
		}
	}
}

func TestAManuallyBoundSlotKeepsItsDigit(t *testing.T) {
	s := openStore(t)
	base := time.Unix(1_700_000_000, 0)

	touch(t, s, "/library/oldest", base)
	touch(t, s, "/library/newest", base.Add(time.Hour))
	if err := s.BindSlot("/library/oldest", 1); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	digits := map[string]int{}
	for _, r := range rows {
		digits[r.Path] = r.Digit
	}
	if digits["/library/oldest"] != 1 {
		t.Errorf("a bound slot must survive a newer destination, got %d", digits["/library/oldest"])
	}
	if digits["/library/newest"] != 2 {
		t.Errorf("the newest destination takes the first free digit, got %d", digits["/library/newest"])
	}

	got, ok, err := s.DestinationForDigit(1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Path != "/library/oldest" {
		t.Errorf("digit 1 resolved to %+v (ok=%v)", got, ok)
	}
}

func TestBindingASlotTakesItFromWhoeverHeldIt(t *testing.T) {
	s := openStore(t)

	touch(t, s, "/library/first", time.Unix(1_700_000_000, 0))
	touch(t, s, "/library/second", time.Unix(1_700_000_100, 0))
	if err := s.BindSlot("/library/first", 4); err != nil {
		t.Fatal(err)
	}
	if err := s.BindSlot("/library/second", 4); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Path == "/library/first" && r.Slot != 0 {
			t.Errorf("a digit names one place: /library/first still claims slot %d", r.Slot)
		}
		if r.Path == "/library/second" && r.Slot != 4 {
			t.Errorf("/library/second did not take slot 4, got %d", r.Slot)
		}
	}
}

func TestBindSlotRejectsDigitsOffTheKeyboard(t *testing.T) {
	s := openStore(t)
	touch(t, s, "/library/a", time.Unix(1_700_000_000, 0))

	if err := s.BindSlot("/library/a", 10); err == nil {
		t.Error("slot 10 has no key to press")
	}
	if err := s.BindSlot("/library/a", -1); err == nil {
		t.Error("a negative slot is not a slot")
	}
	if err := s.BindSlot("/library/a", 0); err != nil {
		t.Errorf("0 releases the slot: %v", err)
	}
}

func TestDestinationForDigitWithNothingBound(t *testing.T) {
	s := openStore(t)

	got, ok, err := s.DestinationForDigit(3)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("digit 3 resolved to %+v with no destinations at all", got)
	}
}

func TestForgetDestination(t *testing.T) {
	s := openStore(t)

	touch(t, s, "/library/a", time.Unix(1_700_000_000, 0))
	touch(t, s, "/library/b", time.Unix(1_700_000_100, 0))
	if err := s.ForgetDestination("/library/a"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Path != "/library/b" {
		t.Errorf("after forgetting /library/a: %v", paths(rows))
	}
}

func TestUseDestinationRejectsAnEmptyPath(t *testing.T) {
	s := openStore(t)
	if err := s.UseDestination("  ", ""); err == nil {
		t.Error("a destination with no path is not a destination")
	}
}

func TestDestinationsSurviveAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.UseDestination("/library/keepers", "Keepers"); err != nil {
		t.Fatal(err)
	}
	if err := first.BindSlot("/library/keepers", 7); err != nil {
		t.Fatal(err)
	}
	if err := first.SetDestination("h1", "/cardA", "DSCF0001", "/library/keepers"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	rows, err := second.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Slot != 7 || rows[0].Digit != 7 {
		t.Errorf("destinations after reopen: %+v", rows)
	}
	if r, _ := mustGet(t, second, "h1"); r.Destination != "/library/keepers" {
		t.Errorf("the frame's destination after reopen: %+v", r)
	}
}

/* ---- migration ---- */

// verdictSchema is what the store wrote before frames could be routed: the
// verdict model, with no destination column and no destinations table.
const verdictSchema = `
CREATE TABLE IF NOT EXISTS decisions (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	verdict    TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	mask       TEXT NOT NULL CHECK (mask IN ('rj','r','j')),
	rating     INTEGER NOT NULL CHECK (rating BETWEEN 0 AND 5),
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_dir ON decisions(dir);
`

func TestMigrationFromTheVerdictSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(verdictSchema); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		hash, stem, verdict, mask string
		rating                    int
	}{
		{"hash-1", "DSCF0001", "keep", "rj", 5},
		{"hash-2", "DSCF0002", "cut", "rj", 0},
		{"hash-3", "DSCF0003", "keep", "r", 3},
	}
	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, updated_at) VALUES (?,?,?,?,?,?,?)`,
			r.hash, "/cardA", r.stem, r.verdict, r.mask, r.rating, 1_700_000_000)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open must migrate a verdict-era database: %v", err)
	}
	defer s.Close()

	got, err := s.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Record{
		"DSCF0001": {Verdict: Keep, Mask: MaskBoth, Rating: 5},
		"DSCF0002": {Verdict: Cut, Mask: MaskBoth},
		"DSCF0003": {Verdict: Keep, Mask: MaskRAW, Rating: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("migrated %d rows, want %d: %v", len(got), len(want), got)
	}
	for stem, w := range want {
		if got[stem] != w {
			t.Errorf("%s migrated to %+v, want %+v", stem, got[stem], w)
		}
	}

	// The new column and the new table are both usable straight away.
	if err := s.SetDestination("hash-1", "/cardA", "DSCF0001", "/library/keepers"); err != nil {
		t.Fatalf("write a destination to a migrated store: %v", err)
	}
	if r, _ := mustGet(t, s, "hash-1"); r.Destination != "/library/keepers" || r.Rating != 5 {
		t.Errorf("after routing a migrated row: %+v", r)
	}
	if err := s.UseDestination("/library/keepers", ""); err != nil {
		t.Fatalf("the destinations table is missing after migration: %v", err)
	}
}

func TestMigrationFromTheOldestSchemaLeavesNoDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	writeOldDatabase(t, path, oldSchema, map[string]string{"DSCF0001": "keep_all"})

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r, ok := mustGet(t, s, "hash-DSCF0001")
	if !ok {
		t.Fatal("the row did not survive both migrations")
	}
	if r.Verdict != Keep || r.Destination != "" {
		t.Errorf("a pre-destination decision cannot invent one: %+v", r)
	}
}

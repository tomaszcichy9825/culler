package decide

import (
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

func mustGet(t *testing.T, s *Store, hash string) (Decision, bool) {
	t.Helper()
	d, ok, err := s.Get(hash)
	if err != nil {
		t.Fatalf("Get(%s): %v", hash, err)
	}
	return d, ok
}

func TestSetGetRoundTrip(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/photos", "DSCF1234", DropRAW); err != nil {
		t.Fatal(err)
	}
	d, ok := mustGet(t, s, "h1")
	if !ok {
		t.Fatal("decision not found after Set")
	}
	if d != DropRAW {
		t.Errorf("want %q, got %q", DropRAW, d)
	}
}

func TestGetUnknownHash(t *testing.T) {
	s := openStore(t)

	d, ok := mustGet(t, s, "never-seen")
	if ok {
		t.Errorf("want ok=false for an unknown hash, got decision %q", d)
	}
	if d != None {
		t.Errorf("want %q for an unknown hash, got %q", None, d)
	}
}

func TestUpsertOverwrites(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/photos", "DSCF1234", DropRAW); err != nil {
		t.Fatal(err)
	}
	// Same frame, seen at a new path, decided differently.
	if err := s.Set("h1", "/photos/keep", "RENAMED", KeepAll); err != nil {
		t.Fatal(err)
	}

	d, ok := mustGet(t, s, "h1")
	if !ok || d != KeepAll {
		t.Fatalf("want %q, got %q (ok=%v)", KeepAll, d, ok)
	}
	// The row moved with the file, so the old directory is now empty.
	old, err := s.ForDir("/photos")
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 0 {
		t.Errorf("upsert must refresh dir/stem, old dir still holds %v", old)
	}
	fresh, err := s.ForDir("/photos/keep")
	if err != nil {
		t.Fatal(err)
	}
	if fresh["RENAMED"] != KeepAll {
		t.Errorf("want RENAMED->%q in the new dir, got %v", KeepAll, fresh)
	}
}

func TestSetNoneDeletes(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/photos", "DSCF1234", DropAll); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("h1", "/photos", "DSCF1234", None); err != nil {
		t.Fatal(err)
	}

	if d, ok := mustGet(t, s, "h1"); ok {
		t.Errorf("Set(None) must delete the row, still got %q", d)
	}
	// Undeciding a frame that was never decided is not an error.
	if err := s.Set("h2", "/photos", "DSCF9999", None); err != nil {
		t.Errorf("Set(None) on an unknown hash: %v", err)
	}
}

func TestSetRejectsInvalidDecision(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/photos", "DSCF1234", Decision("drop_everything")); err == nil {
		t.Fatal("want an error for an unknown decision value")
	}
	if _, ok := mustGet(t, s, "h1"); ok {
		t.Error("a rejected decision must not be stored")
	}
}

func TestForDirIsScopedToOneDir(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/cardA", "DSCF0001", DropRAW); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("h2", "/cardA", "DSCF0002", KeepAll); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("h3", "/cardB", "DSCF0003", DropAll); err != nil {
		t.Fatal(err)
	}

	got, err := s.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 decisions for /cardA, got %d: %v", len(got), got)
	}
	if got["DSCF0001"] != DropRAW || got["DSCF0002"] != KeepAll {
		t.Errorf("wrong decisions for /cardA: %v", got)
	}
	if _, leaked := got["DSCF0003"]; leaked {
		t.Error("ForDir leaked a decision from another directory")
	}

	empty, err := s.ForDir("/cardC")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("want no decisions for an unvisited dir, got %v", empty)
	}
}

func TestSetBatch(t *testing.T) {
	s := openStore(t)

	items := []Item{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", D: DropRAW},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", D: DropJPEG},
		{Hash: "h3", Dir: "/cardA", Stem: "DSCF0003", D: KeepAll},
	}
	if err := s.SetBatch(items); err != nil {
		t.Fatal(err)
	}

	got, err := s.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 decisions, got %d: %v", len(got), got)
	}
	if got["DSCF0002"] != DropJPEG {
		t.Errorf("want DSCF0002->%q, got %q", DropJPEG, got["DSCF0002"])
	}

	// None inside a batch deletes, so unmarking in the UI reaches the store.
	if err := s.SetBatch([]Item{{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", D: None}}); err != nil {
		t.Fatal(err)
	}
	if d, ok := mustGet(t, s, "h2"); ok {
		t.Errorf("None in a batch must delete, still got %q", d)
	}

	if err := s.SetBatch(nil); err != nil {
		t.Errorf("an empty batch must be a no-op, got %v", err)
	}
}

func TestSetBatchIsAtomic(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h0", "/cardA", "DSCF0000", KeepAll); err != nil {
		t.Fatal(err)
	}

	// The bad item sits in the middle: the writes before it have already been
	// executed inside the transaction and must be rolled back with it.
	items := []Item{
		{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", D: DropRAW},
		{Hash: "h2", Dir: "/cardA", Stem: "DSCF0002", D: Decision("nonsense")},
		{Hash: "h3", Dir: "/cardA", Stem: "DSCF0003", D: KeepAll},
		{Hash: "h0", Dir: "/cardA", Stem: "DSCF0000", D: None},
	}
	if err := s.SetBatch(items); err == nil {
		t.Fatal("want an error for a batch containing an invalid decision")
	}

	got, err := s.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("batch must roll back entirely, got %v", got)
	}
	if got["DSCF0000"] != KeepAll {
		t.Errorf("pre-existing decision was disturbed by the failed batch: %v", got)
	}
}

func TestSetBatchOnClosedStoreFails(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "decisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	err = s.SetBatch([]Item{{Hash: "h1", Dir: "/cardA", Stem: "DSCF0001", D: DropRAW}})
	if err == nil {
		t.Error("want an error writing to a closed store")
	}
}

func TestClear(t *testing.T) {
	s := openStore(t)

	if err := s.Set("h1", "/cardA", "DSCF0001", DropRAW); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("h2", "/cardB", "DSCF0002", DropAll); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}

	if _, ok := mustGet(t, s, "h1"); ok {
		t.Error("Clear left h1 behind")
	}
	got, err := s.ForDir("/cardB")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Clear left %v behind", got)
	}
	// The store stays usable afterwards.
	if err := s.Set("h3", "/cardA", "DSCF0003", KeepAll); err != nil {
		t.Fatal(err)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("h1", "/cardA", "DSCF0001", DropJPEG); err != nil {
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

	d, ok := mustGet(t, s2, "h1")
	if !ok || d != DropJPEG {
		t.Fatalf("decision lost across reopen: %q (ok=%v)", d, ok)
	}
	got, err := s2.ForDir("/cardA")
	if err != nil {
		t.Fatal(err)
	}
	if got["DSCF0001"] != DropJPEG {
		t.Errorf("ForDir lost the decision across reopen: %v", got)
	}
}

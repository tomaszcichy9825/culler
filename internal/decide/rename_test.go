package decide_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/hash"
)

// A decision is keyed on the content AND the place it was seen, so it does not
// follow a rename. That is a deliberate trade: under a content-only key the
// same bytes in two places shared one row, and cutting one twin silently cut
// the other — the one mistake this store must never make. A renamed frame
// reads as undecided, which costs the user a re-judge and deletes nothing.
func TestDecisionDoesNotFollowARename(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "DSCF1234.RAF")
	if err := os.WriteFile(old, bytes.Repeat([]byte("raw bytes"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := decide.Open(filepath.Join(t.TempDir(), "decisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	h, err := hash.Content(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict(h, dir, "DSCF1234", decide.Cut, decide.MaskBoth); err != nil {
		t.Fatal(err)
	}

	renamed := filepath.Join(dir, "2026-08-02_holiday_001.RAF")
	if err := os.Rename(old, renamed); err != nil {
		t.Fatal(err)
	}
	h2, err := hash.Content(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if h2 != h {
		t.Fatalf("hash changed on rename: %s -> %s", h, h2)
	}

	// The renamed frame is undecided — the safe direction. A cut that followed
	// the content would equally follow it onto a byte-identical twin.
	r, ok, err := s.Get(h2, dir, "2026-08-02_holiday_001")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("a decision followed the rename: %+v", r)
	}
	// The old row still answers at the old place, so nothing is half-lost.
	if r, ok, err := s.Get(h, dir, "DSCF1234"); err != nil || !ok || r.Verdict != decide.Cut {
		t.Fatalf("the original row went missing: %+v (ok=%v, err=%v)", r, ok, err)
	}
}

// The other half of the contract is unchanged: once the bytes change it is a
// different photograph, and the old decision must not be applied to it.
func TestDecisionDoesNotSurviveEdit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "DSCF1234.JPG")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := decide.Open(filepath.Join(t.TempDir(), "decisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	h, err := hash.Content(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerdict(h, dir, "DSCF1234", decide.Cut, decide.MaskBoth); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte("edited in another app"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := hash.Content(p)
	if err != nil {
		t.Fatal(err)
	}
	if h2 == h {
		t.Fatal("edited file kept its hash")
	}
	if r, ok, err := s.Get(h2, dir, "DSCF1234"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Errorf("edited file inherited the old verdict %+v", r)
	}
}

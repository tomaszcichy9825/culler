package decide_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/hash"
)

// A decision must follow the photograph, not the filename. Cards get renamed
// by import tools and by the user between marking and applying, and losing a
// decision there means deleting the wrong frame.
func TestDecisionSurvivesRename(t *testing.T) {
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
	if err := s.SetVerdict(h, dir, "DSCF1234", decide.Keep, decide.MaskJPEG); err != nil {
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
	r, ok, err := s.Get(h2)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || r.Verdict != decide.Keep || r.Mask != decide.MaskJPEG {
		t.Fatalf("verdict lost across rename: %+v (ok=%v)", r, ok)
	}
}

// The other half of the contract: once the bytes change it is a different
// photograph, and the old decision must not be applied to it.
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
	if r, ok, err := s.Get(h2); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Errorf("edited file inherited the old verdict %+v", r)
	}
}

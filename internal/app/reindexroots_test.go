package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A pass covers every root, and a root it cannot read does not take the others
// with it.
//
// This matters because a pass now runs at launch: a root on a drive that is
// not plugged in, or a share that is not mounted, would otherwise freeze every
// other root's catalogue for as long as it stayed missing — and the symptom is
// the one that is hardest to spot, a library that is simply out of date with
// nothing on screen to say so.
func TestReindexCoversEveryRootWhenOneCannotBeRead(t *testing.T) {
	s := indexService(t)

	// Named so the unreadable one is walked first whatever order the roots
	// come back in, which is what makes the failure land before the good root
	// rather than after it.
	gone := filepath.Join(t.TempDir(), "aaa-unplugged")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(t.TempDir(), "zzz-library")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	shoot(t, good, "DSCF0001", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))

	for _, dir := range []string{gone, good} {
		if _, err := s.RegisterRoot(dir); err != nil {
			t.Fatalf("RegisterRoot(%s): %v", dir, err)
		}
	}
	// The drive goes away after it was registered, which is the whole point.
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	if _, err := s.reindex(""); err == nil {
		t.Error("a pass over an unreadable root reported success")
	}

	// The good root was still walked.
	roots, err := s.Roots()
	if err != nil {
		t.Fatal(err)
	}
	var indexed bool
	for _, r := range roots {
		if r.Path == good && r.Frames == 1 {
			indexed = true
		}
	}
	if !indexed {
		t.Errorf("the readable root was never indexed: %+v", roots)
	}
}

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// twinFolders writes the same bytes under the same name into two folders —
// the same shot on two cards, or a backup copy — and returns both.
func twinFolders(t *testing.T) (a, b string) {
	t.Helper()
	a, b = t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(dir, "DSCF0001.JPG"), []byte("identical bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}

// The scenario the composite decision key exists for: cutting a frame in one
// folder must not touch its byte-identical twin in another. Under a hash-only
// key the twin shared the cut, and applying the other folder trashed a file
// the user never judged.
func TestApplyLeavesAnUndecidedTwinAlone(t *testing.T) {
	app := testApp(t)
	dirA, dirB := twinFolders(t)
	library := NewLibraryService(app)
	decisions := NewDecisionService(app)
	apply := NewApplyService(app)

	folderA, err := library.OpenFolder(dirA)
	if err != nil {
		t.Fatal(err)
	}
	frame := folderA.Groups[0]
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatal(err)
	}

	// The twin's folder shows an undecided frame, not the twin's cut.
	folderB, err := library.OpenFolder(dirB)
	if err != nil {
		t.Fatal(err)
	}
	if v := folderB.Groups[0].Verdict; v != "" {
		t.Fatalf("the twin inherited a verdict it was never given: %q", v)
	}

	// Applying everything decided in B must find nothing to do.
	batch, err := apply.Apply(dirB, nil)
	if err != nil {
		t.Fatalf("Apply(B): %v", err)
	}
	if len(batch.Actions) != 0 {
		t.Fatalf("applying the twin's folder planned %d actions: %+v", len(batch.Actions), batch.Actions)
	}
	if !exists(t, filepath.Join(dirB, "DSCF0001.JPG")) {
		t.Fatal("the unjudged twin was removed")
	}

	// Applying A carries out A's own cut.
	if _, err := apply.Apply(dirA, nil); err != nil {
		t.Fatalf("Apply(A): %v", err)
	}
	if exists(t, filepath.Join(dirA, "DSCF0001.JPG")) {
		t.Error("A's cut was not applied")
	}
	if !exists(t, filepath.Join(dirB, "DSCF0001.JPG")) {
		t.Error("applying A took B's twin with it")
	}
}

// Opposite verdicts on byte-identical twins can now coexist; under the old key
// the second write overwrote the first.
func TestTwinsCanHoldOppositeVerdicts(t *testing.T) {
	app := testApp(t)
	dirA, dirB := twinFolders(t)
	library := NewLibraryService(app)
	decisions := NewDecisionService(app)

	folderA, err := library.OpenFolder(dirA)
	if err != nil {
		t.Fatal(err)
	}
	a := folderA.Groups[0]
	if err := decisions.SetVerdict(a.Hash, a.Dir, a.Stem, "cut", "rj"); err != nil {
		t.Fatal(err)
	}

	folderB, err := library.OpenFolder(dirB)
	if err != nil {
		t.Fatal(err)
	}
	b := folderB.Groups[0]
	if err := decisions.SetVerdict(b.Hash, b.Dir, b.Stem, "keep", "rj"); err != nil {
		t.Fatal(err)
	}

	reopenedA, err := library.OpenFolder(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if v := reopenedA.Groups[0].Verdict; v != "cut" {
		t.Errorf("A's cut was overwritten by the twin's keep: %q", v)
	}
	reopenedB, err := library.OpenFolder(dirB)
	if err != nil {
		t.Fatal(err)
	}
	if v := reopenedB.Groups[0].Verdict; v != "keep" {
		t.Errorf("B's keep did not stick: %q", v)
	}
}

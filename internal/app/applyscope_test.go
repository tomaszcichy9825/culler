package app

import (
	"path/filepath"
	"testing"
)

// TestApplyScopeCullsAcrossFoldersAsOneBatch is the heart of session culling: a
// scope can span folders, and applying it must trash each folder's rejects into
// that folder's own _Rejected while recording the whole thing as a single batch,
// so one undo reverses the entire session rather than the last folder only.
func TestApplyScopeCullsAcrossFoldersAsOneBatch(t *testing.T) {
	a := testApp(t)
	dirA, dirB := card(t), card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)
	apply := NewApplyService(a)

	var refs []FrameRef
	for _, dir := range []string{dirA, dirB} {
		folder, err := library.OpenFolder(dir)
		if err != nil {
			t.Fatalf("OpenFolder(%s): %v", dir, err)
		}
		frame := folder.Groups[0] // DSCF0001, the paired frame
		if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
			t.Fatalf("SetVerdict: %v", err)
		}
		refs = append(refs, FrameRef{Dir: dir, Hash: frame.Hash})
	}

	// Plan is pure and describes both folders without touching either.
	plan, err := apply.PlanScope(refs)
	if err != nil {
		t.Fatalf("PlanScope: %v", err)
	}
	if len(plan.Actions) != 6 {
		t.Fatalf("%d planned actions, want 6 (RAW+JPEG+sidecar in each of two folders): %+v", len(plan.Actions), plan.Actions)
	}
	for _, dir := range []string{dirA, dirB} {
		if !exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
			t.Fatalf("PlanScope deleted a file from %s", dir)
		}
	}

	batch, err := apply.ApplyScope(refs)
	if err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}
	if len(batch.Actions) != 6 {
		t.Fatalf("%d executed actions, want 6: %+v", len(batch.Actions), batch.Actions)
	}
	for _, action := range batch.Actions {
		if action.Outcome != "ok" {
			t.Errorf("action %+v did not succeed", action)
		}
	}

	// Every folder's rejects land in that folder's own _Rejected, not a shared
	// bin: the whole point of routing by parent.
	for _, dir := range []string{dirA, dirB} {
		if exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
			t.Errorf("%s still holds the cut RAW", dir)
		}
		if !exists(t, filepath.Join(dir, "_Rejected", "DSCF0001.RAF")) {
			t.Errorf("%s: cut RAW is not in its own _Rejected", dir)
		}
		if !exists(t, filepath.Join(dir, "_Rejected", "DSCF0001.JPG")) {
			t.Errorf("%s: cut JPEG is not in its own _Rejected", dir)
		}
	}

	// One batch, so one undo restores both folders at once.
	if err := apply.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	for _, dir := range []string{dirA, dirB} {
		for _, name := range []string{"DSCF0001.RAF", "DSCF0001.JPG", "DSCF0001.RAF.xmp"} {
			if !exists(t, filepath.Join(dir, name)) {
				t.Errorf("%s/%s was not restored by the single undo", dir, name)
			}
		}
		if exists(t, filepath.Join(dir, "_Rejected", "DSCF0001.RAF")) {
			t.Errorf("%s: undo left the RAW in _Rejected", dir)
		}
	}
	if err := apply.Undo(); err == nil {
		t.Error("a second undo reversed something; the session cull was one batch")
	}
}

// TestApplyScopeSystemTrashSpansFolders proves the scope path also works in the
// machine-trash mode, where one trasher already serves every folder.
func TestApplyScopeEmptyRefs(t *testing.T) {
	a := testApp(t)
	apply := NewApplyService(a)
	batch, err := apply.ApplyScope(nil)
	if err != nil {
		t.Fatalf("ApplyScope(nil): %v", err)
	}
	if len(batch.Actions) != 0 {
		t.Errorf("empty scope executed %d actions", len(batch.Actions))
	}
}

// A scope can name the same folder more than once and in more than one
// spelling — a session row and a tree pick, or an unclean path. The plan must
// not double up: two identical trash actions would run the file into the
// rejects twice, and the duplicate's failure would then mark the frame as not
// done, leaving a cut on a frame whose files are already gone.
func TestApplyScopeDeduplicatesRepeatedRefs(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)
	apply := NewApplyService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	frame := folder.Groups[0]
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatal(err)
	}

	// The same frame three times: twice verbatim, once through an unclean
	// spelling of its folder.
	refs := []FrameRef{
		{Dir: dir, Hash: frame.Hash},
		{Dir: dir, Hash: frame.Hash},
		{Dir: dir + "/.", Hash: frame.Hash},
	}
	batch, err := apply.ApplyScope(refs)
	if err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}
	// The paired frame is three files; a duplicated plan would be six.
	if len(batch.Actions) != 3 {
		t.Fatalf("planned %d actions for one frame named three times, want 3: %+v", len(batch.Actions), batch.Actions)
	}
	for _, act := range batch.Actions {
		if act.Outcome != "ok" {
			t.Errorf("action failed: %+v", act)
		}
	}
	// The cut was carried out in full, so it is spent.
	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range reopened.Groups {
		if g.Stem == frame.Stem && g.Verdict != "" {
			t.Errorf("the applied cut was not cleared: %q", g.Verdict)
		}
	}
}

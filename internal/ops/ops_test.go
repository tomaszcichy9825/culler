package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

func pairedGroup(dir string) scan.PhotoGroup {
	return scan.PhotoGroup{
		Dir:  dir,
		Stem: "DSCF0001",
		Kind: scan.KindPaired,
		Raw:  &scan.FileRef{Path: filepath.Join(dir, "DSCF0001.RAF")},
		Jpeg: &scan.FileRef{Path: filepath.Join(dir, "DSCF0001.JPG")},
		Sidecars: []scan.FileRef{
			{Path: filepath.Join(dir, "DSCF0001.RAF.xmp")},
		},
	}
}

func jpegOnlyGroup(dir string) scan.PhotoGroup {
	return scan.PhotoGroup{
		Dir:  dir,
		Stem: "IMG_0002",
		Kind: scan.KindJPEGOnly,
		Jpeg: &scan.FileRef{Path: filepath.Join(dir, "IMG_0002.JPG")},
	}
}

func paths(actions []FileAction) []string {
	var out []string
	for _, a := range actions {
		out = append(out, filepath.Base(a.Src))
	}
	return out
}

// Plans are pure: none of these paths exist on disk.

func TestDropRAWPlan(t *testing.T) {
	groups := []scan.PhotoGroup{pairedGroup("/p"), jpegOnlyGroup("/p")}
	actions, err := DropRAW{}.Plan(groups)
	if err != nil {
		t.Fatal(err)
	}
	// raw + its sidecar, never the jpeg; jpeg-only group contributes nothing
	got := paths(actions)
	want := map[string]bool{"DSCF0001.RAF": true, "DSCF0001.RAF.xmp": true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Fatalf("want raw+sidecar, got %v", got)
	}
	for _, a := range actions {
		if a.Verb != VerbTrash {
			t.Errorf("decision ops trash, never remove: %v", a.Verb)
		}
	}
}

func TestDropJPEGPlanLeavesSidecars(t *testing.T) {
	actions, err := DropJPEG{}.Plan([]scan.PhotoGroup{pairedGroup("/p")})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(actions)
	if len(got) != 1 || got[0] != "DSCF0001.JPG" {
		t.Fatalf("sidecars follow the RAW, not the JPEG: %v", got)
	}
}

func TestDropBothPlan(t *testing.T) {
	actions, err := DropBoth{}.Plan([]scan.PhotoGroup{pairedGroup("/p")})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Fatalf("want raw+jpeg+sidecar, got %v", paths(actions))
	}
}

func TestKeepAllPlanIsEmpty(t *testing.T) {
	actions, err := KeepAll{}.Plan([]scan.PhotoGroup{pairedGroup("/p")})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("keep all must plan nothing, got %v", paths(actions))
	}
}

func TestOpsDescribe(t *testing.T) {
	for _, op := range []Op{DropRAW{}, DropJPEG{}, DropBoth{}, KeepAll{}} {
		if op.Describe() == "" {
			t.Errorf("%T has no description", op)
		}
	}
}

// Executor integration: real files in a temp dir.

func setupTree(t *testing.T) (dir string, files map[string]string) {
	t.Helper()
	dir = t.TempDir()
	files = map[string]string{
		"DSCF0001.RAF":     "raw1",
		"DSCF0001.JPG":     "jpg1",
		"DSCF0001.RAF.xmp": "xmp1",
		"IMG_0002.JPG":     "jpg2",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, files
}

func assertTree(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(b)
	}
	if len(got) != len(want) {
		t.Fatalf("tree mismatch:\n got %v\nwant %v", got, want)
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s: got %q want %q", name, got[name], content)
		}
	}
}

func newExecutor(t *testing.T) (*Executor, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return &Executor{Journal: j, Trasher: platform.DirTrasher{Dir: t.TempDir()}}, j
}

func TestApplyThenUndoRestoresTree(t *testing.T) {
	dir, files := setupTree(t)
	ex, j := newExecutor(t)

	group := pairedGroup(dir)
	actions, err := DropRAW{}.Plan([]scan.PhotoGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := ex.Apply("drop raw", actions)
	if err != nil {
		t.Fatal(err)
	}

	afterApply := map[string]string{
		"DSCF0001.JPG": "jpg1",
		"IMG_0002.JPG": "jpg2",
	}
	assertTree(t, dir, afterApply)

	// journal recorded the batch with ok outcomes and real destinations
	batches, err := j.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || len(batches[0].Actions) != 2 {
		t.Fatalf("journal wrong: %+v", batches)
	}
	for _, a := range batches[0].Actions {
		if a.Outcome != journal.OutcomeOK || a.Dst == "" {
			t.Errorf("action not journaled with destination: %+v", a)
		}
	}

	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	assertTree(t, dir, files) // byte-identical restore

	// undo itself is journaled
	batches, _ = j.ReadAll()
	if len(batches) != 2 || batches[1].UndoOf != batch.ID {
		t.Fatalf("undo not journaled: %+v", batches)
	}
}

func TestApplyRecordsPartialFailureAndUndoStillWorks(t *testing.T) {
	dir, _ := setupTree(t)
	ex, j := newExecutor(t)

	missing := filepath.Join(dir, "GONE.RAF")
	actions := []FileAction{
		{Verb: VerbTrash, Src: filepath.Join(dir, "DSCF0001.RAF")},
		{Verb: VerbTrash, Src: missing}, // vanished before apply
	}
	batch, err := ex.Apply("drop raw", actions)
	if err != nil {
		t.Fatal(err) // partial failure is not an Apply error; it's in the record
	}

	batches, _ := j.ReadAll()
	acts := batches[0].Actions
	if acts[0].Outcome != journal.OutcomeOK {
		t.Errorf("first action should succeed: %+v", acts[0])
	}
	if acts[1].Outcome != journal.OutcomeError || acts[1].Err == "" {
		t.Errorf("missing file must be recorded as error: %+v", acts[1])
	}

	// undo restores what succeeded, skips what failed
	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "DSCF0001.RAF")); err != nil {
		t.Errorf("undo did not restore the trashed file: %v", err)
	}
}

func TestApplyMove(t *testing.T) {
	dir, _ := setupTree(t)
	dest := t.TempDir()
	ex, _ := newExecutor(t)

	src := filepath.Join(dir, "IMG_0002.JPG")
	actions := []FileAction{{Verb: VerbMove, Src: src, Dst: filepath.Join(dest, "IMG_0002.JPG")}}
	batch, err := ex.Apply("move", actions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still present after move")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "IMG_0002.JPG")); string(got) != "jpg2" {
		t.Error("moved content wrong")
	}

	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(src); string(got) != "jpg2" {
		t.Error("undo did not move the file back")
	}
}

func TestMoveCollisionGetsSuffix(t *testing.T) {
	dir, _ := setupTree(t)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "IMG_0002.JPG"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex, j := newExecutor(t)

	actions := []FileAction{{
		Verb: VerbMove,
		Src:  filepath.Join(dir, "IMG_0002.JPG"),
		Dst:  filepath.Join(dest, "IMG_0002.JPG"),
	}}
	if _, err := ex.Apply("move", actions); err != nil {
		t.Fatal(err)
	}
	// original untouched, incoming renamed
	if got, _ := os.ReadFile(filepath.Join(dest, "IMG_0002.JPG")); string(got) != "existing" {
		t.Fatal("silently overwrote existing file")
	}
	batches, _ := j.ReadAll()
	realDst := batches[0].Actions[0].Dst
	if realDst == filepath.Join(dest, "IMG_0002.JPG") {
		t.Fatal("journal must record the suffixed destination")
	}
	if got, _ := os.ReadFile(realDst); string(got) != "jpg2" {
		t.Error("suffixed copy has wrong content")
	}
}

func TestApplyCopyAndUndo(t *testing.T) {
	dir, _ := setupTree(t)
	dest := t.TempDir()
	ex, _ := newExecutor(t)

	src := filepath.Join(dir, "IMG_0002.JPG")
	actions := []FileAction{{Verb: VerbCopy, Src: src, Dst: filepath.Join(dest, "IMG_0002.JPG")}}
	batch, err := ex.Apply("copy", actions)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(src); string(got) != "jpg2" {
		t.Error("copy must leave source intact")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "IMG_0002.JPG")); string(got) != "jpg2" {
		t.Error("copy content wrong")
	}

	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "IMG_0002.JPG")); !os.IsNotExist(err) {
		t.Error("undo of copy must remove the copied file")
	}
	if got, _ := os.ReadFile(src); string(got) != "jpg2" {
		t.Error("undo of copy must not touch the source")
	}
}

func TestUndoRefusesToOverwriteNewFile(t *testing.T) {
	dir, _ := setupTree(t)
	ex, j := newExecutor(t)

	src := filepath.Join(dir, "IMG_0002.JPG")
	batch, err := ex.Apply("drop", []FileAction{{Verb: VerbTrash, Src: src}})
	if err != nil {
		t.Fatal(err)
	}
	// a new file appears at the original path before undo
	if err := os.WriteFile(src, []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(src); string(got) != "newer" {
		t.Fatal("undo silently overwrote a newer file")
	}
	batches, _ := j.ReadAll()
	last := batches[len(batches)-1]
	if last.Actions[0].Outcome != journal.OutcomeError {
		t.Fatalf("occupied destination must be recorded as an error: %+v", last.Actions[0])
	}
}

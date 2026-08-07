package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// putRejects writes files into dir's _Rejected subfolder and returns its path.
func putRejects(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	rejected := filepath.Join(dir, "_Rejected")
	if err := os.MkdirAll(rejected, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		path := filepath.Join(rejected, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return rejected
}

func TestSurveyCountsRejectsByClass(t *testing.T) {
	a := testApp(t)
	dir := card(t) // frames outside the rejected folder, which must not be counted
	putRejects(t, dir, map[string]string{
		"DSCF0100.RAF":     "raw",       // pair with the JPEG below
		"DSCF0100.JPG":     "jpeg",      // 4 bytes
		"DSCF0100.RAF.xmp": "<xmp/>",    // sidecar
		"DSCF0101.RAF":     "lone raw",  // RAW only
		"DSCF0102.JPG":     "lone jpeg", // JPEG only
		"notes.txt":        "not a frame, but it is going to be deleted all the same",
	})

	got, err := NewRejectsService(a).Survey([]string{dir})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if got.Raw != 2 {
		t.Errorf("Raw = %d, want 2", got.Raw)
	}
	if got.Jpeg != 2 {
		t.Errorf("Jpeg = %d, want 2", got.Jpeg)
	}
	if got.Pairs != 1 {
		t.Errorf("Pairs = %d, want 1 — only DSCF0100 has both halves", got.Pairs)
	}
	if got.Sidecars != 1 {
		t.Errorf("Sidecars = %d, want 1", got.Sidecars)
	}
	if got.Other != 1 {
		t.Errorf("Other = %d, want 1 — notes.txt is destroyed too, so it is counted", got.Other)
	}
	if got.Files != 6 {
		t.Errorf("Files = %d, want 6", got.Files)
	}

	// The byte total is every file in the folder, unrecognised ones included:
	// it is the size of what the command destroys, not of what it recognises.
	var want int64
	for _, body := range []string{"raw", "jpeg", "<xmp/>", "lone raw", "lone jpeg",
		"not a frame, but it is going to be deleted all the same"} {
		want += int64(len(body))
	}
	if got.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d", got.TotalBytes, want)
	}

	if len(got.Dirs) != 1 {
		t.Fatalf("%d folders in the breakdown, want 1: %+v", len(got.Dirs), got.Dirs)
	}
	per := got.Dirs[0]
	if per.Dir != dir {
		t.Errorf("breakdown names %q, want the culled folder %q", per.Dir, dir)
	}
	if per.Path != filepath.Join(dir, "_Rejected") {
		t.Errorf("breakdown path = %q, want the rejected folder", per.Path)
	}
	if per.Files != 6 || per.Bytes != want {
		t.Errorf("per-folder counts = %d files / %d bytes, want %d / %d", per.Files, per.Bytes, 6, want)
	}
}

// A folder with no rejects contributes nothing and is not an error: the
// command is offered over every open folder, most of which have never been
// culled.
func TestSurveySkipsFoldersWithoutRejects(t *testing.T) {
	a := testApp(t)
	dir := card(t)

	got, err := NewRejectsService(a).Survey([]string{dir, filepath.Join(dir, "nowhere")})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if got.Files != 0 || len(got.Dirs) != 0 {
		t.Errorf("survey found %d files in %d folders, want nothing: %+v", got.Files, len(got.Dirs), got.Dirs)
	}
	if got.Folder != "_Rejected" {
		t.Errorf("Folder = %q, want the configured rejected folder name", got.Folder)
	}
}

func TestSurveyAggregatesFoldersAndIgnoresRepeats(t *testing.T) {
	a := testApp(t)
	first := t.TempDir()
	second := t.TempDir()
	putRejects(t, first, map[string]string{"A.RAF": "raw"})
	putRejects(t, second, map[string]string{"B.JPG": "jpeg", "C.JPG": "jpeg"})

	got, err := NewRejectsService(a).Survey([]string{first, second, first + string(filepath.Separator)})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if len(got.Dirs) != 2 {
		t.Fatalf("%d folders, want 2 — the repeat of the first must not double-count: %+v", len(got.Dirs), got.Dirs)
	}
	if got.Files != 3 || got.Raw != 1 || got.Jpeg != 2 {
		t.Errorf("totals = %d files / %d raw / %d jpeg, want 3 / 1 / 2", got.Files, got.Raw, got.Jpeg)
	}
}

// The scan config's extension lists decide the classes, so a user who has
// taught the app a new RAW extension sees it counted as a RAW here too.
func TestSurveyUsesTheConfiguredExtensions(t *testing.T) {
	a := testApp(t)
	cfg := a.Config()
	cfg.RawExts = append(cfg.RawExts, ".fake")
	if err := a.setConfig(cfg); err != nil {
		t.Fatalf("setConfig: %v", err)
	}
	dir := t.TempDir()
	putRejects(t, dir, map[string]string{"A.fake": "raw", "A.JPG": "jpeg"})

	got, err := NewRejectsService(a).Survey([]string{dir})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if got.Raw != 1 || got.Pairs != 1 {
		t.Errorf("Raw = %d, Pairs = %d, want 1 and 1 for the configured extension", got.Raw, got.Pairs)
	}
}

func TestEmptyDestroysOnlyInsideTheRejectedFolder(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	rejected := putRejects(t, dir, map[string]string{
		"DSCF0100.RAF":    "raw",
		"DSCF0100.JPG":    "jpeg",
		"nested/deep.JPG": "kept in a subfolder of the rejects, and just as gone",
	})
	keeper := filepath.Join(dir, "DSCF0001.RAF")

	res, err := NewRejectsService(a).Empty([]string{dir})
	if err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if res.Deleted != 3 || res.Failed != 0 {
		t.Errorf("deleted %d, failed %d, want 3 and 0", res.Deleted, res.Failed)
	}
	if exists(t, rejected) {
		t.Errorf("%s survived; the emptied folder is removed with its contents", rejected)
	}
	if !exists(t, keeper) {
		t.Fatalf("%s was destroyed; nothing outside the rejected folder may be touched", keeper)
	}
	for _, name := range []string{"DSCF0001.JPG", "DSCF0001.RAF.xmp", "DSCF0002.JPG"} {
		if !exists(t, filepath.Join(dir, name)) {
			t.Errorf("%s was destroyed; it lives outside the rejected folder", name)
		}
	}
	if !exists(t, dir) {
		t.Error("the culled folder itself was removed")
	}
}

func TestEmptyJournalsEveryFileAsADestroy(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	putRejects(t, dir, map[string]string{"A.RAF": "raw", "B.JPG": "jpeg"})

	res, err := NewRejectsService(a).Empty([]string{dir})
	if err != nil {
		t.Fatalf("Empty: %v", err)
	}

	jrnl, err := a.openJournal()
	if err != nil {
		t.Fatal(err)
	}
	batches, err := jrnl.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("%d batches in the journal, want 1", len(batches))
	}
	b := batches[0]
	if b.ID != res.BatchID {
		t.Errorf("result batch %q is not the journalled one %q", res.BatchID, b.ID)
	}
	if !strings.Contains(b.Description, "EMPTY REJECTS") || !strings.Contains(b.Description, "unrecoverable") {
		t.Errorf("description = %q, want it to say the batch cannot be taken back", b.Description)
	}
	if len(b.Actions) != 2 {
		t.Fatalf("%d actions, want one per file", len(b.Actions))
	}
	for _, act := range b.Actions {
		if act.Verb != string(ops.VerbDestroy) {
			t.Errorf("verb %q, want %q", act.Verb, ops.VerbDestroy)
		}
		if act.Dst != "" {
			t.Errorf("action for %s recorded a destination %q; the file went nowhere", act.Src, act.Dst)
		}
		if act.Outcome != journal.OutcomeOK {
			t.Errorf("action for %s = %q (%s), want ok", act.Src, act.Outcome, act.Err)
		}
		if filepath.Dir(act.Src) != filepath.Join(dir, "_Rejected") {
			t.Errorf("action names %q, which is not inside the rejected folder", act.Src)
		}
	}
}

// Emptying nothing writes nothing: the journal is a record of work done, and a
// batch with no actions in it would only make undo harder to read.
func TestEmptyWithNothingToDestroyJournalsNothing(t *testing.T) {
	a := testApp(t)
	res, err := NewRejectsService(a).Empty([]string{card(t)})
	if err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if res.Deleted != 0 || res.BatchID != "" {
		t.Errorf("result = %+v, want no work and no batch", res)
	}
	jrnl, err := a.openJournal()
	if err != nil {
		t.Fatal(err)
	}
	batches, err := jrnl.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 0 {
		t.Errorf("%d batches journalled for an empty run", len(batches))
	}
}

// mod+z after an empty-rejects reverses the last batch that can be reversed,
// which is the one before it. The destroyed files are not offered back.
func TestUndoAfterEmptyRejectsSkipsToThePreviousBatch(t *testing.T) {
	a := testApp(t)
	culled := card(t)
	other := t.TempDir()
	putRejects(t, other, map[string]string{"gone.RAF": "raw"})

	// A real batch first: one frame trashed into the culled folder's own
	// rejects, which the empty below does not touch.
	jrnl, err := a.openJournal()
	if err != nil {
		t.Fatal(err)
	}
	trashed := filepath.Join(culled, "DSCF0002.JPG")
	executor := &ops.Executor{
		Journal: jrnl,
		Trasher: platform.DirTrasher{Dir: filepath.Join(culled, "_Rejected")},
	}
	if _, err := executor.Apply("Drop both (1 frame)", []ops.FileAction{{Verb: ops.VerbTrash, Src: trashed}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if exists(t, trashed) {
		t.Fatal("the trashed frame is still in place")
	}

	if _, err := NewRejectsService(a).Empty([]string{other}); err != nil {
		t.Fatalf("Empty: %v", err)
	}

	if err := NewApplyService(a, nil).Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !exists(t, trashed) {
		t.Error("undo did not restore the trashed frame; it stopped at the empty-rejects batch")
	}
	if exists(t, filepath.Join(other, "_Rejected", "gone.RAF")) {
		t.Error("undo brought back a permanently deleted file, which is not possible")
	}
}

func TestPickUndoTargetSkipsBatchesThatDestroyed(t *testing.T) {
	batches := []journal.Batch{
		{ID: "a", Description: "Drop both (3 frames)"},
		{ID: "b", Description: emptyRejectsDescription, Actions: []journal.Action{
			{Verb: string(ops.VerbDestroy), Src: "/card/_Rejected/x.RAF", Outcome: journal.OutcomeOK},
		}},
	}
	got, ok := pickUndoTarget(batches)
	if !ok {
		t.Fatal("no undo target, want the batch before the destruction")
	}
	if got.ID != "a" {
		t.Errorf("undo target %q, want %q: a batch that destroyed files can never be undone", got.ID, "a")
	}

	only := []journal.Batch{batches[1]}
	if _, ok := pickUndoTarget(only); ok {
		t.Error("an empty-rejects batch was offered as an undo target")
	}
}

// Belt and braces: even handed the batch directly, the executor refuses.
func TestExecutorRefusesToUndoADestroy(t *testing.T) {
	a := testApp(t)
	jrnl, err := a.openJournal()
	if err != nil {
		t.Fatal(err)
	}
	batch := journal.Batch{ID: "b", Description: emptyRejectsDescription, Actions: []journal.Action{
		{Verb: string(ops.VerbDestroy), Src: "/card/_Rejected/x.RAF", Outcome: journal.OutcomeOK},
	}}
	_, err = (&ops.Executor{Journal: jrnl}).Undo(batch)
	if err == nil {
		t.Fatal("undo of a destroy batch succeeded")
	}
	if !strings.Contains(err.Error(), "permanently") {
		t.Errorf("error %q does not say why the batch cannot come back", err)
	}
	batches, err := jrnl.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 0 {
		t.Errorf("the refused undo wrote %d batches to the journal", len(batches))
	}
}

// The folder name comes from a hand-editable config file. A name that could
// reach outside the culled folder is refused before anything is walked, let
// alone deleted.
func TestRejectedFolderNameIsRefusedWhenItCouldEscape(t *testing.T) {
	for _, name := range []string{"..", ".", "../.." + string(filepath.Separator), "a/b", "/absolute"} {
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			cfg := a.Config()
			cfg.Behaviour.RejectedFolderName = name
			// Validate() would reject some of these, so the field is set behind
			// it: the point is that the service does not trust what it reads.
			a.cfg = cfg

			svc := NewRejectsService(a)
			if _, err := svc.Survey([]string{t.TempDir()}); err == nil {
				t.Errorf("Survey accepted a rejected folder named %q", name)
			}
			if _, err := svc.Empty([]string{t.TempDir()}); err == nil {
				t.Errorf("Empty accepted a rejected folder named %q", name)
			}
		})
	}
}

// A rejected folder that is really a symlink is left alone entirely: following
// it would delete files in a folder the user never pointed at.
func TestEmptyIgnoresASymlinkedRejectedFolder(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "elsewhere.RAF"), []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "_Rejected")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	survey, err := NewRejectsService(a).Survey([]string{dir})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if survey.Files != 0 {
		t.Errorf("survey counted %d files through a symlink", survey.Files)
	}
	if _, err := NewRejectsService(a).Empty([]string{dir}); err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if !exists(t, filepath.Join(real, "elsewhere.RAF")) {
		t.Fatal("a file outside the culled folder was destroyed through a symlink")
	}
}

// The rejected folder is named by the configuration even when the app is in
// system-trash mode, where the folder is a leftover from a previous setting.
func TestSurveyUsesTheDefaultNameWhenTheConfigLeavesItBlank(t *testing.T) {
	a := testApp(t)
	cfg := a.Config()
	cfg.Behaviour.TrashMode = config.TrashSystem
	cfg.Behaviour.RejectedFolderName = ""
	a.cfg = cfg

	dir := t.TempDir()
	putRejects(t, dir, map[string]string{"A.RAF": "raw"})

	got, err := NewRejectsService(a).Survey([]string{dir})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if got.Folder != config.Default().Behaviour.RejectedFolderName || got.Files != 1 {
		t.Errorf("Folder = %q with %d files, want the stock name and the one file", got.Folder, got.Files)
	}
}

// A file that cannot be removed is a journalled failure, not a panic, and the
// folder stays because it is not empty.
func TestEmptyRecordsAFileItCouldNotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes files out of read-only folders")
	}
	a := testApp(t)
	dir := t.TempDir()
	rejected := putRejects(t, dir, map[string]string{"locked/A.RAF": "raw", "B.JPG": "jpeg"})
	locked := filepath.Join(rejected, "locked")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	res, err := NewRejectsService(a).Empty([]string{dir})
	if err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if res.Deleted != 1 || res.Failed != 1 {
		t.Errorf("deleted %d, failed %d, want 1 and 1", res.Deleted, res.Failed)
	}
	if len(res.Errors) != 1 {
		t.Errorf("%d errors reported, want the one failure: %v", len(res.Errors), res.Errors)
	}
	if !exists(t, rejected) {
		t.Error("the rejected folder was removed while it still holds a file")
	}
}

// Emptying is the one unrecoverable command, so two runs must never overlap:
// the second would survey files the first is mid-way through destroying. Like
// Import and Reindex, one runs at a time and the second is told so.
func TestEmptyRefusesToRunTwiceAtOnce(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	putRejects(t, dir, map[string]string{"A.RAF": "raw", "B.JPG": "jpeg", "C.RAF": "raw"})

	svc := NewRejectsService(a)
	var inner error
	tried := false
	svc.onProgress = func(RejectsProgress) {
		if !tried {
			tried = true
			_, inner = svc.Empty([]string{dir})
		}
	}
	if _, err := svc.Empty([]string{dir}); err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if !tried {
		t.Fatal("the progress hook never fired, so the guard was never exercised")
	}
	if inner == nil {
		t.Fatal("a second empty ran while the first was destroying files")
	}
}

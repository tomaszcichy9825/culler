package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
)

// testApp returns an App whose config and data directories are temporary, in
// rejected-folder mode so a test never reaches the machine's real trash.
func testApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Behaviour.TrashMode = config.TrashRejectedFolder
	cfg.Behaviour.RejectedFolderName = "_Rejected"

	a := newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), cfg)
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	return a
}

// card writes a small RAW+JPEG pair with a sidecar plus a JPEG-only frame,
// and returns the directory.
func card(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"DSCF0001.RAF":     "raw bytes",
		"DSCF0001.JPG":     "jpeg bytes",
		"DSCF0001.RAF.xmp": "<xmp/>",
		"DSCF0002.JPG":     "another frame",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

func TestOpenFolderReportsFrames(t *testing.T) {
	a := testApp(t)
	dir := card(t)

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	if folder.Dir != dir {
		t.Errorf("Dir = %q, want %q", folder.Dir, dir)
	}
	if len(folder.Groups) != 2 {
		t.Fatalf("%d frames, want 2: %+v", len(folder.Groups), folder.Groups)
	}

	paired := folder.Groups[0]
	if paired.Stem != "DSCF0001" || paired.Kind != "paired" {
		t.Errorf("first frame = %s/%s, want DSCF0001/paired", paired.Stem, paired.Kind)
	}
	if paired.Sidecars != 1 {
		t.Errorf("sidecars = %d, want 1", paired.Sidecars)
	}
	if paired.Hash == "" {
		t.Error("frame has no identity hash")
	}
	if paired.Decision != "none" {
		t.Errorf("decision = %q, want none on a fresh folder", paired.Decision)
	}
	if folder.Groups[1].Kind != "jpeg-only" {
		t.Errorf("second frame kind = %q, want jpeg-only", folder.Groups[1].Kind)
	}
}

func TestOpenFolderRejectsNonDirectories(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	if _, err := NewLibraryService(a).OpenFolder(filepath.Join(dir, "DSCF0001.JPG")); err == nil {
		t.Error("opened a file as a folder")
	}
	if _, err := NewLibraryService(a).OpenFolder(filepath.Join(dir, "nope")); err == nil {
		t.Error("opened a folder that does not exist")
	}
}

func TestApplyThenUndoRestoresTheFolder(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)
	apply := NewApplyService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := decisions.Set(frame.Hash, frame.Dir, frame.Stem, "drop_raw"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Plan is pure: it says what would happen and touches nothing.
	plan, err := apply.Plan(dir, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("%d planned actions, want the RAW and its sidecar: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Counts["drop_raw"] != 1 {
		t.Errorf("counts = %v, want one drop_raw frame", plan.Counts)
	}
	if plan.TotalBytes == 0 {
		t.Error("plan totals no bytes")
	}
	if !exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
		t.Fatal("Plan deleted a file")
	}

	batch, err := apply.Apply(dir, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(batch.Actions) != 2 {
		t.Fatalf("%d executed actions, want 2: %+v", len(batch.Actions), batch.Actions)
	}
	for _, a := range batch.Actions {
		if a.Outcome != "ok" {
			t.Errorf("action %+v did not succeed", a)
		}
	}
	if exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
		t.Error("RAW is still on the card after a drop_raw apply")
	}
	if exists(t, filepath.Join(dir, "DSCF0001.RAF.xmp")) {
		t.Error("sidecar did not follow the RAW")
	}
	if !exists(t, filepath.Join(dir, "DSCF0001.JPG")) {
		t.Error("drop_raw removed the JPEG")
	}
	if !exists(t, filepath.Join(dir, "_Rejected", "DSCF0001.RAF")) {
		t.Error("RAW is not in the rejected folder; it must always be recoverable")
	}

	// The decision is spent: reopening the folder shows an undecided frame.
	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder after apply: %v", err)
	}
	if got := reopened.Groups[0].Decision; got != "none" {
		t.Errorf("decision after apply = %q, want none", got)
	}
	if got := reopened.Groups[0].Kind; got != "jpeg-only" {
		t.Errorf("frame kind after dropping the RAW = %q, want jpeg-only", got)
	}

	if err := apply.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	for _, name := range []string{"DSCF0001.RAF", "DSCF0001.RAF.xmp", "DSCF0001.JPG", "DSCF0002.JPG"} {
		if !exists(t, filepath.Join(dir, name)) {
			t.Errorf("%s was not restored by undo", name)
		}
	}
	if exists(t, filepath.Join(dir, "_Rejected", "DSCF0001.RAF")) {
		t.Error("undo left the RAW in the rejected folder as well")
	}

	if err := apply.Undo(); err == nil {
		t.Error("second undo reversed something; there is nothing left to undo")
	}
}

func TestApplyOnlyTouchesRequestedFrames(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	apply := NewApplyService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	items := make([]DecisionItem, 0, len(folder.Groups))
	for _, g := range folder.Groups {
		items = append(items, DecisionItem{Hash: g.Hash, Dir: g.Dir, Stem: g.Stem, Decision: "drop_all"})
	}
	if err := NewDecisionService(a).SetBatch(items); err != nil {
		t.Fatalf("SetBatch: %v", err)
	}

	// Only the second frame is named, so the first must survive untouched.
	if _, err := apply.Apply(dir, []string{folder.Groups[1].Hash}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if exists(t, filepath.Join(dir, "DSCF0002.JPG")) {
		t.Error("the requested frame was not applied")
	}
	if !exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
		t.Error("a frame that was not requested was applied anyway")
	}

	// Its decision is still pending, so a later apply can carry it out.
	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	if got := reopened.Groups[0].Decision; got != "drop_all" {
		t.Errorf("untouched frame's decision = %q, want drop_all", got)
	}
}

func TestConfigServiceRoundTrip(t *testing.T) {
	a := testApp(t)
	settings := NewConfigService(a)

	if settings.Path() != a.configPath {
		t.Errorf("Path = %q, want %q", settings.Path(), a.configPath)
	}

	cfg, err := settings.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	cfg.Behaviour.BulkConfirmThreshold = 50
	if err := settings.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	saved, err := config.Load(a.configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Behaviour.BulkConfirmThreshold != 50 {
		t.Errorf("threshold on disk = %d, want 50", saved.Behaviour.BulkConfirmThreshold)
	}
	if got, _ := settings.Get(); got.Behaviour.BulkConfirmThreshold != 50 {
		t.Error("the running app kept the old settings")
	}
}

func TestConfigServiceRejectsInvalidConfig(t *testing.T) {
	a := testApp(t)
	settings := NewConfigService(a)

	cfg, err := settings.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	cfg.Behaviour.CollisionPolicy = "clobber"
	if err := settings.Save(cfg); err == nil {
		t.Fatal("saved a config with an unknown collision policy")
	}
	if exists(t, a.configPath) {
		t.Error("the rejected config was written to disk")
	}
	if got, _ := settings.Get(); got.Behaviour.CollisionPolicy == "clobber" {
		t.Error("the rejected config was adopted by the running app")
	}
}

func TestDecisionServiceRejectsUnknownDecision(t *testing.T) {
	a := testApp(t)
	if err := NewDecisionService(a).Set("hash", "/card", "DSCF0001", "burn"); err == nil {
		t.Error("unknown decision accepted")
	}
}

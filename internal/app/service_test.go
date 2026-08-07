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

// unwritableDir makes dir read-only and skips the test where that cannot be
// arranged — Windows ignores directory modes, and root walks through them.
func unwritableDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	probe := filepath.Join(dir, ".probe")
	if err := os.WriteFile(probe, nil, 0o644); err == nil {
		os.Remove(probe)
		t.Skip("cannot make the directory read-only on this platform")
	}
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
	if paired.Verdict != "" || paired.Rating != 0 {
		t.Errorf("verdict/rating = %q/%d, want an undecided frame in a fresh folder", paired.Verdict, paired.Rating)
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
	apply := NewApplyService(a, nil)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "keep", "j"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
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
	if got := reopened.Groups[0].Verdict; got != "" {
		t.Errorf("verdict after apply = %q, want cleared", got)
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
	apply := NewApplyService(a, nil)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	items := make([]VerdictItem, 0, len(folder.Groups))
	for _, g := range folder.Groups {
		items = append(items, VerdictItem{Hash: g.Hash, Dir: g.Dir, Stem: g.Stem, Verdict: "cut", Mask: "rj"})
	}
	if err := NewDecisionService(a).SetVerdictBatch(items); err != nil {
		t.Fatalf("SetVerdictBatch: %v", err)
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
	if got := reopened.Groups[0].Verdict; got != "cut" {
		t.Errorf("untouched frame's verdict = %q, want cut", got)
	}
}

// A rating outlives the cull it was made during: applying spends the verdict
// and leaves the stars on the frame that survived.
func TestApplyClearsTheVerdictAndKeepsTheRating(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "keep", "j"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if err := decisions.SetRating(frame.Hash, frame.Dir, frame.Stem, 5); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if _, err := NewApplyService(a, nil).Apply(dir, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder after apply: %v", err)
	}
	got := reopened.Groups[0]
	if got.Verdict != "" {
		t.Errorf("verdict after apply = %q, want cleared", got.Verdict)
	}
	if got.Rating != 5 {
		t.Errorf("rating after apply = %d, want the 5 stars to survive", got.Rating)
	}
}

// A move the collision policy skips never took the photo off the card, so the
// apply must not spend the frame's verdict: a skip journalled as done is what
// would let a later card format destroy the only copy.
func TestSkippedMoveKeepsTheVerdictAndTheFile(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	settings := NewConfigService(a)

	libraryRoot := t.TempDir()
	cfg, err := settings.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	cfg.Behaviour.LibraryRoot = libraryRoot
	cfg.Behaviour.MoveOnImport = true
	cfg.Behaviour.CollisionPolicy = config.CollisionSkip
	if err := settings.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[1] // the JPEG-only DSCF0002
	decisions := NewDecisionService(a)
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "keep", "rj"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if err := decisions.SetDestination(frame.Hash, frame.Dir, frame.Stem, "keepers"); err != nil {
		t.Fatalf("SetDestination: %v", err)
	}

	// The destination is already occupied by a different photograph.
	occupied := filepath.Join(libraryRoot, "keepers", "DSCF0002.JPG")
	if err := os.MkdirAll(filepath.Dir(occupied), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("someone else's photo"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := NewApplyService(a, nil).Apply(dir, []string{frame.Hash})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(batch.Actions) != 1 || batch.Actions[0].Outcome != "skipped" {
		t.Fatalf("want one skipped action, got %+v", batch.Actions)
	}
	if !exists(t, filepath.Join(dir, "DSCF0002.JPG")) {
		t.Fatal("the skipped move took the photo off the card")
	}

	// The verdict survives, so the user can retry once the collision is
	// resolved rather than losing the frame's decision to a move that never
	// happened.
	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder after apply: %v", err)
	}
	kept := false
	for _, g := range reopened.Groups {
		if g.Stem == "DSCF0002" {
			kept = g.Verdict == "keep"
		}
	}
	if !kept {
		t.Error("the verdict was cleared although the move was skipped")
	}
}

// Undo puts the files back, and with them the decisions the apply consumed: a
// cull that was undone is a cull that has not happened yet, verdicts included.
func TestUndoRestoresTheCutVerdict(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)
	apply := NewApplyService(a, nil)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	// A cut with a mask, so the restore has to bring back both halves of the
	// record rather than a bare verdict.
	if err := decisions.SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "r"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if _, err := apply.Apply(dir, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The apply spent the verdict, as it always has.
	mid, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := mid.Groups[0].Verdict; got != "" {
		t.Fatalf("verdict after apply = %q, want cleared", got)
	}

	if err := apply.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Groups[0]
	if got.Verdict != "cut" || got.Mask != "r" {
		t.Errorf("verdict/mask after undo = %q/%q, want the cut/r the apply consumed", got.Verdict, got.Mask)
	}
}

// A partial undo restores only the decisions of the frames whose files all
// came back. A frame still half in the rejects must not regain a verdict that
// would act on files that are not there.
func TestUndoRestoresVerdictsOnlyForFramesThatCameBack(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)
	apply := NewApplyService(a, nil)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	for _, g := range folder.Groups {
		if err := decisions.SetVerdict(g.Hash, g.Dir, g.Stem, "cut", "rj"); err != nil {
			t.Fatalf("SetVerdict: %v", err)
		}
	}
	if _, err := apply.Apply(dir, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Something new occupies the first frame's RAW path, so its restore is
	// blocked while the rest of the folder comes back.
	if err := os.WriteFile(filepath.Join(dir, "DSCF0001.RAF"), []byte("a new photograph"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := apply.Undo(); err == nil {
		t.Fatal("an undo blocked from restoring a file reported success")
	}

	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[string]string{}
	for _, g := range reopened.Groups {
		verdicts[g.Stem] = g.Verdict
	}
	if verdicts["DSCF0002"] != "cut" {
		t.Errorf("DSCF0002 came back whole but its verdict did not: %q", verdicts["DSCF0002"])
	}
	if verdicts["DSCF0001"] != "" {
		t.Errorf("DSCF0001 is only half restored yet regained verdict %q", verdicts["DSCF0001"])
	}
}

// An apply built over the shell's catalogue handle prunes through that same
// handle rather than opening — and leaking — a fresh one per batch.
func TestApplyPrunesTheSharedCatalogue(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	index := NewLibraryIndexService(a)
	t.Cleanup(func() {
		if err := index.Close(); err != nil {
			t.Errorf("close catalogue: %v", err)
		}
	})
	if _, err := index.RegisterRoot(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := index.reindex(dir); err != nil {
		t.Fatal(err)
	}

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[1] // DSCF0002, the JPEG-only frame
	if err := NewDecisionService(a).SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	if _, err := NewApplyService(a, index).Apply(dir, []string{frame.Hash}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	res, err := index.Search("", FacetsDTO{}, 0, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, f := range res.Frames {
		if f.Stem == "DSCF0002" {
			t.Error("the applied frame is still in the catalogue")
		}
	}
	if res.Total != 1 {
		t.Errorf("catalogue holds %d frames after the prune, want the 1 survivor", res.Total)
	}
}

// With cuts scoped to the mask, a cut that masks both halves in plans nothing.
// A verdict whose files never moved must survive the apply: clearing it would
// silently erase a judgement in exchange for no work at all.
func TestAMaskedOutCutSurvivesAnApplyThatMovesNothing(t *testing.T) {
	a := testApp(t)
	a.cfg.Behaviour.CutRemoves = config.CutRemovesMasked
	dir := card(t)
	library := NewLibraryService(a)
	apply := NewApplyService(a, nil)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := NewDecisionService(a).SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}

	plan, err := apply.Plan(dir, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("a fully masked cut planned %d actions: %+v", len(plan.Actions), plan.Actions)
	}
	if _, err := apply.Apply(dir, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Groups[0].Verdict; got != "cut" {
		t.Errorf("verdict after an apply that moved nothing = %q, want the cut kept", got)
	}
}

// The folder is resolved once and the trasher built from the resolved path, so
// applying "~/shoot" in rejected-folder mode writes the rejects under the real
// folder rather than under a literal "~" beside the working directory.
func TestApplyResolvesTheFolderBeforePlacingRejects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	t.Chdir(work)

	a := testApp(t)
	shoot := filepath.Join(home, "shoot")
	if err := os.MkdirAll(shoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shoot, "DSCF0001.JPG"), []byte("jpeg bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	library := NewLibraryService(a)
	folder, err := library.OpenFolder(shoot)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := NewDecisionService(a).SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}

	if _, err := NewApplyService(a, nil).Apply("~/shoot", nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if exists(t, filepath.Join(work, "~")) {
		t.Error("the trasher was built from the unexpanded path: a literal ~ folder appeared under the working directory")
	}
	if !exists(t, filepath.Join(shoot, "_Rejected", "DSCF0001.JPG")) {
		t.Error("the reject did not land in the shoot folder's own _Rejected")
	}
}

// Ratings are set and batched separately from verdicts, and a rating alone is
// enough to remember a frame.
func TestRatingServiceRoundTrip(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)
	decisions := NewDecisionService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	items := make([]RatingItem, 0, len(folder.Groups))
	for i, g := range folder.Groups {
		items = append(items, RatingItem{Hash: g.Hash, Dir: g.Dir, Stem: g.Stem, Rating: i + 1})
	}
	if err := decisions.SetRatingBatch(items); err != nil {
		t.Fatalf("SetRatingBatch: %v", err)
	}

	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	for i, g := range reopened.Groups {
		if g.Rating != i+1 {
			t.Errorf("%s rating = %d, want %d", g.Stem, g.Rating, i+1)
		}
		if g.Verdict != "" {
			t.Errorf("%s gained verdict %q from a rating", g.Stem, g.Verdict)
		}
	}

	// A plan ignores rated frames: only a verdict asks for files to move.
	plan, err := NewApplyService(a, nil).Plan(dir, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("a rating planned %d actions: %+v", len(plan.Actions), plan.Actions)
	}
}

// The pre-verdict decision strings still arrive from the frontend that has not
// been restyled yet, and land as the verdict and mask they mean.
func TestLegacyDecisionServiceStillWrites(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	library := NewLibraryService(a)

	folder, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := NewDecisionService(a).SetBatch([]DecisionItem{
		{Hash: frame.Hash, Dir: frame.Dir, Stem: frame.Stem, Decision: "drop_jpeg"},
	}); err != nil {
		t.Fatalf("SetBatch: %v", err)
	}

	reopened, err := library.OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	got := reopened.Groups[0]
	if got.Verdict != "keep" || got.Mask != "r" {
		t.Errorf("drop_jpeg stored as %q/%q, want keep/r", got.Verdict, got.Mask)
	}
	if got.Decision != "drop_jpeg" {
		t.Errorf("Decision reads back as %q, want drop_jpeg", got.Decision)
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

func TestDecisionServiceRejectsUnknownValues(t *testing.T) {
	a := testApp(t)
	decisions := NewDecisionService(a)

	if err := decisions.Set("hash", "/card", "DSCF0001", "burn"); err == nil {
		t.Error("unknown decision accepted")
	}
	if err := decisions.SetVerdict("hash", "/card", "DSCF0001", "burn", "rj"); err == nil {
		t.Error("unknown verdict accepted")
	}
	if err := decisions.SetVerdict("hash", "/card", "DSCF0001", "keep", "raw"); err == nil {
		t.Error("unknown mask accepted")
	}
	if err := decisions.SetRating("hash", "/card", "DSCF0001", 6); err == nil {
		t.Error("rating off the scale accepted")
	}
}

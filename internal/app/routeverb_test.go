package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
)

// verbedItem is a routed frame that says for itself how it wants to travel,
// which is what pressing m or c records.
func verbedItem(stem, dest string, verb decide.Verb) planned {
	it := routedItem(stem, dest, decide.MaskBoth)
	it.record.Verb = verb
	return it
}

func TestBuildPlanTakesTheVerbFromTheFrame(t *testing.T) {
	// The configuration says copy; the frame says move. The frame wins — the
	// user pressed m on this one.
	items := []planned{verbedItem("DSCF0001", libAbs("/library/portraits"), decide.VerbMove)}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbMove {
			t.Errorf("verb %q on %s, want a move", a.Verb, a.Src)
		}
	}
	if p.dto.Destinations[0].Verb != "move" {
		t.Errorf("summary verb %q", p.dto.Destinations[0].Verb)
	}
	if !strings.Contains(p.dto.Description, "Move to") {
		t.Errorf("description %q", p.dto.Description)
	}
}

// And the other way round: the configuration moves by default, the frame was
// copied with c, and a copy leaves the card alone.
func TestBuildPlanCopiesWhenTheFrameSaysCopy(t *testing.T) {
	items := []planned{verbedItem("DSCF0001", libAbs("/library/portraits"), decide.VerbCopy)}
	p, err := buildPlan(items, importRules(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbCopy {
			t.Errorf("verb %q on %s, want a copy", a.Verb, a.Src)
		}
	}
	if p.dto.Destinations[0].Verb != "copy" {
		t.Errorf("summary verb %q", p.dto.Destinations[0].Verb)
	}
}

// A route recorded before verbs existed, or by a palette that did not say,
// still follows the setting.
func TestBuildPlanFallsBackToTheConfiguredVerb(t *testing.T) {
	items := []planned{verbedItem("DSCF0001", libAbs("/library/portraits"), decide.VerbDefault)}
	p, err := buildPlan(items, importRules(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbMove {
			t.Errorf("verb %q on %s, want the configured move", a.Verb, a.Src)
		}
	}
}

// One folder can be both a copy target and a move target in the same apply, so
// the summary reports each separately rather than picking one and lying about
// the other.
func TestBuildPlanSplitsOneDestinationByVerb(t *testing.T) {
	dest := libAbs("/library/portraits")
	items := []planned{
		verbedItem("DSCF0001", dest, decide.VerbMove),
		verbedItem("DSCF0002", dest, decide.VerbCopy),
	}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.dto.Destinations) != 2 {
		t.Fatalf("want one summary per verb, got %+v", p.dto.Destinations)
	}
	// Copies before moves at the same destination, so the same frames always
	// produce the same plan.
	if p.dto.Destinations[0].Verb != "copy" || p.dto.Destinations[1].Verb != "move" {
		t.Fatalf("summaries out of order: %+v", p.dto.Destinations)
	}
	for _, d := range p.dto.Destinations {
		if d.Path != dest || d.Frames != 1 {
			t.Errorf("summary %+v", d)
		}
	}

	verbs := map[string]ops.Verb{}
	for _, a := range p.actions {
		verbs[a.Src] = a.Verb
	}
	for src, v := range verbs {
		want := ops.VerbCopy
		if strings.Contains(src, "DSCF0001") {
			want = ops.VerbMove
		}
		if v != want {
			t.Errorf("%s planned as %q, want %q", src, v, want)
		}
	}
	if !strings.Contains(p.dto.Description, "Copy to") || !strings.Contains(p.dto.Description, "Move to") {
		t.Errorf("description %q", p.dto.Description)
	}
}

// A move takes the frame off the card, so the whole decision — destination and
// verb alike — is spent by the apply and has to come back on an undo. Restored
// as a copy it would silently import the same frame twice.
func TestUndoRestoresTheRouteVerb(t *testing.T) {
	card, library := importTree(t)

	cfg := config.Default()
	cfg.Behaviour.LibraryRoot = library
	a := newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), cfg)
	t.Cleanup(func() { a.Close() })

	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := scanCard(t, a, card)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("scanned %d frames, want 1", len(groups))
	}
	// The setting says copy, so only the frame's own verb can make this a move.
	if err := store.SetDestination(groups[0].Hash, card, groups[0].Stem, "keepers", decide.VerbMove); err != nil {
		t.Fatal(err)
	}

	svc := NewApplyService(a, nil)
	batch, err := svc.Apply(card, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range batch.Actions {
		if action.Outcome != journal.OutcomeOK {
			t.Fatalf("%s %s: %s", action.Verb, action.Src, action.Err)
		}
		if action.Verb != string(ops.VerbMove) {
			t.Errorf("%s applied as %q, want a move", action.Src, action.Verb)
		}
	}
	if _, err := os.Lstat(filepath.Join(card, "DSCF0001.RAF")); !os.IsNotExist(err) {
		t.Errorf("a move left the original on the card: %v", err)
	}

	if err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(card, "DSCF0001.RAF")); err != nil {
		t.Fatalf("undo did not put the frame back: %v", err)
	}
	r, ok, err := store.Get(groups[0].Hash, groups[0].Dir, groups[0].Stem)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || r.Destination != "keepers" {
		t.Fatalf("undo did not restore the routing: %+v (ok=%v)", r, ok)
	}
	if r.Verb != decide.VerbMove {
		t.Errorf("undo restored the route as %q, want a move", r.Verb)
	}
}

// routeAllWith routes every frame in dir with a verb of its own, which is what
// pressing m or c on a selection records.
func routeAllWith(t *testing.T, a *App, dir, destination, verb string) {
	t.Helper()
	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := NewDecisionService(a)
	for _, g := range folder.Groups {
		if err := decisions.SetDestination(g.Hash, g.Dir, g.Stem, destination, verb); err != nil {
			t.Fatal(err)
		}
	}
}

// The import screen reads the plan, not the setting, so a card routed with m
// warns that the frames are leaving even though the default is to copy.
func TestImportPlanReportsTheVerbTheFramesCarry(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())
	routeAllWith(t, a, dir, "2026/portraits", string(decide.VerbMove))

	plan, err := importService(t, a).ImportPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Verb != "move" {
		t.Errorf("plan verb %q, want move", plan.Verb)
	}
	if len(plan.Routes) != 1 || plan.Routes[0].Verb != "move" {
		t.Errorf("routes %+v", plan.Routes)
	}
}

// A card holding both is neither a copy nor a move, and the screen has to be
// able to say so rather than promising one of them.
func TestImportPlanReportsMixedVerbs(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(folder.Groups) < 2 {
		t.Fatalf("fixture holds %d frames, want at least 2", len(folder.Groups))
	}
	decisions := NewDecisionService(a)
	verbs := []string{string(decide.VerbMove), string(decide.VerbCopy)}
	for i, g := range folder.Groups {
		if err := decisions.SetDestination(g.Hash, g.Dir, g.Stem, "2026/portraits", verbs[i%2]); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := importService(t, a).ImportPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Verb != "mixed" {
		t.Errorf("plan verb %q, want mixed", plan.Verb)
	}
}

// The backup leg exists so a moving import never has only one copy of a
// photograph in flight. It has to be ordered by what the plan does, not by
// what the setting says, or a card routed with m under a copying default
// would have its originals removed before the second copy was read.
func TestExecuteBacksUpBeforeAMoveTheFrameAskedFor(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	backup := t.TempDir()
	a := importApp(t, libraryRoot)
	// The setting copies; only the frame's verb makes this a move.
	routeAllWith(t, a, dir, "2026/portraits", string(decide.VerbMove))

	if _, err := importService(t, a).Execute(dir, backup); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !exists(t, filepath.Join(backup, "2026", "portraits", "DSCF0001.RAF")) {
		t.Error("the backup copy is missing: it was planned after the move")
	}
	if !exists(t, filepath.Join(libraryRoot, "2026", "portraits", "DSCF0001.RAF")) {
		t.Error("the library copy is missing")
	}
	if exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
		t.Error("a moving import left the frame on the card")
	}
}

// Free space is a property of the folder, not of how the frames reach it, so
// a destination that is both copied and moved into is weighed once. Two rows
// for one folder would double-count nothing and read as two places.
func TestImportPlanWeighsAMixedDestinationOnce(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := NewDecisionService(a)
	verbs := []string{string(decide.VerbMove), string(decide.VerbCopy)}
	for i, g := range folder.Groups {
		if err := decisions.SetDestination(g.Hash, g.Dir, g.Stem, "2026/portraits", verbs[i%2]); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := importService(t, a).ImportPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Routes) != 2 {
		t.Fatalf("want one route per verb, got %+v", plan.Routes)
	}
	if len(plan.Space) != 1 {
		t.Fatalf("want one space row per folder, got %+v", plan.Space)
	}
	if plan.Space[0].Frames != len(folder.Groups) {
		t.Errorf("space row holds %d frames, want %d", plan.Space[0].Frames, len(folder.Groups))
	}
	var bytes int64
	for _, r := range plan.Routes {
		bytes += r.Bytes
	}
	if plan.Space[0].Bytes != bytes {
		t.Errorf("space row holds %d bytes, want %d", plan.Space[0].Bytes, bytes)
	}
}

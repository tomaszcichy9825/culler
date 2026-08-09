package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/hash"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// hashCounter is hash.Content with a tally. Planning a folder costs one head
// read per frame it decides to identify, so the tally is the cost of a plan.
type hashCounter struct {
	mu sync.Mutex
	n  int
}

func (c *hashCounter) read(path string) (string, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return hash.Content(path)
}

func (c *hashCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// bigCard writes frames paired RAW+JPEG frames into a fresh directory. The
// JPEG is the frame's primary file and so the one an identity read opens, so
// it is the only one worth making big enough to cost something.
func bigCard(t *testing.T, frames, jpegBytes int) string {
	t.Helper()
	dir := t.TempDir()
	jpeg := make([]byte, jpegBytes)
	for i := range jpeg {
		jpeg[i] = byte(i)
	}
	for i := 0; i < frames; i++ {
		stem := fmt.Sprintf("DSCF%04d", i)
		// The tail differs per frame so no two frames share an identity.
		body := append(append([]byte(nil), jpeg...), []byte(stem)...)
		write(t, filepath.Join(dir, stem+".JPG"), body)
		write(t, filepath.Join(dir, stem+".RAF"), []byte("raw "+stem))
	}
	return dir
}

func write(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// decideOne records a cut on one frame of an opened folder and returns its
// scope reference.
func decideOne(t *testing.T, a *App, dir string, index int) FrameRef {
	t.Helper()
	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[index]
	if err := NewDecisionService(a).SetVerdict(frame.Hash, frame.Dir, frame.Stem, "cut", "rj"); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	return FrameRef{Dir: frame.Dir, Hash: frame.Hash}
}

// A plan over a scope must open only the frames that could produce an action.
// Reading every frame in the folder is what made pressing apply on a full card
// feel like the app had hung.
func TestPlanScopeReadsOnlyTheDecidedFrames(t *testing.T) {
	a := testApp(t)
	dir := bigCard(t, 200, 1024)
	ref := decideOne(t, a, dir, 7)

	counter := &hashCounter{}
	apply := NewApplyService(a, nil)
	apply.hashFn = counter.read

	plan, err := apply.PlanScope([]FrameRef{ref})
	if err != nil {
		t.Fatalf("PlanScope: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("%d planned actions, want 2 (the RAW and the JPEG of one cut frame): %+v", len(plan.Actions), plan.Actions)
	}
	if got := counter.count(); got != 1 {
		t.Errorf("planning one frame out of 200 read %d frames, want 1", got)
	}
}

// The same for a whole-folder plan: the decisions name the frames, so the
// hundreds of undecided ones are never opened.
func TestPlanFolderReadsOnlyTheDecidedFrames(t *testing.T) {
	a := testApp(t)
	dir := bigCard(t, 200, 1024)
	decideOne(t, a, dir, 3)
	decideOne(t, a, dir, 11)

	counter := &hashCounter{}
	apply := NewApplyService(a, nil)
	apply.hashFn = counter.read

	plan, err := apply.Plan(dir, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 4 {
		t.Fatalf("%d planned actions, want 4: %+v", len(plan.Actions), plan.Actions)
	}
	if got := counter.count(); got != 2 {
		t.Errorf("planning 2 decided frames out of 200 read %d frames, want 2", got)
	}
}

// Narrowing the identity reads must not narrow identity itself. A frame edited
// since it was judged has a different hash, so its decision no longer applies —
// and the plan has to notice that by reading the frame, not by trusting the
// stem the decision was recorded against.
func TestPlanIgnoresAFrameEditedSinceItWasDecided(t *testing.T) {
	a := testApp(t)
	dir := bigCard(t, 8, 1024)
	ref := decideOne(t, a, dir, 2)

	// The frame is rewritten: same name, different content, so a different
	// identity. Nothing may be planned against it.
	write(t, filepath.Join(dir, "DSCF0002.JPG"), []byte(strings.Repeat("edited", 300)))

	apply := NewApplyService(a, nil)
	plan, err := apply.Plan(dir, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("planned %d actions against an edited frame, want none: %+v", len(plan.Actions), plan.Actions)
	}
	scoped, err := apply.PlanScope([]FrameRef{ref})
	if err != nil {
		t.Fatalf("PlanScope: %v", err)
	}
	if len(scoped.Actions) != 0 {
		t.Fatalf("scope planned %d actions against an edited frame, want none: %+v", len(scoped.Actions), scoped.Actions)
	}
}

// applyRecorder collects the events a service emits instead of posting them to a
// webview that is not running.
type applyRecorder struct {
	mu     sync.Mutex
	events []ApplyProgress
}

func (r *applyRecorder) emit(name string, data any) {
	if name != EventApplyProgress {
		return
	}
	p, ok := data.(ApplyProgress)
	if !ok {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, p)
	r.mu.Unlock()
}

func (r *applyRecorder) phase(name string) []ApplyProgress {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ApplyProgress
	for _, e := range r.events {
		if e.Phase == name {
			out = append(out, e)
		}
	}
	return out
}

// Pressing apply has to look like something is happening. The backend says how
// far it has got through both halves of the work — identifying the judged
// frames, then moving their files — so the confirm button and the status bar
// can stop looking dead.
func TestApplyEmitsProgress(t *testing.T) {
	a := testApp(t)
	dirA, dirB := card(t), card(t)

	var refs []FrameRef
	for _, dir := range []string{dirA, dirB} {
		refs = append(refs, decideOne(t, a, dir, 0))
	}

	rec := &applyRecorder{}
	apply := NewApplyService(a, nil)
	apply.emit = rec.emit

	if _, err := apply.ApplyScope(refs); err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}

	planning := rec.phase(ApplyPhasePlanning)
	if len(planning) == 0 {
		t.Fatal("no planning progress at all — the wait before the dialog is unexplained")
	}
	last := planning[len(planning)-1]
	if last.Done != last.Total || last.Total != 2 {
		t.Errorf("planning ended at %d/%d, want 2/2 — one per folder in the scope", last.Done, last.Total)
	}

	applying := rec.phase(ApplyPhaseApplying)
	if len(applying) == 0 {
		t.Fatal("no applying progress at all — the file work is the slow half")
	}
	last = applying[len(applying)-1]
	// Three files per cut frame across two folders: RAW, JPEG and the sidecar.
	if last.Done != 6 || last.Total != 6 {
		t.Errorf("applying ended at %d/%d, want 6/6", last.Done, last.Total)
	}
	for _, e := range applying {
		if e.Done > e.Total {
			t.Errorf("progress %d/%d overshoots", e.Done, e.Total)
		}
	}
}

// After an apply the frontend has to be able to drop the frames that left
// without reopening the folder, so the batch names them.
func TestApplyReportsTheFramesItConsumed(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	ref := decideOne(t, a, dir, 0)

	batch, err := NewApplyService(a, nil).ApplyScope([]FrameRef{ref})
	if err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}
	if len(batch.Removed) != 1 {
		t.Fatalf("batch reported %d removed frames, want 1: %+v", len(batch.Removed), batch.Removed)
	}
	got := batch.Removed[0]
	if got.Dir != dir || got.Stem != "DSCF0001" || got.Hash != ref.Hash {
		t.Errorf("removed frame = %+v, want %s/DSCF0001 with hash %s", got, dir, ref.Hash)
	}
	if len(batch.Unrouted) != 0 {
		t.Errorf("a cut frame is not a routed one: %+v", batch.Unrouted)
	}
}

// A frame copied into the library keeps its files and loses its routing, so it
// is reported separately: the grid clears its destination badge rather than
// dropping the frame.
func TestApplyReportsAnImportedFrameAsUnrouted(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	dest := t.TempDir()

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}
	frame := folder.Groups[0]
	if err := NewDecisionService(a).SetDestination(frame.Hash, frame.Dir, frame.Stem, dest); err != nil {
		t.Fatalf("SetDestination: %v", err)
	}

	batch, err := NewApplyService(a, nil).ApplyScope([]FrameRef{{Dir: dir, Hash: frame.Hash}})
	if err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}
	if len(batch.Removed) != 0 {
		t.Errorf("a copy into the library takes nothing off the card: %+v", batch.Removed)
	}
	if len(batch.Unrouted) != 1 {
		t.Fatalf("batch reported %d unrouted frames, want 1: %+v", len(batch.Unrouted), batch.Unrouted)
	}
	if batch.Unrouted[0].Stem != "DSCF0001" {
		t.Errorf("unrouted frame = %+v, want DSCF0001", batch.Unrouted[0])
	}
}

// The measurement behind the performance work: how long the plan-then-apply
// round trip takes on a folder far bigger than the handful of frames being
// culled. The numbers go in the pull request; the test only fails if the work
// itself fails.
func TestApplyRoundTripTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("writes tens of megabytes")
	}
	const frames = 500
	const jpegBytes = 80 << 10

	a := testApp(t)
	start := time.Now()
	dir := bigCard(t, frames, jpegBytes)
	t.Logf("wrote %d frames (%d KB each) in %v", frames, jpegBytes>>10, time.Since(start))

	var refs []FrameRef
	for _, i := range []int{1, 50, 123, 400, 499} {
		refs = append(refs, decideOne(t, a, dir, i))
	}

	// The walk on its own, so the identity reads can be told from it. It is the
	// floor a plan cannot go below without trusting a stale listing.
	start = time.Now()
	if _, err := scan.ScanDir(dir, a.Config().ScanConfig()); err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	walk := time.Since(start)

	counter := &hashCounter{}
	apply := NewApplyService(a, nil)
	apply.hashFn = counter.read

	start = time.Now()
	plan, err := apply.PlanScope(refs)
	if err != nil {
		t.Fatalf("PlanScope: %v", err)
	}
	planned := time.Since(start)

	start = time.Now()
	batch, err := apply.ApplyScope(refs)
	if err != nil {
		t.Fatalf("ApplyScope: %v", err)
	}
	applied := time.Since(start)

	t.Logf("folder of %d frames, %d decided: walk alone %v, plan %v, apply %v, total %v (%d actions, %d identity reads)",
		frames, len(refs), walk, planned, applied, planned+applied, len(plan.Actions), counter.count())
	for _, action := range batch.Actions {
		if action.Outcome != "ok" {
			t.Errorf("action %+v did not succeed", action)
		}
	}
}

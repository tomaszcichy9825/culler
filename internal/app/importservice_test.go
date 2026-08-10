package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// importApp returns an App whose library root is a folder the test owns, so an
// import lands somewhere it can be asserted on and nowhere near the real one.
func importApp(t *testing.T, libraryRoot string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Behaviour.TrashMode = config.TrashRejectedFolder
	cfg.Behaviour.RejectedFolderName = "_Rejected"
	cfg.Behaviour.LibraryRoot = libraryRoot

	a := newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), cfg)
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	return a
}

// importService binds a service to a, with its card detection answering the
// volumes the test names rather than whatever is plugged into the machine.
func importService(t *testing.T, a *App, vols ...platform.Volume) *ImportService {
	t.Helper()
	index := NewLibraryIndexService(a)
	t.Cleanup(func() {
		if err := index.Close(); err != nil {
			t.Errorf("close catalogue: %v", err)
		}
	})
	s := NewImportService(a, index)
	if len(vols) > 0 {
		s.volumes = func() ([]platform.Volume, error) { return vols, nil }
	}
	return s
}

// writeFrame writes one RAW+JPEG pair.
func writeFrame(t *testing.T, dir, stem string) {
	t.Helper()
	for ext, body := range map[string]string{".RAF": "raw bytes of " + stem, ".JPG": "jpeg bytes of " + stem} {
		if err := os.WriteFile(filepath.Join(dir, stem+ext), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// cardDir writes a DCIM card holding one image folder per count, each with
// that many frames, and returns the card root.
func cardDir(t *testing.T, counts ...int) string {
	t.Helper()
	root := t.TempDir()
	for i, n := range counts {
		dir := filepath.Join(root, "DCIM", fmt.Sprintf("%03d_FUJI", 100+i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 1; f <= n; f++ {
			writeFrame(t, dir, fmt.Sprintf("DSCF%04d", i*1000+f))
		}
	}
	return root
}

// imageDir is the folder inside a card that holds frames.
func imageDir(card string, index int) string {
	return filepath.Join(card, "DCIM", fmt.Sprintf("%03d_FUJI", 100+index))
}

// routeAll records a destination for every frame in dir, which is what
// reviewing the card in CULL leaves behind.
func routeAll(t *testing.T, a *App, dir, destination string) {
	t.Helper()
	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := NewDecisionService(a)
	for _, g := range folder.Groups {
		if err := decisions.SetDestination(g.Hash, g.Dir, g.Stem, destination, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectCardsListsOnlyRemovableVolumes(t *testing.T) {
	card := cardDir(t, 3)
	a := importApp(t, t.TempDir())
	s := importService(t, a,
		platform.Volume{Path: t.TempDir(), Name: "Macintosh HD", Total: 500, Free: 100},
		platform.Volume{Path: card, Name: "UNTITLED", Removable: true, Total: 64000, Free: 12000},
		platform.Volume{Path: t.TempDir(), Name: "archive", Removable: true, Network: true},
	)

	cards, err := s.DetectCards()
	if err != nil {
		t.Fatalf("DetectCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("detected %d cards, want the one removable local volume: %+v", len(cards), cards)
	}
	got := cards[0]
	if got.Path != card || got.Name != "UNTITLED" {
		t.Errorf("card = %q/%q, want %q/UNTITLED", got.Path, got.Name, card)
	}
	if got.Total != 64000 || got.Free != 12000 {
		t.Errorf("capacity = %d of %d, want the volume's own figures", got.Free, got.Total)
	}
	if !got.HasDCIM {
		t.Error("a card with a DCIM folder reported as having none")
	}
	if got.Dir != imageDir(card, 0) {
		t.Errorf("first folder = %q, want %q", got.Dir, imageDir(card, 0))
	}
}

func TestDetectCardsCountsFramesInASingleFolderExactly(t *testing.T) {
	card := cardDir(t, 4)
	a := importApp(t, t.TempDir())
	s := importService(t, a, platform.Volume{Path: card, Name: "CARD", Removable: true})

	cards, err := s.DetectCards()
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].Frames != 4 {
		t.Errorf("frames = %d, want 4", cards[0].Frames)
	}
	if cards[0].Estimated {
		t.Error("one folder is one listing: the count is exact, not an estimate")
	}
	if cards[0].Folders != 1 {
		t.Errorf("folders = %d, want 1", cards[0].Folders)
	}
}

func TestDetectCardsEstimatesAcrossFolders(t *testing.T) {
	// Three folders of five. Only the first is read — a card is not walked to
	// draw a list — so the count is five times three and says it is a guess.
	card := cardDir(t, 5, 5, 5)
	a := importApp(t, t.TempDir())
	s := importService(t, a, platform.Volume{Path: card, Name: "CARD", Removable: true})

	cards, err := s.DetectCards()
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].Folders != 3 {
		t.Errorf("folders = %d, want 3", cards[0].Folders)
	}
	if cards[0].Frames != 15 {
		t.Errorf("frames = %d, want 15", cards[0].Frames)
	}
	if !cards[0].Estimated {
		t.Error("a count extrapolated from one folder must say so")
	}
}

func TestDetectCardsHandlesAVolumeWithNoDCIM(t *testing.T) {
	stick := t.TempDir()
	writeFrame(t, stick, "DSCF0001")
	a := importApp(t, t.TempDir())
	s := importService(t, a, platform.Volume{Path: stick, Name: "STICK", Removable: true})

	cards, err := s.DetectCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("detected %d cards, want 1", len(cards))
	}
	if cards[0].HasDCIM {
		t.Error("reported a DCIM folder that is not there")
	}
	if cards[0].Frames != 1 || cards[0].Dir != stick {
		t.Errorf("frames = %d in %q, want 1 in the volume root", cards[0].Frames, cards[0].Dir)
	}
}

func TestDetectCardsSurvivesAVolumeThatHasGone(t *testing.T) {
	a := importApp(t, t.TempDir())
	s := importService(t, a, platform.Volume{Path: "/definitely/not/mounted", Name: "GONE", Removable: true})

	cards, err := s.DetectCards()
	if err != nil {
		t.Fatalf("a card pulled mid-scan is not an error: %v", err)
	}
	if len(cards) != 1 || cards[0].Frames != 0 {
		t.Fatalf("cards = %+v, want the volume listed with nothing on it", cards)
	}
}

func TestCardSummaryCountsEveryFolder(t *testing.T) {
	card := cardDir(t, 2, 3)
	a := importApp(t, t.TempDir())
	s := importService(t, a)

	got, err := s.CardSummary(card)
	if err != nil {
		t.Fatalf("CardSummary: %v", err)
	}
	if got.Frames != 5 {
		t.Errorf("frames = %d, want 5", got.Frames)
	}
	if len(got.Dirs) != 2 {
		t.Fatalf("folders = %d, want 2", len(got.Dirs))
	}
	if got.Dirs[0].Frames != 2 || got.Dirs[1].Frames != 3 {
		t.Errorf("per-folder frames = %d/%d, want 2/3", got.Dirs[0].Frames, got.Dirs[1].Frames)
	}
	if got.Bytes <= 0 {
		t.Error("a card holding files reports no bytes")
	}
	if got.Files != 10 {
		t.Errorf("files = %d, want two per frame", got.Files)
	}
}

// A folder the summary cannot read is still on the card: it appears in the
// listing with zero frames rather than vanishing from the summary, because a
// folder silently missing understates what an import is about to leave behind.
func TestCardSummaryListsAFolderItCannotRead(t *testing.T) {
	card := cardDir(t, 2, 3)
	locked := imageDir(card, 1)
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, err := os.Stat(filepath.Join(locked, "DSCF1001.RAF")); err == nil {
		t.Skip("cannot make the directory untraversable on this platform")
	}
	a := importApp(t, t.TempDir())
	s := importService(t, a)

	got, err := s.CardSummary(card)
	if err != nil {
		t.Fatalf("CardSummary: %v", err)
	}
	if len(got.Dirs) != 2 {
		t.Fatalf("folders = %d, want both listed, the unreadable one included: %+v", len(got.Dirs), got.Dirs)
	}
	if got.Dirs[1].Path != locked || got.Dirs[1].Frames != 0 {
		t.Errorf("unreadable folder listed as %+v, want its path with zero frames", got.Dirs[1])
	}
	if got.Frames != 2 {
		t.Errorf("frames = %d, want the 2 the readable folder holds", got.Frames)
	}
}

func TestCardSummaryClustersShotTimesIntoSessions(t *testing.T) {
	card := cardDir(t, 4)
	dir := imageDir(card, 0)
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	// Two frames in the morning, two after a break longer than the session gap.
	when := []time.Time{base, base.Add(2 * time.Minute), base.Add(9 * time.Hour), base.Add(9*time.Hour + 3*time.Minute)}
	for i, ts := range when {
		stem := fmt.Sprintf("DSCF%04d", i+1)
		for _, ext := range []string{".RAF", ".JPG"} {
			if err := os.Chtimes(filepath.Join(dir, stem+ext), ts, ts); err != nil {
				t.Fatal(err)
			}
		}
	}

	a := importApp(t, t.TempDir())
	got, err := importService(t, a).CardSummary(card)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", got.Sessions)
	}
	if got.First == "" || got.Last == "" {
		t.Errorf("span = %q..%q, want both ends of the card", got.First, got.Last)
	}
}

func TestCardSummaryCountsWhatIsAlreadyInTheCatalogue(t *testing.T) {
	card := cardDir(t, 3)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)

	// The library already holds byte-identical copies of two of the frames,
	// which is what a second import of the same card would find.
	for _, stem := range []string{"DSCF0001", "DSCF0002"} {
		writeFrame(t, libraryRoot, stem)
	}
	index := NewLibraryIndexService(a)
	t.Cleanup(func() { _ = index.Close() })
	if _, err := index.RegisterRoot(libraryRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := index.reindex(libraryRoot); err != nil {
		t.Fatal(err)
	}

	got, err := NewImportService(a, index).CardSummary(card)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sampled != 3 {
		t.Fatalf("sampled %d frames, want all 3 of a card this small", got.Sampled)
	}
	if got.Imported != 2 {
		t.Errorf("already imported = %d of %d sampled, want 2", got.Imported, got.Sampled)
	}
}

func TestCardSummaryWithoutACatalogueReportsNoSample(t *testing.T) {
	card := cardDir(t, 2)
	a := importApp(t, t.TempDir())

	got, err := NewImportService(a, nil).CardSummary(card)
	if err != nil {
		t.Fatalf("a service with no catalogue must still summarise: %v", err)
	}
	if got.Frames != 2 {
		t.Errorf("frames = %d, want 2", got.Frames)
	}
	if got.Sampled != 0 || got.Imported != 0 {
		t.Errorf("sampled %d/%d, want nothing claimed without a catalogue to claim it from", got.Imported, got.Sampled)
	}
}

func TestImportPlanSplitsRoutedFromUnrouted(t *testing.T) {
	card := cardDir(t, 4)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := NewDecisionService(a)
	// Two routed to one folder, one cut, one left alone.
	for _, g := range folder.Groups[:2] {
		if err := decisions.SetDestination(g.Hash, g.Dir, g.Stem, "2026/portraits", ""); err != nil {
			t.Fatal(err)
		}
	}
	third := folder.Groups[2]
	if err := decisions.SetVerdict(third.Hash, third.Dir, third.Stem, string(decide.Cut), string(decide.MaskBoth)); err != nil {
		t.Fatal(err)
	}

	got, err := importService(t, a).ImportPlan(dir)
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if got.Frames != 4 {
		t.Errorf("frames = %d, want 4", got.Frames)
	}
	// Routed, cut and unrouted partition the folder: two going to the library,
	// one being dropped, one nobody has dealt with.
	if got.Routed != 2 || got.Cut != 1 || got.Unrouted != 1 {
		t.Errorf("routed/cut/unrouted = %d/%d/%d, want 2/1/1", got.Routed, got.Cut, got.Unrouted)
	}
	if got.Undecided != 1 {
		t.Errorf("undecided = %d, want the one frame nobody judged", got.Undecided)
	}
	if len(got.Routes) != 1 {
		t.Fatalf("routes = %+v, want one destination", got.Routes)
	}
	route := got.Routes[0]
	if route.Destination != "2026/portraits" {
		t.Errorf("destination = %q, want it as the user recorded it", route.Destination)
	}
	if route.Path != filepath.Join(libraryRoot, "2026/portraits") {
		t.Errorf("resolved path = %q, want it under the library root", route.Path)
	}
	if route.Frames != 2 || route.Files != 4 {
		t.Errorf("route = %d frames / %d files, want 2/4", route.Frames, route.Files)
	}
	if route.Bytes <= 0 {
		t.Error("a route carrying files reports no bytes")
	}
	if got.Verb != "copy" {
		t.Errorf("verb = %q, want copy: an import leaves the card as it found it", got.Verb)
	}
}

func TestImportPlanOfAFolderNobodyRoutedWarnsAboutAllOfIt(t *testing.T) {
	card := cardDir(t, 3)
	a := importApp(t, t.TempDir())

	got, err := importService(t, a).ImportPlan(imageDir(card, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Routed != 0 || got.Unrouted != 3 {
		t.Errorf("routed/unrouted = %d/%d, want 0/3", got.Routed, got.Unrouted)
	}
	if len(got.Routes) != 0 {
		t.Errorf("routes = %+v, want none", got.Routes)
	}
}

func TestExecuteCopiesRoutedFramesIntoTheLibrary(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)
	routeAll(t, a, dir, "2026/portraits")

	batch, err := importService(t, a).Execute(dir, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(batch.Actions) != 4 {
		t.Fatalf("batch holds %d actions, want two files for each of two frames", len(batch.Actions))
	}
	for _, action := range batch.Actions {
		if action.Outcome != "ok" {
			t.Errorf("%s %s: %s", action.Verb, action.Src, action.Err)
		}
	}
	for _, name := range []string{"DSCF0001.RAF", "DSCF0001.JPG", "DSCF0002.RAF", "DSCF0002.JPG"} {
		if !exists(t, filepath.Join(libraryRoot, "2026", "portraits", name)) {
			t.Errorf("%s did not land in the library", name)
		}
		// An import reads the card and leaves it exactly as it found it.
		if !exists(t, filepath.Join(dir, name)) {
			t.Errorf("%s was taken off the card by a copying import", name)
		}
	}
}

func TestExecuteWritesTheSecondCopyToTheBackup(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	backup := t.TempDir()
	a := importApp(t, libraryRoot)
	routeAll(t, a, dir, "2026/portraits")

	batch, err := importService(t, a).Execute(dir, backup)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(batch.Actions) != 4 {
		t.Fatalf("batch holds %d actions, want each of two files copied twice", len(batch.Actions))
	}
	for _, name := range []string{"DSCF0001.RAF", "DSCF0001.JPG"} {
		if !exists(t, filepath.Join(libraryRoot, "2026", "portraits", name)) {
			t.Errorf("%s did not land in the library", name)
		}
		// The backup mirrors the library's own layout, so the same frame is
		// found in the same place on both.
		if !exists(t, filepath.Join(backup, "2026", "portraits", name)) {
			t.Errorf("%s did not land in the backup", name)
		}
	}
}

func TestExecuteClearsWhatItImportedAndLeavesTheRest(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	routed, untouched := folder.Groups[0], folder.Groups[1]
	if err := NewDecisionService(a).SetDestination(routed.Hash, routed.Dir, routed.Stem, "2026/portraits", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := importService(t, a).Execute(dir, ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.Get(routed.Hash, routed.Dir, routed.Stem)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rec.Verdict != decide.Keep {
		t.Errorf("a keep survives its import — the judgement is not consumed by the copy: %v %q", ok, rec.Verdict)
	}
	if rec.Destination != "" {
		t.Errorf("the destination is consumed once the copy lands, got %q", rec.Destination)
	}
	if _, ok, err := store.Get(untouched.Hash, untouched.Dir, untouched.Stem); err != nil || ok {
		t.Errorf("a frame nobody routed gained a record: %v %v", ok, err)
	}
}

// An import is ingest, not a cull: cut and keep verdicts on the card's frames
// are PHOTOS mode's business, and the import must not plan so much as a trash
// against the source card — in rejected-folder mode that trash would write a
// rejected folder onto the card itself.
func TestExecuteLeavesCutFramesOnTheCard(t *testing.T) {
	card := cardDir(t, 3)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)

	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := NewDecisionService(a)
	// One routed, one cut, one untouched.
	routed, cut := folder.Groups[0], folder.Groups[1]
	if err := decisions.SetDestination(routed.Hash, routed.Dir, routed.Stem, "2026/portraits", ""); err != nil {
		t.Fatal(err)
	}
	if err := decisions.SetVerdict(cut.Hash, cut.Dir, cut.Stem, string(decide.Cut), string(decide.MaskBoth)); err != nil {
		t.Fatal(err)
	}

	s := importService(t, a)
	plan, err := s.ImportPlan(dir)
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if plan.Cut != 1 {
		t.Errorf("plan reports %d cut frames, want 1", plan.Cut)
	}

	batch, err := s.Execute(dir, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The plan's numbers are a promise about what execute does: the two files
	// of the one routed frame, and nothing else.
	if len(batch.Actions) != plan.Files || plan.Files != 2 {
		t.Fatalf("executed %d actions against a plan of %d files, want 2", len(batch.Actions), plan.Files)
	}
	for _, action := range batch.Actions {
		if action.Verb != "copy" {
			t.Errorf("an import executed a %s on %s; it may only copy", action.Verb, action.Src)
		}
	}

	// The card is left exactly as it was found: every file still there, and no
	// rejected folder written onto it.
	for _, stem := range []string{"DSCF0001", "DSCF0002", "DSCF0003"} {
		for _, ext := range []string{".RAF", ".JPG"} {
			if !exists(t, filepath.Join(dir, stem+ext)) {
				t.Errorf("the import took %s off the card", stem+ext)
			}
		}
	}
	if exists(t, filepath.Join(dir, "_Rejected")) {
		t.Error("the import wrote a rejected folder onto the card")
	}

	// The cut is left in place for PHOTOS mode to carry out, not consumed by
	// an import that did nothing to its files.
	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok, err := store.Get(cut.Hash, cut.Dir, cut.Stem); err != nil || !ok || rec.Verdict != decide.Cut {
		t.Errorf("the cut verdict did not survive the import: %v %v %+v", ok, err, rec)
	}
}

func TestExecuteIsUndoneThroughTheJournal(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	backup := t.TempDir()
	a := importApp(t, libraryRoot)
	routeAll(t, a, dir, "2026/portraits")

	if _, err := importService(t, a).Execute(dir, backup); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// One batch covers the library copy and the backup copy, so one undo takes
	// the whole import back.
	if err := NewApplyService(a, nil).Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	for _, root := range []string{libraryRoot, backup} {
		if exists(t, filepath.Join(root, "2026", "portraits", "DSCF0001.RAF")) {
			t.Errorf("undo left a copy under %s", root)
		}
	}
	if !exists(t, filepath.Join(dir, "DSCF0001.RAF")) {
		t.Error("undo of an import must never touch the card")
	}

	// The routing the import consumed comes back with the files it removed, so
	// the same import can simply be run again.
	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	g := folder.Groups[0]
	rec, ok, err := store.Get(g.Hash, g.Dir, g.Stem)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rec.Destination != "2026/portraits" {
		t.Errorf("undo did not restore the routing: %v %+v", ok, rec)
	}
}

func TestExecuteBacksUpBeforeTakingFramesOffTheCard(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	backup := t.TempDir()
	a := importApp(t, libraryRoot)
	a.cfg.Behaviour.MoveOnImport = true
	routeAll(t, a, dir, "2026/portraits")

	if _, err := importService(t, a).Execute(dir, backup); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The second copy is read off the card, so it has to be written before the
	// move takes the file away.
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

func TestExecuteReportsProgressThroughToTheEnd(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	backup := t.TempDir()
	a := importApp(t, t.TempDir())
	routeAll(t, a, dir, "2026/portraits")

	s := importService(t, a)
	var reports []ImportProgress
	s.onProgress = func(p ImportProgress) { reports = append(reports, p) }

	if _, err := s.Execute(dir, backup); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("an import that copied eight files reported nothing")
	}
	last := reports[len(reports)-1]
	if !last.Complete {
		t.Error("the last report of a finished import must say so")
	}
	if last.Files != last.Total || last.Total != 8 {
		t.Errorf("finished at %d of %d, want 8 of 8", last.Files, last.Total)
	}
	if last.Error != "" {
		t.Errorf("import reported an error: %s", last.Error)
	}

	var phases []string
	for _, r := range reports {
		if len(phases) == 0 || phases[len(phases)-1] != r.Phase {
			phases = append(phases, r.Phase)
		}
	}
	// The card is read, then written to the library, then written to the
	// backup — and the panel names the one it is in.
	want := []string{ImportPhaseScan, ImportPhaseCopy, ImportPhaseBackup}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i, phase := range want {
		if phases[i] != phase {
			t.Errorf("phase %d = %q, want %q", i, phases[i], phase)
		}
	}
}

func TestExecuteRefusesToRunTwiceAtOnce(t *testing.T) {
	card := cardDir(t, 1)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())
	routeAll(t, a, dir, "2026/portraits")

	s := importService(t, a)
	var inner error
	s.onProgress = func(p ImportProgress) {
		if p.Phase == ImportPhaseCopy && inner == nil {
			_, inner = s.Execute(dir, "")
		}
	}
	if _, err := s.Execute(dir, ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner == nil {
		t.Fatal("a second import started while the first was copying")
	}
}

func TestExecuteOfAnUnroutedFolderDoesNothing(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)

	batch, err := importService(t, a).Execute(dir, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(batch.Actions) != 0 {
		t.Errorf("an unrouted folder produced %d actions", len(batch.Actions))
	}
	entries, err := os.ReadDir(libraryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the library gained %d entries from an import of nothing", len(entries))
	}
}

func TestImportPlanReportsTheVolumeEachRouteLandsOn(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)
	routeAll(t, a, dir, "2026/portraits")

	s := importService(t, a,
		platform.Volume{Path: "/", Name: "Macintosh HD", Total: 100, Free: 10},
		platform.Volume{Path: libraryRoot, Name: "Photos", Total: 2000, Free: 900},
	)
	plan, err := s.ImportPlan(dir)
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	got := plan.Space
	if len(got) != 1 {
		t.Fatalf("space = %+v, want one destination", got)
	}
	// The longest matching mount point wins, not the first: everything is
	// under "/".
	if got[0].VolumeName != "Photos" || got[0].Free != 900 || got[0].Total != 2000 {
		t.Errorf("volume = %q %d/%d, want Photos 900/2000", got[0].VolumeName, got[0].Free, got[0].Total)
	}
	if got[0].Frames != 2 || got[0].Bytes <= 0 {
		t.Errorf("landing = %d frames / %d bytes, want both counted", got[0].Frames, got[0].Bytes)
	}
	if got[0].Fits != true {
		t.Error("900 bytes free is enough for a handful of test frames")
	}
}

func TestImportPlanFlagsAVolumeThatCannotHoldIt(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	libraryRoot := t.TempDir()
	a := importApp(t, libraryRoot)
	routeAll(t, a, dir, "2026/portraits")

	s := importService(t, a, platform.Volume{Path: libraryRoot, Name: "Tiny", Total: 10, Free: 1})
	plan, err := s.ImportPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Space) != 1 || plan.Space[0].Fits {
		t.Errorf("space = %+v, want the destination reported as too small", plan.Space)
	}
}

func TestImportPlanDoesNotReportProgress(t *testing.T) {
	card := cardDir(t, 2)
	dir := imageDir(card, 0)
	a := importApp(t, t.TempDir())
	routeAll(t, a, dir, "2026/portraits")

	s := importService(t, a)
	var reports []ImportProgress
	s.onProgress = func(p ImportProgress) { reports = append(reports, p) }

	if _, err := s.ImportPlan(dir); err != nil {
		t.Fatal(err)
	}
	// Drawing the routing table is a read. A bar that fills every time the user
	// looks at a folder is a bar that means nothing when files are moving.
	if len(reports) != 0 {
		t.Errorf("reading the plan reported %d times: %+v", len(reports), reports)
	}
}

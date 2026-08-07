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

// cullRules is the configuration of a plan that only culls: no destination in
// any of its frames, so the library root and the import verb never come up.
func cullRules(cut config.CutScope) rules {
	return rules{cut: cut, libraryRoot: "/library"}
}

func importRules(move bool) rules {
	return rules{cut: config.CutRemovesBoth, libraryRoot: "/library", moveOnImport: move}
}

func routedItem(stem, dest string, mask decide.Mask) planned {
	return planned{
		group:  pairedGroup(stem),
		hash:   "hash-" + stem,
		record: decide.Record{Verdict: decide.Keep, Mask: mask, Destination: dest},
	}
}

func TestBuildPlanCopiesRoutedFrames(t *testing.T) {
	items := []planned{
		routedItem("DSCF0001", "/library/portraits", decide.MaskBoth),
		routedItem("DSCF0002", "/library/portraits", decide.MaskBoth),
	}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}

	// Two frames, three files each, and nothing trashed: an import reads the
	// card and leaves it as it found it.
	if len(p.actions) != 6 {
		t.Fatalf("planned %d actions, want 6: %v", len(p.actions), p.actions)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbCopy {
			t.Errorf("an import plans copies, got %q on %s", a.Verb, a.Src)
		}
	}
	if p.actions[0].Dst != "/library/portraits/DSCF0001.RAF" {
		t.Errorf("first copy lands at %q", p.actions[0].Dst)
	}
	if len(p.dto.Destinations) != 1 {
		t.Fatalf("summary holds %d destinations, want 1", len(p.dto.Destinations))
	}
	got := p.dto.Destinations[0]
	if got.Path != "/library/portraits" || got.Frames != 2 || got.Files != 6 || got.Verb != "copy" {
		t.Errorf("destination summary %+v", got)
	}
	if want := int64(2 * (30_000_000 + 6_000_000 + 2_000)); got.Bytes != want {
		t.Errorf("destination bytes %d, want %d", got.Bytes, want)
	}
	if !strings.Contains(p.dto.Description, "Copy to /library/portraits (2 frames)") {
		t.Errorf("description %q", p.dto.Description)
	}
}

func TestBuildPlanGroupsCountsByDestination(t *testing.T) {
	items := []planned{
		routedItem("DSCF0001", "/library/b", decide.MaskBoth),
		routedItem("DSCF0002", "/library/a", decide.MaskBoth),
		routedItem("DSCF0003", "/library/a", decide.MaskBoth),
		{group: pairedGroup("DSCF0004"), hash: "h4", record: decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}},
	}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.dto.Destinations) != 2 {
		t.Fatalf("want two destinations, got %+v", p.dto.Destinations)
	}
	// Sorted, so the same frames always produce the same plan.
	if p.dto.Destinations[0].Path != "/library/a" || p.dto.Destinations[0].Frames != 2 {
		t.Errorf("first destination %+v", p.dto.Destinations[0])
	}
	if p.dto.Destinations[1].Path != "/library/b" || p.dto.Destinations[1].Frames != 1 {
		t.Errorf("second destination %+v", p.dto.Destinations[1])
	}
	// The cut is still a cut; routing some frames changes nothing about it.
	if p.dto.Counts["drop_all"] != 1 {
		t.Errorf("counts %v", p.dto.Counts)
	}
}

func TestARoutedFrameLeavesTheCardAlone(t *testing.T) {
	// A mask on a routed frame chooses which halves are imported. It must not
	// also trash the other half: the card is the source, and the same card has
	// to be importable twice.
	items := []planned{routedItem("DSCF0001", "/library/raws", decide.MaskRAW)}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.actions {
		if a.Verb == ops.VerbTrash {
			t.Errorf("an import trashed %s", a.Src)
		}
	}
	want := []string{"/library/raws/DSCF0001.RAF", "/library/raws/DSCF0001.RAF.xmp"}
	if len(p.actions) != len(want) {
		t.Fatalf("planned %d actions, want %d: %+v", len(p.actions), len(want), p.actions)
	}
	for i, w := range want {
		if p.actions[i].Dst != w {
			t.Errorf("action %d lands at %q, want %q", i, p.actions[i].Dst, w)
		}
	}
}

func TestBuildPlanMovesOnImportWhenAsked(t *testing.T) {
	items := []planned{routedItem("DSCF0001", "/library/portraits", decide.MaskBoth)}
	p, err := buildPlan(items, importRules(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbMove {
			t.Errorf("verb %q with moveOnImport set", a.Verb)
		}
	}
	if p.dto.Destinations[0].Verb != "move" {
		t.Errorf("summary verb %q", p.dto.Destinations[0].Verb)
	}
	if !strings.Contains(p.dto.Description, "Move to") {
		t.Errorf("description %q", p.dto.Description)
	}
}

func TestBuildPlanExpandsTokensPerFrame(t *testing.T) {
	first := routedItem("DSCF0001", "{date:2006-01-02}/keepers", decide.MaskBoth)
	second := routedItem("DSCF0002", "{date:2006-01-02}/keepers", decide.MaskBoth)
	second.group.Shot = second.group.Shot.AddDate(0, 0, 1)

	p, err := buildPlan([]planned{first, second}, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	// One destination in the summary, because one template is one instruction,
	// and two folders on disk, because it expands per frame.
	if len(p.dto.Destinations) != 1 || p.dto.Destinations[0].Frames != 2 {
		t.Fatalf("summary %+v", p.dto.Destinations)
	}
	dirs := map[string]bool{}
	for _, a := range p.actions {
		dirs[filepath.Dir(a.Dst)] = true
	}
	for _, want := range []string{"/library/2026-05-01/keepers", "/library/2026-05-02/keepers"} {
		if !dirs[want] {
			t.Errorf("nothing landed in %s: %v", want, dirs)
		}
	}
}

func TestARelativeDestinationHangsOffTheLibraryRoot(t *testing.T) {
	items := []planned{routedItem("DSCF0001", "2026/portraits", decide.MaskBoth)}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	if p.actions[0].Dst != "/library/2026/portraits/DSCF0001.RAF" {
		t.Errorf("landed at %q", p.actions[0].Dst)
	}
}

func TestAnAbsoluteDestinationIgnoresTheLibraryRoot(t *testing.T) {
	items := []planned{routedItem("DSCF0001", "/Volumes/Backup/2026", decide.MaskBoth)}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	if p.actions[0].Dst != "/Volumes/Backup/2026/DSCF0001.RAF" {
		t.Errorf("landed at %q", p.actions[0].Dst)
	}
}

func TestACutWithADestinationIsStillACut(t *testing.T) {
	// The store will not create this, but a hand-edited database could, and
	// importing a frame the user asked to delete is the worse mistake.
	items := []planned{{
		group:  pairedGroup("DSCF0001"),
		hash:   "h1",
		record: decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth, Destination: "/library/portraits"},
	}}
	p, err := buildPlan(items, importRules(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.dto.Destinations) != 0 {
		t.Errorf("a cut was routed: %+v", p.dto.Destinations)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbTrash {
			t.Errorf("verb %q on a cut frame", a.Verb)
		}
	}
}

func TestPlanRulesResolvesTheLibraryRoot(t *testing.T) {
	cfg := config.Default()
	cfg.Behaviour.LibraryRoot = "~/Pictures"
	r, err := planRules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if r.libraryRoot != filepath.Join(home, "Pictures") {
		t.Errorf("library root resolved to %q", r.libraryRoot)
	}
}

/* ---- end to end, on real files ---- */

// importTree lays out a card and a library and returns both.
func importTree(t *testing.T) (card, library string) {
	t.Helper()
	card = t.TempDir()
	library = filepath.Join(t.TempDir(), "library")
	for name, body := range map[string]string{
		"DSCF0001.RAF":     strings.Repeat("raw bytes", 2000),
		"DSCF0001.JPG":     "jpeg bytes",
		"DSCF0001.RAF.xmp": "<xmp/>",
	} {
		if err := os.WriteFile(filepath.Join(card, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return card, library
}

func TestApplyImportsAndLeavesTheCardIntact(t *testing.T) {
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
	if err := store.SetDestination(groups[0].Hash, card, groups[0].Stem, "keepers"); err != nil {
		t.Fatal(err)
	}

	batch, err := NewApplyService(a).Apply(card, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range batch.Actions {
		if action.Outcome != journal.OutcomeOK {
			t.Fatalf("%s %s: %s", action.Verb, action.Src, action.Err)
		}
	}

	for _, name := range []string{"DSCF0001.RAF", "DSCF0001.JPG", "DSCF0001.RAF.xmp"} {
		if _, err := os.Lstat(filepath.Join(card, name)); err != nil {
			t.Errorf("the card lost %s: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(library, "keepers", name)); err != nil {
			t.Errorf("the library never got %s: %v", name, err)
		}
	}

	// The copy consumes the destination; the keep is a judgement and stays.
	if r, ok, err := store.Get(groups[0].Hash, groups[0].Dir, groups[0].Stem); err != nil {
		t.Fatal(err)
	} else if !ok || r.Verdict != decide.Keep {
		t.Errorf("a keep must survive its import: %v %+v", ok, r)
	} else if r.Destination != "" {
		t.Errorf("the destination must be consumed by the copy, got %q", r.Destination)
	}
}

func TestApplyKeepsTheDecisionWhenACopyFails(t *testing.T) {
	card, library := importTree(t)

	cfg := config.Default()
	// A library root that is an existing file, so every copy into it fails
	// without anything being corrupted to arrange it.
	blocked := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Behaviour.LibraryRoot = blocked
	_ = library

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
	if err := store.SetDestination(groups[0].Hash, card, groups[0].Stem, "keepers"); err != nil {
		t.Fatal(err)
	}

	batch, err := NewApplyService(a).Apply(card, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, action := range batch.Actions {
		if action.Outcome != journal.OutcomeOK {
			failed++
		}
	}
	if failed == 0 {
		t.Fatal("copies into a path blocked by a file were reported as done")
	}
	r, ok, err := store.Get(groups[0].Hash, groups[0].Dir, groups[0].Stem)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || r.Destination != "keepers" {
		t.Errorf("a frame that did not import must keep its decision for a retry, got %+v (ok=%v)", r, ok)
	}
}

// scanCard opens a folder through the library service, which is how the apply
// service will see the same frames and hashes.
func scanCard(t *testing.T, a *App, dir string) ([]GroupDTO, error) {
	t.Helper()
	folder, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		return nil, err
	}
	return folder.Groups, nil
}

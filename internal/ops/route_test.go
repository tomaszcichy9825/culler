package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

/* ---- template expansion ---- */

func shotTokens() Tokens {
	return Tokens{
		Shot:   time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC),
		Stem:   "DSCF0001",
		Ext:    "raf",
		Camera: "X-T5",
		Lens:   "XF23mmF1.4 R",
	}
}

func TestExpandTemplateFillsEveryToken(t *testing.T) {
	cases := []struct{ template, want string }{
		{"/library/{date:2006}", "/library/2026"},
		{"/library/{date:2006-01-02}", "/library/2026-05-17"},
		{"/library/{date:2006}/{date:2006-01-02}", "/library/2026/2026-05-17"},
		{"/library/{camera}", "/library/X-T5"},
		{"/library/{camera}/{lens}", "/library/X-T5/XF23mmF1.4 R"},
		{"/library/{stem}", "/library/DSCF0001"},
		{"/library/by-type/{ext}", "/library/by-type/raf"},
		{"/library/plain", "/library/plain"},
		// A layout with separators in it is the user asking for nested folders.
		{"/library/{date:2006/01/02}", "/library/2026/05/17"},
		// Tokens sit inside a segment as happily as they own one.
		{"/library/shoot-{date:2006}-{camera}", "/library/shoot-2026-X-T5"},
	}
	for _, c := range cases {
		got, err := ExpandTemplate(c.template, shotTokens())
		if err != nil {
			t.Errorf("%s: %v", c.template, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s expanded to %q, want %q", c.template, got, c.want)
		}
	}
}

func TestExpandTemplateCollapsesWhatItCannotAnswer(t *testing.T) {
	bare := Tokens{Shot: shotTokens().Shot, Stem: "DSCF0001", Ext: "jpg"}
	cases := []struct {
		name, template string
		tokens         Tokens
		want           string
	}{
		{"no camera", "/library/{camera}/{date:2006}", bare, "/library/2026"},
		{"no lens", "/library/{date:2006}/{lens}", bare, "/library/2026"},
		{"the whole segment goes", "/library/shot-on-{camera}/{stem}", bare, "/library/DSCF0001"},
		{"a token nothing answers", "/library/{shoot}/{stem}", bare, "/library/DSCF0001"},
		{"no shot time", "/library/{date:2006}/{stem}", Tokens{Stem: "DSCF0001"}, "/library/DSCF0001"},
		{"everything collapses", "/library/{camera}", bare, "/library"},
	}
	for _, c := range cases {
		got, err := ExpandTemplate(c.template, c.tokens)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: %q expanded to %q, want %q", c.name, c.template, got, c.want)
		}
		if strings.ContainsAny(got, "{}") {
			t.Errorf("%s: braces reached the path: %q", c.name, got)
		}
	}
}

func TestExpandTemplateKeepsPathsWhole(t *testing.T) {
	tokens := shotTokens()
	// A separator inside a value from the camera would silently create a
	// folder level the user never asked for.
	tokens.Camera = "Nikon/Z8"
	got, err := ExpandTemplate("/library/{camera}/{stem}", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/library/Nikon-Z8/DSCF0001" {
		t.Errorf("a separator in a metadata value must not split the path: %q", got)
	}

	tokens.Camera = ".."
	got, err = ExpandTemplate("/library/{camera}/{stem}", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/library/DSCF0001" {
		t.Errorf("a value that climbs out of the destination must be dropped: %q", got)
	}
}

func TestExpandTemplateRejectsAnUnclosedToken(t *testing.T) {
	if _, err := ExpandTemplate("/library/{date:2006", shotTokens()); err == nil {
		t.Error("an unclosed token would put a brace in a real path")
	}
}

func TestExpandTemplateTreatsBackslashesAsSeparators(t *testing.T) {
	bare := Tokens{Stem: "DSCF0001"}
	// A backslash-joined template must split into the same segments it would
	// with forward slashes: a dead token takes its own segment, never the
	// whole template.
	got, err := ExpandTemplate(`/library\shot-on-{camera}\{stem}`, bare)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/library/DSCF0001" {
		t.Errorf("backslash-joined template expanded to %q, want /library/DSCF0001", got)
	}
}

// A destination whose expansion collapses to nothing, or never was absolute,
// must fail the frame's routing rather than plan an action whose Dst lands
// wherever the process happens to be running from.
func TestRouteRefusesADestinationThatIsNotAbsolute(t *testing.T) {
	cases := []struct{ name, dest string }{
		// No Metadata func, so {camera} is unanswerable and the whole
		// expansion collapses to the empty string.
		{"collapses to nothing", "{camera}"},
		{"backslash-joined and unanswerable", `C:\photos\{camera}`},
		{"relative after expansion", "keepers/{date:2006}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actions, err := CopyTo{Dest: c.dest, Halves: HalvesBoth}.Plan([]scan.PhotoGroup{datedGroup("/card")})
			if err == nil {
				t.Fatalf("planned %v instead of refusing a destination that is not absolute", destPaths(actions))
			}
		})
	}
}

func TestExpandTemplateKeepsRelativeTemplatesRelative(t *testing.T) {
	got, err := ExpandTemplate("{date:2006}/keepers", shotTokens())
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026/keepers" {
		t.Errorf("got %q, want 2026/keepers", got)
	}
}

/* ---- planning ---- */

func destPaths(actions []FileAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.Dst)
	}
	return out
}

func datedGroup(dir string) scan.PhotoGroup {
	g := pairedGroup(dir)
	g.Shot = time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC)
	return g
}

func TestCopyToPlansTheSurvivingHalves(t *testing.T) {
	g := datedGroup("/card")

	cases := []struct {
		halves Halves
		want   []string
	}{
		{HalvesBoth, []string{
			"/library/2026/DSCF0001.RAF",
			"/library/2026/DSCF0001.JPG",
			"/library/2026/DSCF0001.RAF.xmp",
		}},
		{HalvesRAW, []string{
			"/library/2026/DSCF0001.RAF",
			"/library/2026/DSCF0001.RAF.xmp",
		}},
		// Sidecars belong to the RAW, so a JPEG-only import leaves them.
		{HalvesJPEG, []string{"/library/2026/DSCF0001.JPG"}},
	}
	for _, c := range cases {
		actions, err := CopyTo{Dest: "/library/{date:2006}", Halves: c.halves}.Plan([]scan.PhotoGroup{g})
		if err != nil {
			t.Fatalf("%s: %v", c.halves, err)
		}
		got := destPaths(actions)
		if len(got) != len(c.want) {
			t.Fatalf("%s planned %v, want %v", c.halves, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s planned %v, want %v", c.halves, got, c.want)
				break
			}
		}
		for _, a := range actions {
			if a.Verb != VerbCopy {
				t.Errorf("CopyTo planned a %s", a.Verb)
			}
		}
	}
}

func TestCopyToKeepsTheSourcePaths(t *testing.T) {
	g := datedGroup("/card")
	actions, err := CopyTo{Dest: "/library", Halves: HalvesBoth}.Plan([]scan.PhotoGroup{g})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Fatalf("planned %d actions, want 3", len(actions))
	}
	if actions[0].Src != filepath.Join("/card", "DSCF0001.RAF") {
		t.Errorf("source path lost: %q", actions[0].Src)
	}
}

func TestCopyToTakesSidecarsWithAJPEGOnlyFrame(t *testing.T) {
	g := jpegOnlyGroup("/card")
	g.Sidecars = []scan.FileRef{{Path: "/card/IMG_0002.xmp"}}

	actions, err := CopyTo{Dest: "/library", Halves: HalvesBoth}.Plan([]scan.PhotoGroup{g})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/library/IMG_0002.JPG", "/library/IMG_0002.xmp"}
	got := destPaths(actions)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("a frame with no RAW still takes its sidecar: got %v, want %v", got, want)
	}
}

func TestMoveToPlansMoves(t *testing.T) {
	actions, err := MoveTo{Dest: "/library", Halves: HalvesBoth}.Plan([]scan.PhotoGroup{datedGroup("/card")})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a.Verb != VerbMove {
			t.Errorf("MoveTo planned a %s", a.Verb)
		}
	}
}

func TestRoutingAsksForMetadataOnlyOncePerFrame(t *testing.T) {
	calls := 0
	op := CopyTo{
		Dest:   "/library/{camera}",
		Halves: HalvesBoth,
		Metadata: func(scan.PhotoGroup) (string, string) {
			calls++
			return "X-T5", "XF23mmF1.4 R"
		},
	}
	actions, err := op.Plan([]scan.PhotoGroup{datedGroup("/card")})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("read metadata %d times for one frame, want 1", calls)
	}
	if actions[0].Dst != "/library/X-T5/DSCF0001.RAF" {
		t.Errorf("planned %q", actions[0].Dst)
	}
}

func TestRoutingWithoutMetadataCollapsesTheSegment(t *testing.T) {
	actions, err := CopyTo{Dest: "/library/{camera}", Halves: HalvesBoth}.Plan([]scan.PhotoGroup{datedGroup("/card")})
	if err != nil {
		t.Fatal(err)
	}
	if actions[0].Dst != "/library/DSCF0001.RAF" {
		t.Errorf("planned %q, want the camera segment gone", actions[0].Dst)
	}
}

func TestRoutingRefusesAnEmptyDestination(t *testing.T) {
	if _, err := (CopyTo{Halves: HalvesBoth}).Plan([]scan.PhotoGroup{datedGroup("/card")}); err == nil {
		t.Error("a copy with nowhere to go must not plan")
	}
}

func TestRoutingDescribesItself(t *testing.T) {
	if got := (CopyTo{Dest: "/library"}).Describe(); got != "Copy to /library" {
		t.Errorf("CopyTo.Describe() = %q", got)
	}
	if got := (MoveTo{Dest: "/library"}).Describe(); got != "Move to /library" {
		t.Errorf("MoveTo.Describe() = %q", got)
	}
}

/* ---- collision policy ---- */

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCopyCollisionPolicies(t *testing.T) {
	cases := []struct {
		policy      config.CollisionPolicy
		wantOutcome string
		wantDst     string
		wantContent map[string]string
	}{
		{config.CollisionRenameSuffix, journal.OutcomeOK, "DSCF0001-2.RAF", map[string]string{
			"DSCF0001.RAF":   "already here",
			"DSCF0001-2.RAF": "fresh",
		}},
		{config.CollisionOverwrite, journal.OutcomeOK, "DSCF0001.RAF", map[string]string{
			"DSCF0001.RAF": "fresh",
		}},
		// A skip is the user's instruction carried out, not a failure — but
		// nothing moved, so it must not be journalled as done: a done that
		// never happened is what lets an apply clear a verdict whose files
		// are still exactly where they were.
		{config.CollisionSkip, journal.OutcomeSkipped, "", map[string]string{
			"DSCF0001.RAF": "already here",
		}},
	}
	for _, c := range cases {
		t.Run(string(c.policy), func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
			writeFile(t, src, "fresh")
			dstDir := t.TempDir()
			writeFile(t, filepath.Join(dstDir, "DSCF0001.RAF"), "already here")

			ex, _ := newExecutor(t)
			ex.Collision = c.policy
			batch, err := ex.Apply("import", []FileAction{
				{Verb: VerbCopy, Src: src, Dst: filepath.Join(dstDir, "DSCF0001.RAF")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if batch.Actions[0].Outcome != c.wantOutcome {
				t.Fatalf("outcome %s, want %s: %s", batch.Actions[0].Outcome, c.wantOutcome, batch.Actions[0].Err)
			}
			wantDst := ""
			if c.wantDst != "" {
				wantDst = filepath.Join(dstDir, c.wantDst)
			}
			if batch.Actions[0].Dst != wantDst {
				t.Errorf("journalled destination %q, want %q", batch.Actions[0].Dst, wantDst)
			}
			assertTree(t, dstDir, c.wantContent)
		})
	}
}

func TestUndoLeavesASkippedCopyAlone(t *testing.T) {
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, "fresh")
	dstDir := t.TempDir()
	writeFile(t, filepath.Join(dstDir, "DSCF0001.RAF"), "already here")

	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionSkip
	batch, err := ex.Apply("import", []FileAction{
		{Verb: VerbCopy, Src: src, Dst: filepath.Join(dstDir, "DSCF0001.RAF")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	assertTree(t, dstDir, map[string]string{"DSCF0001.RAF": "already here"})
}

// A move the collision policy skips leaves the source exactly where it was.
// Recording it as done would let the caller clear the frame's verdict and
// prune it from the catalogue while the photo still sits on the card.
func TestSkippedMoveIsJournalledAsSkippedNotDone(t *testing.T) {
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, "still on the card")
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "DSCF0001.RAF")
	writeFile(t, dst, "already here")

	ex, j := newExecutor(t)
	ex.Collision = config.CollisionSkip
	batch, err := ex.Apply("import", []FileAction{{Verb: VerbMove, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeSkipped {
		t.Fatalf("a move that never happened was journalled %q: %s", batch.Actions[0].Outcome, batch.Actions[0].Err)
	}
	if readFile(t, src) != "still on the card" {
		t.Fatal("the skipped move touched the source")
	}

	// Undo treats the skipped action as the no-op it is.
	if err := ex.Undo(batch); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if readFile(t, src) != "still on the card" || readFile(t, dst) != "already here" {
		t.Error("undo of a skipped action moved something")
	}
	if batches, _ := j.ReadAll(); len(batches) != 2 {
		t.Errorf("undo of an all-skipped batch should still journal: %d batches", len(batches))
	}
}

// An action that depends on the one before it must not run when that one was
// skipped by the collision policy: its premise did not happen any more than
// it did after a failure.
func TestNeedsPriorDoesNotRunAfterASkippedPredecessor(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "DSCF0001.JPG")
	writeFile(t, original, "original")
	staged := filepath.Join(t.TempDir(), "staged.JPG")
	writeFile(t, staged, "edited")
	backup := filepath.Join(t.TempDir(), "backup", "DSCF0001.JPG")
	writeFile(t, backup, "occupied")
	// The dependent action's own destination is free, so if the chain does
	// not stop it, it will land there and the test will see it.
	install := filepath.Join(dir, "DSCF0001-edited.JPG")

	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionSkip
	batch, err := ex.Apply("edit metadata", []FileAction{
		{Verb: VerbMove, Src: original, Dst: backup},
		{Verb: VerbCopy, Src: staged, Dst: install, NeedsPrior: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeSkipped {
		t.Fatalf("the backup move was supposed to be skipped: %+v", batch.Actions[0])
	}
	if batch.Actions[1].Outcome == journal.OutcomeOK {
		t.Fatalf("the dependent action ran on a state that does not exist: %+v", batch.Actions[1])
	}
	if _, err := os.Lstat(install); !os.IsNotExist(err) {
		t.Error("the dependent copy landed although the action it depends on never happened")
	}
	if readFile(t, original) != "original" {
		t.Errorf("the original was disturbed: %q", readFile(t, original))
	}
}

/* ---- verified copies ---- */

func TestVerifiedCopyPassesOnAGoodCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, strings.Repeat("photograph", 5000))
	dstDir := t.TempDir()

	ex, _ := newExecutor(t)
	ex.Verify = true
	batch, err := ex.Apply("import", []FileAction{
		{Verb: VerbCopy, Src: src, Dst: filepath.Join(dstDir, "DSCF0001.RAF")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeOK {
		t.Fatalf("a sound copy failed verification: %s", batch.Actions[0].Err)
	}
	if readFile(t, filepath.Join(dstDir, "DSCF0001.RAF")) != strings.Repeat("photograph", 5000) {
		t.Error("the copy is not the original")
	}
}

func TestVerifiedCopyCatchesACorruptedCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, strings.Repeat("photograph", 5000))
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "DSCF0001.RAF")

	ex, _ := newExecutor(t)
	ex.Verify = true
	// A drive that writes the right length and the wrong bytes is exactly what
	// verification is for, and exactly what no test can produce honestly.
	ex.Copier = func(src, dst string) error {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		b[len(b)/2] ^= 0xff
		return os.WriteFile(dst, b, 0o644)
	}
	batch, err := ex.Apply("import", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeError {
		t.Fatal("a corrupted copy was recorded as done")
	}
	if !strings.Contains(batch.Actions[0].Err, "verif") {
		t.Errorf("the journal should say what failed, got %q", batch.Actions[0].Err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("a copy known to be wrong must not be left at the destination")
	}
}

func TestVerificationOffAcceptsWhateverWasWritten(t *testing.T) {
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, "original")
	dst := filepath.Join(t.TempDir(), "DSCF0001.RAF")

	ex, _ := newExecutor(t)
	ex.Copier = func(_, dst string) error { return os.WriteFile(dst, []byte("wrong"), 0o644) }
	batch, err := ex.Apply("import", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeOK {
		t.Errorf("with verification off nothing is re-read: %s", batch.Actions[0].Err)
	}
}

func TestVerifiedCopyReadsTheWholeFile(t *testing.T) {
	// The identity hash covers the head of a file only, which would miss a
	// tail that never landed. Verification has to read further than that.
	const size = 200 << 10
	body := make([]byte, size)
	for i := range body {
		body[i] = byte(i)
	}
	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	writeFile(t, src, string(body))
	dst := filepath.Join(t.TempDir(), "DSCF0001.RAF")

	ex, _ := newExecutor(t)
	ex.Verify = true
	ex.Copier = func(src, dst string) error {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		b[size-1] ^= 0xff // the very last byte, far past the identity hash
		return os.WriteFile(dst, b, 0o644)
	}
	batch, err := ex.Apply("import", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != journal.OutcomeError {
		t.Error("a wrong byte at the end of the file went unnoticed")
	}
}

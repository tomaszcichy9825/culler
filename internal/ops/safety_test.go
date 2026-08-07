package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// read is a small helper for asserting file contents.
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// Overwrite policy must not permanently delete the file it displaces: the
// existing file goes to the trash, recoverable, like every other deletion.
func TestOverwriteTrashesTheDisplacedFile(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionOverwrite
	trashDir := t.TempDir()
	ex.Trasher = platform.DirTrasher{Dir: trashDir}

	dir := t.TempDir()
	src := filepath.Join(dir, "new.jpg")
	dst := filepath.Join(dir, "existing.jpg")
	if err := os.WriteFile(src, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("valuable original"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := ex.Apply("copy over", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "ok" {
		t.Fatalf("copy did not succeed: %+v", batch.Actions[0])
	}
	if read(t, dst) != "incoming" {
		t.Errorf("destination should hold the new content, got %q", read(t, dst))
	}
	// The displaced original must be somewhere recoverable, not gone.
	found := false
	_ = filepath.Walk(trashDir, func(p string, fi os.FileInfo, e error) error {
		if e == nil && !fi.IsDir() && read(t, p) == "valuable original" {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("BUG: the overwritten file was permanently deleted instead of trashed")
	}
}

// A copy whose destination resolves onto its own source must be refused, not
// carried out by deleting the source first.
func TestOverwriteRefusesToDestroyTheSource(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionOverwrite

	dir := t.TempDir()
	path := filepath.Join(dir, "only.raf")
	if err := os.WriteFile(path, []byte("the only copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := ex.Apply("copy onto self", []FileAction{{Verb: VerbCopy, Src: path, Dst: path}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome == "ok" {
		t.Error("a copy onto its own source should not report success")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("BUG: the source was destroyed by an overwrite onto itself: %v", err)
	}
	if read(t, path) != "the only copy" {
		t.Errorf("source content changed: %q", read(t, path))
	}
}

// Undoing a copy must remove only the copy we made, never a different file a
// user has since put at that path.
func TestUndoOfCopyRefusesToDeleteAReplacement(t *testing.T) {
	ex, _ := newExecutor(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "card.jpg")
	dst := filepath.Join(t.TempDir(), "library.jpg")
	if err := os.WriteFile(src, []byte("shot"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := ex.Apply("import", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "ok" {
		t.Fatalf("copy failed: %+v", batch.Actions[0])
	}

	// The user replaces the imported file with different content.
	if err := os.WriteFile(dst, []byte("a newer edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ex.Undo(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("BUG: undo deleted the replacement it never created: %v", err)
	}
	if read(t, dst) != "a newer edit" {
		t.Errorf("the replacement was altered: %q", read(t, dst))
	}
}

// A cross-filesystem move with verification on must not delete the source
// when the copy arrives corrupt: the original is the only copy left.
func TestVerifiedMoveKeepsSourceWhenCopyIsCorrupt(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Verify = true
	// A copier that writes wrong bytes forces the cross-fs fallback to fail
	// verification, standing in for a card reader dropping bytes.
	ex.Renamer = func(_, _ string) error { return errors.New("cross-device link") }
	ex.Copier = func(_, dst string) error {
		return os.WriteFile(dst, []byte("corrupt"), 0o644)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "card.raf")
	if err := os.WriteFile(src, []byte("the original photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "library.raf")

	batch, err := ex.Apply("move import", []FileAction{{Verb: VerbMove, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome == "ok" {
		t.Fatal("a move whose copy failed verification must not report success")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("BUG: verified move deleted the source after a corrupt copy: %v", err)
	}
	if read(t, src) != "the original photo" {
		t.Errorf("source content changed: %q", read(t, src))
	}
}

// Displacing a file is only half of overwrite's promise: undo must bring it
// back. The trash location has to travel through the journal for that, or the
// original is "recoverable" only to someone digging through the trash by hand.
func TestOverwriteUndoRestoresTheDisplacedFile(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionOverwrite
	ex.Trasher = platform.DirTrasher{Dir: t.TempDir()}

	dir := t.TempDir()
	src := filepath.Join(dir, "new.jpg")
	dst := filepath.Join(dir, "existing.jpg")
	if err := os.WriteFile(src, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("valuable original"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := ex.Apply("copy over", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Undo(batch); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if read(t, dst) != "valuable original" {
		t.Errorf("undo did not put the displaced file back, dst holds %q", read(t, dst))
	}
}

// A copy that fails after the overwrite policy has already trashed the
// destination must put the displaced file straight back: the user asked to
// replace it, not to lose it to a copy that never happened.
func TestFailedCopyAfterDisplacementPutsTheFileBack(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Collision = config.CollisionOverwrite
	ex.Trasher = platform.DirTrasher{Dir: t.TempDir()}
	ex.Copier = func(src, dst string) error { return errors.New("reader dropped off the bus") }

	dir := t.TempDir()
	src := filepath.Join(dir, "new.jpg")
	dst := filepath.Join(dir, "existing.jpg")
	if err := os.WriteFile(src, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("valuable original"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := ex.Apply("copy over", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "error" {
		t.Fatalf("the copy was supposed to fail: %+v", batch.Actions[0])
	}
	if read(t, dst) != "valuable original" {
		t.Errorf("the displaced file was not put back after the failed copy, dst holds %q", read(t, dst))
	}
}

// A cross-filesystem move whose source cannot be removed after the copy must
// not leave the copy behind untracked: the journal records an error and denies
// a destination, so a surviving copy would be invisible to undo and pile up a
// numbered duplicate on every retry.
func TestMoveThatCannotRemoveTheSourceLeavesNoUntrackedCopy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions do not bite")
	}
	ex, _ := newExecutor(t)
	// No rename, so the move takes the copy-then-remove road.
	ex.Renamer = func(src, dst string) error { return errors.New("cross-device") }

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "IMG_0001.JPG")
	if err := os.WriteFile(src, []byte("shot"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "IMG_0001.JPG")
	// The copy out of srcDir works; deleting src needs write on srcDir and fails.
	if err := os.Chmod(srcDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(srcDir, 0o755) })

	batch, err := ex.Apply("move", []FileAction{{Verb: VerbMove, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "error" {
		t.Fatalf("the move was supposed to fail: %+v", batch.Actions[0])
	}
	if read(t, src) != "shot" {
		t.Error("the source went missing during a failed move")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("a copy the journal denies exists was left at the destination")
	}
}

// Between the collision check and the rename, another writer can land a file
// on the same destination. The move must fail rather than silently swallow it:
// a rename overwrites, and the first file's bytes would be gone for good.
func TestMoveRefusesToClobberAFileThatAppearedLate(t *testing.T) {
	ex, _ := newExecutor(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "incoming.jpg")
	dst := filepath.Join(dir, "settled.jpg")
	if err := os.WriteFile(src, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The file that appeared after clearTarget's check would have run.
	if err := os.WriteFile(dst, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ex.move(src, dst); err == nil {
		t.Fatal("the move clobbered a file that appeared at the destination")
	}
	if read(t, dst) != "already here" {
		t.Errorf("the settled file was overwritten: %q", read(t, dst))
	}
	if read(t, src) != "incoming" {
		t.Errorf("the source was consumed by a refused move: %q", read(t, src))
	}
}

// Undo's move-back is the same only-copy-destroying road as a forward move,
// so it takes the same verified path: a corrupt copy back must never cost the
// one intact copy sitting in the trash.
func TestUndoMoveBackIsVerified(t *testing.T) {
	ex, j := newExecutor(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "IMG_0001.JPG")
	if err := os.WriteFile(src, []byte("the only copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := ex.Apply("drop", []FileAction{{Verb: VerbTrash, Src: src}})
	if err != nil {
		t.Fatal(err)
	}
	trashed := batch.Actions[0].Dst

	// The undo has to cross filesystems (rename refused) through a reader that
	// corrupts what it copies. Verification is on: the corrupt copy-back must
	// be noticed before the trash copy is removed.
	undoEx := &Executor{
		Journal: j,
		Verify:  true,
		Renamer: func(src, dst string) error { return errors.New("cross-device") },
		Copier: func(src, dst string) error {
			return os.WriteFile(dst, []byte("corrupted in transit"), 0o644)
		},
	}
	if _, err := undoEx.Undo(batch); err == nil {
		t.Fatal("an undo whose copy-back failed verification reported success")
	}
	if read(t, trashed) != "the only copy" {
		t.Fatal("BUG: the only intact copy was removed after a corrupt copy-back")
	}
	if _, err := os.Stat(src); err == nil && read(t, src) == "corrupted in transit" {
		t.Error("a corrupt copy was left at the restored path")
	}
}

// An undo that could not remove the copy for a real reason — permissions, a
// failing disk — must count as blocked, so the reversed-nothing guard fires
// and the batch stays retryable, instead of being silently marked undone.
func TestUndoCopyRemovalFailureBlocksTheBatch(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions do not bite")
	}
	ex, j := newExecutor(t)

	src := filepath.Join(t.TempDir(), "card.jpg")
	if err := os.WriteFile(src, []byte("shot"), 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "library.jpg")

	batch, err := ex.Apply("import", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	// The directory is readable but not writable, so the digest still matches
	// and only the removal itself fails.
	if err := os.Chmod(dstDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dstDir, 0o755) })

	if _, err := ex.Undo(batch); err == nil {
		t.Fatal("an undo that removed nothing reported success")
	}
	// Nothing was reversed, so nothing was journalled: the batch must still be
	// the undo target so it can be retried once the permissions are fixed.
	if batches, _ := j.ReadAll(); len(batches) != 1 {
		t.Fatalf("a fully-blocked undo consumed the batch: %d batches", len(batches))
	}
	if read(t, dst) != "shot" {
		t.Error("the copy went missing during a blocked undo")
	}
}

// When copy-undo protectively leaves a replacement alone, the file the
// overwrite displaced is still sitting in the trash. Undo must not report
// success over it: its path is occupied, which is a blocked restore, and the
// batch has to stay retryable.
func TestUndoCountsAStrandedDisplacedFileAsBlocked(t *testing.T) {
	ex, j := newExecutor(t)
	ex.Collision = config.CollisionOverwrite
	ex.Trasher = platform.DirTrasher{Dir: t.TempDir()}

	dir := t.TempDir()
	src := filepath.Join(dir, "new.jpg")
	dst := filepath.Join(dir, "existing.jpg")
	if err := os.WriteFile(src, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("valuable original"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := ex.Apply("copy over", []FileAction{{Verb: VerbCopy, Src: src, Dst: dst}})
	if err != nil {
		t.Fatal(err)
	}
	// The user replaces our copy with an edit of their own. Undo must leave
	// the edit alone — and must not pretend the displaced original came back.
	if err := os.WriteFile(dst, []byte("a newer edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ex.Undo(batch); err == nil {
		t.Fatal("undo reported success while the displaced original is stranded in the trash")
	}
	if read(t, dst) != "a newer edit" {
		t.Errorf("the replacement was altered: %q", read(t, dst))
	}
	if batches, _ := j.ReadAll(); len(batches) != 1 {
		t.Fatalf("a fully-blocked undo consumed the batch: %d batches", len(batches))
	}
}

// The mirror case: the protective skip found nothing at the destination at
// all — the user deleted our unverifiable copy — so the displaced file's old
// path is free, and undo must put it back rather than leave it in the trash.
func TestUndoRestoresTheDisplacedFileWhenTheCopyIsGone(t *testing.T) {
	ex, _ := newExecutor(t)

	dir := t.TempDir()
	dst := filepath.Join(dir, "existing.jpg")
	trash := t.TempDir()
	displaced := filepath.Join(trash, "existing.jpg")
	if err := os.WriteFile(displaced, []byte("valuable original"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hand-written batch: the copy's digest was never recorded and its
	// destination no longer exists, while the displaced original waits in
	// the trash.
	batch := journal.Batch{
		ID:          "b1",
		Description: "copy over",
		Actions: []journal.Action{{
			Verb:      string(VerbCopy),
			Src:       "/card/new.jpg",
			Dst:       dst,
			Digest:    digestUnreadable,
			Displaced: displaced,
			Outcome:   journal.OutcomeOK,
		}},
	}

	if _, err := ex.Undo(batch); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if read(t, dst) != "valuable original" {
		t.Errorf("the displaced file was not restored, dst holds %q", read(t, dst))
	}
	if _, err := os.Lstat(displaced); !os.IsNotExist(err) {
		t.Error("the displaced file is still in the trash after its restore")
	}
}

// Trashing through an executor that was never given a trash must fail the
// action cleanly, like overwrite already does, not crash the whole apply.
func TestTrashWithoutATrasherFailsTheActionNotTheApply(t *testing.T) {
	ex, _ := newExecutor(t)
	ex.Trasher = nil

	src := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	if err := os.WriteFile(src, []byte("shot"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := ex.Apply("drop", []FileAction{{Verb: VerbTrash, Src: src}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "error" || batch.Actions[0].Err == "" {
		t.Fatalf("a trash with no trash must be recorded as an error: %+v", batch.Actions[0])
	}
	if read(t, src) != "shot" {
		t.Error("the file went somewhere although there was no trash to take it")
	}
}

// A copy whose digest could never be read must not fall back to the legacy
// remove-it-anyway undo: with no record of what we wrote, deleting whatever is
// there now is exactly the replacement-deleting mistake digests exist to stop.
func TestRemoveIfOurCopyRefusesAnUnrecordedDigest(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "library.jpg")
	if err := os.WriteFile(dst, []byte("who knows whose"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeIfOurCopy(dst, digestUnreadable); err == nil {
		t.Fatal("an unreadable digest allowed an unconditional delete")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal("the file was deleted on the strength of no digest at all")
	}
}

// An action marked NeedsPrior only makes sense after the one before it
// succeeded. The metadata writer pairs "move the original to backup" with
// "install the edited copy": installing without the backup would put the
// edited file beside an original that never moved — on the card, under a
// numbered name — so the install must be skipped, not attempted.
func TestNeedsPriorSkipsAfterAFailedPredecessor(t *testing.T) {
	ex, _ := newExecutor(t)

	dir := t.TempDir()
	original := filepath.Join(dir, "DSCF0001.JPG")
	staged := filepath.Join(t.TempDir(), "staged.JPG")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The backup move fails: its destination's parent is a file, so nothing
	// can be created under it.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := ex.Apply("edit metadata", []FileAction{
		{Verb: VerbMove, Src: original, Dst: filepath.Join(blocked, "DSCF0001.JPG")},
		{Verb: VerbCopy, Src: staged, Dst: original, NeedsPrior: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Actions[0].Outcome != "error" {
		t.Fatalf("the backup move was supposed to fail: %+v", batch.Actions[0])
	}
	if batch.Actions[1].Outcome != "error" {
		t.Fatalf("the install must be skipped, not run: %+v", batch.Actions[1])
	}
	if read(t, original) != "original" {
		t.Errorf("the original was disturbed: %q", read(t, original))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a stray file landed beside the original: %v", names)
	}
}

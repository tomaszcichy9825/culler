package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
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

	if err := ex.Undo(batch); err != nil {
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

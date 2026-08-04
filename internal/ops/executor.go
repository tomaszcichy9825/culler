package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// Executor applies planned actions, journals every one with its real
// destination and outcome, and replays batches backwards for undo.
type Executor struct {
	Journal *journal.Journal
	Trasher platform.Trasher

	// Collision decides what happens when a copy or move lands on a file that
	// is already there. The zero value renames, which is both the config
	// default and the only option that cannot lose anything.
	Collision config.CollisionPolicy

	// Verify re-reads every copied file and compares it with the original
	// before the copy is recorded as done. It doubles the reads, which is why
	// it is a setting, and it is the only thing standing between a card reader
	// that drops bytes and a library that quietly holds a broken RAW.
	Verify bool

	// Copier writes one file to a path nothing occupies. nil means
	// platform.CopyFile; a test replaces it to produce the corrupted copy
	// verification exists to catch, which is not something a test can
	// otherwise bring about honestly.
	Copier func(src, dst string) error

	// Renamer relocates a file within a filesystem. nil means os.Rename; a
	// test forces it to fail to exercise the cross-filesystem copy fallback,
	// which is otherwise unreachable without a second volume.
	Renamer func(src, dst string) error
}

var batchCounter atomic.Uint64

// newBatchID is unique across concurrent applies. The counter is the identity
// undo keys on, so two batches must never share one: a monotonic atomic
// bump makes the id distinct even when two applies land in the same
// nanosecond.
func newBatchID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), batchCounter.Add(1))
}

// Apply executes actions in order. A failed action is recorded in the batch
// and execution continues — partial completion is a journaled fact, not an
// exception. The returned error covers only journal write failures.
func (e *Executor) Apply(description string, actions []FileAction) (journal.Batch, error) {
	batch := journal.Batch{
		ID:          newBatchID(),
		Time:        time.Now(),
		Description: description,
	}
	for _, a := range actions {
		rec := journal.Action{Verb: string(a.Verb), Src: a.Src}
		dst, err := e.execute(a)
		rec.Dst = dst
		if err != nil {
			rec.Outcome = journal.OutcomeError
			rec.Err = err.Error()
		} else {
			rec.Outcome = journal.OutcomeOK
			// A copy records the digest of what it wrote, so its undo can be
			// sure it is deleting the copy it made and not a later replacement.
			if a.Verb == VerbCopy && dst != "" {
				if d, derr := contentDigest(dst); derr == nil {
					rec.Digest = d
				}
			}
		}
		batch.Actions = append(batch.Actions, rec)
	}
	if err := e.Journal.Append(batch); err != nil {
		return batch, err
	}
	return batch, nil
}

// execute performs one action and returns where the file ended up. An empty
// destination on a copy or a move means the collision policy skipped it: the
// user's instruction carried out, with nothing of ours at the destination for
// undo to take back.
func (e *Executor) execute(a FileAction) (string, error) {
	switch a.Verb {
	case VerbTrash:
		return e.Trasher.Trash(a.Src)
	case VerbMove:
		dst, ok, err := e.clearTarget(a.Src, a.Dst)
		if err != nil || !ok {
			return "", err
		}
		if err := e.move(a.Src, dst); err != nil {
			return "", err
		}
		return dst, nil
	case VerbCopy:
		dst, ok, err := e.clearTarget(a.Src, a.Dst)
		if err != nil || !ok {
			return "", err
		}
		if err := e.copy(a.Src, dst); err != nil {
			return "", err
		}
		return dst, nil
	case VerbDestroy:
		// Permanent deletion never routes through here. It belongs to the one
		// command sanctioned to do it, which journals the verb itself; letting
		// an op plan a destroy would put unrecoverable deletion one typo away
		// from every apply.
		return "", errors.New("destroy is not an executable verb: only the empty-rejects command deletes permanently")
	}
	return "", fmt.Errorf("unknown verb %q", a.Verb)
}

// clearTarget applies the collision policy and returns the path to write to.
// It reports false when the policy says to leave the existing file alone. The
// destination's parent is created either way: routing a frame into a dated
// folder is an instruction to make the folder. src is the file about to be
// written from, so an operation that would land on its own source is caught
// before anything is removed.
func (e *Executor) clearTarget(src, dst string) (string, bool, error) {
	if same, err := sameFile(src, dst); err != nil {
		return "", false, err
	} else if same {
		return "", false, fmt.Errorf("refusing to overwrite %s with itself", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", false, err
	}
	_, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return dst, true, nil
	}
	if err != nil {
		return "", false, err
	}
	switch e.Collision {
	case config.CollisionSkip:
		return "", false, nil
	case config.CollisionOverwrite:
		// The user asked to replace it, but replacing is not destroying: the
		// displaced file goes to the trash so it is still recoverable, like
		// every other deletion in the app.
		if e.Trasher == nil {
			return "", false, fmt.Errorf("overwrite needs a trash and has none")
		}
		if _, err := e.Trasher.Trash(dst); err != nil {
			return "", false, err
		}
		return dst, true, nil
	default:
		unique, err := platform.UniquePath(dst)
		return unique, err == nil, err
	}
}

// sameFile reports whether src and dst are the same file on disk, following
// the paths through any links. A destination that does not exist yet is not
// the source; that is the common case and not an error.
func sameFile(src, dst string) (bool, error) {
	si, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	di, err := os.Stat(dst)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(si, di), nil
}

// move relocates a file. A same-filesystem rename is atomic and needs no
// verification. Across filesystems a rename cannot happen, so MoveFile copies
// then deletes the source — and with verification on the copy is proven intact
// before the only other copy is removed, because this is the one path that can
// destroy an original.
func (e *Executor) move(src, dst string) error {
	rename := e.Renamer
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(src, dst); err == nil {
		return nil
	}
	if err := e.copy(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// copy writes one file and, when verification is on, proves it landed intact
// before the caller is allowed to call it done. A copy that fails to verify is
// removed: leaving it would mean a broken file in the library under a name
// that looks right, and the rename policy would pile another one beside it on
// every retry.
func (e *Executor) copy(src, dst string) error {
	write := e.Copier
	if write == nil {
		write = platform.CopyFile
	}
	if err := write(src, dst); err != nil {
		return err
	}
	if !e.Verify {
		return nil
	}
	if err := verifyCopy(src, dst); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// verifyCopy re-reads both files in full and compares them. The identity hash
// covers only the head of a file, which is the wrong tool here: a truncated or
// half-written tail is exactly the failure this is looking for.
func verifyCopy(src, dst string) error {
	want, err := contentDigest(src)
	if err != nil {
		return fmt.Errorf("verification read of %s: %w", src, err)
	}
	got, err := contentDigest(dst)
	if err != nil {
		return fmt.Errorf("verification read of %s: %w", dst, err)
	}
	if want != got {
		return fmt.Errorf("verification failed: %s does not match %s", dst, src)
	}
	return nil
}

// removeIfOurCopy deletes path only if it still holds the bytes the copy
// wrote, identified by the digest recorded at the time. A gone file is
// nothing to undo. An empty digest is a pre-digest journal, removed as the old
// undo did. A changed file is left alone with an error, so a replacement is
// never destroyed.
func removeIfOurCopy(path, digest string) error {
	if digest == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	got, err := contentDigest(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if got != digest {
		return fmt.Errorf("destination changed since it was copied: %s", path)
	}
	return os.Remove(path)
}

// contentDigest is a sha256 over every byte of a file.
func contentDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Undo reverses a batch: successful actions are replayed backwards in reverse
// order; failed ones are skipped. The undo is itself journaled as a batch
// with UndoOf set.
func (e *Executor) Undo(b journal.Batch) error {
	// A batch that destroyed files has nothing to replay: there is no
	// destination to move back from. It is refused whole rather than
	// half-undone, and nothing is journalled, so the record of the destruction
	// stays the last word on those files.
	for _, a := range b.Actions {
		if Verb(a.Verb) == VerbDestroy {
			return fmt.Errorf("%q permanently deleted its files and cannot be undone", b.Description)
		}
	}
	undo := journal.Batch{
		ID:          newBatchID(),
		Time:        time.Now(),
		Description: "Undo: " + b.Description,
		UndoOf:      b.ID,
	}
	for i := len(b.Actions) - 1; i >= 0; i-- {
		a := b.Actions[i]
		if a.Outcome != journal.OutcomeOK {
			continue
		}
		if a.Dst == "" && Verb(a.Verb) != VerbTrash {
			// The collision policy skipped this one. Nothing of ours is at the
			// destination, and the file that is there was never part of the
			// batch.
			continue
		}
		rec := journal.Action{Verb: a.Verb, Src: a.Dst, Dst: a.Src}
		var err error
		switch Verb(a.Verb) {
		case VerbTrash, VerbMove:
			// bring it back where it came from — unless something new now
			// lives there; never silently overwrite
			if _, statErr := os.Lstat(a.Src); statErr == nil {
				err = fmt.Errorf("destination occupied: %s", a.Src)
			} else {
				err = platform.MoveFile(a.Dst, a.Src)
			}
		case VerbCopy:
			// Remove only the copy we made. The digest recorded when it was
			// written says what "the copy we made" is; if the file there no
			// longer matches, the user has replaced or edited it and undo
			// leaves it alone rather than deleting work it never created. A
			// journal written before digests were recorded keeps the old
			// behaviour of removing unconditionally.
			err = removeIfOurCopy(a.Dst, a.Digest)
			rec.Dst = ""
		default:
			err = fmt.Errorf("unknown verb %q", a.Verb)
		}
		if err != nil {
			rec.Outcome = journal.OutcomeError
			rec.Err = err.Error()
		} else {
			rec.Outcome = journal.OutcomeOK
		}
		undo.Actions = append(undo.Actions, rec)
	}
	return e.Journal.Append(undo)
}

package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
}

var batchCounter uint64

func newBatchID() string {
	batchCounter++
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), batchCounter)
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
		dst, ok, err := e.clearTarget(a.Dst)
		if err != nil || !ok {
			return "", err
		}
		if err := platform.MoveFile(a.Src, dst); err != nil {
			return "", err
		}
		return dst, nil
	case VerbCopy:
		dst, ok, err := e.clearTarget(a.Dst)
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
// folder is an instruction to make the folder.
func (e *Executor) clearTarget(dst string) (string, bool, error) {
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
		// The user asked for this explicitly; nothing else in the app removes
		// a file the user has not pointed at.
		if err := os.Remove(dst); err != nil {
			return "", false, err
		}
		return dst, true, nil
	default:
		unique, err := platform.UniquePath(dst)
		return unique, err == nil, err
	}
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
			// the copy we created is the only thing removed; the source
			// original is never touched
			err = os.Remove(a.Dst)
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

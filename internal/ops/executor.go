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
		dst, displaced, err := e.execute(a)
		rec.Dst = dst
		rec.Displaced = displaced
		if err != nil {
			rec.Outcome = journal.OutcomeError
			rec.Err = err.Error()
		} else {
			rec.Outcome = journal.OutcomeOK
			// A copy records the digest of what it wrote, so its undo can be
			// sure it is deleting the copy it made and not a later replacement.
			if a.Verb == VerbCopy && dst != "" {
				d, derr := contentDigest(dst)
				if derr != nil {
					// One transient hiccup — a share dropping a read, a
					// scanner holding the file — should not decide the copy's
					// fate, so the read is tried again.
					d, derr = contentDigest(dst)
				}
				if derr != nil {
					// No digest means undo cannot tell our copy from a later
					// replacement. The sentinel makes it refuse to delete
					// rather than fall back to deleting unconditionally.
					d = digestUnreadable
				}
				rec.Digest = d
			}
		}
		batch.Actions = append(batch.Actions, rec)
	}
	if err := e.Journal.Append(batch); err != nil {
		return batch, err
	}
	return batch, nil
}

// execute performs one action and returns where the file ended up, along with
// the trash location of anything the overwrite policy displaced to make room.
// An empty destination on a copy or a move means the collision policy skipped
// it: the user's instruction carried out, with nothing of ours at the
// destination for undo to take back.
func (e *Executor) execute(a FileAction) (string, string, error) {
	switch a.Verb {
	case VerbTrash:
		dst, err := e.Trasher.Trash(a.Src)
		return dst, "", err
	case VerbMove:
		dst, displaced, ok, err := e.clearTarget(a.Src, a.Dst)
		if err != nil || !ok {
			return "", "", err
		}
		if err := e.move(a.Src, dst); err != nil {
			return "", "", e.undisplace(displaced, dst, err)
		}
		return dst, displaced, nil
	case VerbCopy:
		dst, displaced, ok, err := e.clearTarget(a.Src, a.Dst)
		if err != nil || !ok {
			return "", "", err
		}
		if err := e.copy(a.Src, dst); err != nil {
			return "", "", e.undisplace(displaced, dst, err)
		}
		return dst, displaced, nil
	case VerbDestroy:
		// Permanent deletion never routes through here. It belongs to the one
		// command sanctioned to do it, which journals the verb itself; letting
		// an op plan a destroy would put unrecoverable deletion one typo away
		// from every apply.
		return "", "", errors.New("destroy is not an executable verb: only the empty-rejects command deletes permanently")
	}
	return "", "", fmt.Errorf("unknown verb %q", a.Verb)
}

// undisplace puts a displaced file straight back after the action that
// displaced it failed: the user asked to replace it, not to lose it to a copy
// that never happened. When even that fails, the error at least says where the
// file went, which beats a trash nobody was told about.
func (e *Executor) undisplace(displaced, dst string, cause error) error {
	if displaced == "" {
		return cause
	}
	if err := e.move(displaced, dst); err != nil {
		return fmt.Errorf("%w; the file it displaced is in the trash at %s", cause, displaced)
	}
	return cause
}

// clearTarget applies the collision policy and returns the path to write to,
// plus where any displaced file went so the journal can carry it. It reports
// false when the policy says to leave the existing file alone. The
// destination's parent is created either way: routing a frame into a dated
// folder is an instruction to make the folder. src is the file about to be
// written from, so an operation that would land on its own source is caught
// before anything is removed.
func (e *Executor) clearTarget(src, dst string) (string, string, bool, error) {
	if same, err := sameFile(src, dst); err != nil {
		return "", "", false, err
	} else if same {
		return "", "", false, fmt.Errorf("refusing to overwrite %s with itself", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", "", false, err
	}
	_, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return dst, "", true, nil
	}
	if err != nil {
		return "", "", false, err
	}
	switch e.Collision {
	case config.CollisionSkip:
		return "", "", false, nil
	case config.CollisionOverwrite:
		// The user asked to replace it, but replacing is not destroying: the
		// displaced file goes to the trash so it is still recoverable, like
		// every other deletion in the app — and its trash location travels up
		// into the journal, so undo can put it back.
		if e.Trasher == nil {
			return "", "", false, fmt.Errorf("overwrite needs a trash and has none")
		}
		displaced, err := e.Trasher.Trash(dst)
		if err != nil {
			return "", "", false, err
		}
		return dst, displaced, true, nil
	default:
		unique, err := platform.UniquePath(dst)
		return unique, "", err == nil, err
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
// verification. Across filesystems a rename cannot happen, so the move copies
// then deletes the source — and with verification on the copy is proven intact
// before the only other copy is removed, because this is the one path that can
// destroy an original. A source that cannot be removed after the copy takes
// the copy back with it: the journal is about to record an error and no
// destination, and a copy the journal denies exists would be invisible to
// undo and pile a numbered duplicate on every retry.
func (e *Executor) move(src, dst string) error {
	rename := e.Renamer
	if rename == nil {
		rename = noReplaceRename
	}
	if err := rename(src, dst); err == nil {
		return nil
	} else if os.IsExist(err) {
		// Something landed on the destination between the collision check and
		// now. A plain rename would swallow it silently; refusing is the only
		// answer that loses nothing.
		return fmt.Errorf("a file appeared at %s since the collision check", dst)
	}
	if err := e.copy(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dst)
		return fmt.Errorf("source not removable, so the moved copy was withdrawn: %w", err)
	}
	return nil
}

// noReplaceRename relocates within a filesystem without ever landing on an
// existing file. A hard link is the one primitive that fails on an occupied
// destination instead of overwriting it, which closes the gap between the
// collision check and the rename; where links are not supported the plain
// rename stands, which is the behaviour those filesystems always had.
func noReplaceRename(src, dst string) error {
	err := os.Link(src, dst)
	if err == nil {
		if rerr := os.Remove(src); rerr != nil {
			// Two names for one file. Withdraw ours: the source is intact and
			// nothing is lost.
			os.Remove(dst)
			return fmt.Errorf("source not removable after linking: %w", rerr)
		}
		return nil
	}
	if os.IsExist(err) {
		return err
	}
	return os.Rename(src, dst)
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

// digestUnreadable marks a copy whose destination could not be re-read when
// the digest was due. It is not a hash — no sha256 renders to it — so it can
// never match a file, and undo refuses to delete rather than guessing.
const digestUnreadable = "!unreadable"

// removeIfOurCopy deletes path only if it still holds the bytes the copy
// wrote, identified by the digest recorded at the time. A gone file is
// nothing to undo. An empty digest is a pre-digest journal, removed as the old
// undo did. A changed file is left alone with an error, so a replacement is
// never destroyed.
func removeIfOurCopy(path, digest string) error {
	if digest == digestUnreadable {
		// What was written was never fingerprinted, so there is no telling our
		// copy from a later replacement — and deleting a maybe-replacement is
		// the mistake digests exist to prevent.
		return fmt.Errorf("the copy's digest was never recorded; %s is left alone", path)
	}
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
	// reversed counts files actually put back or copies removed; blocked counts
	// only restores stopped because something new occupies the path, which is
	// the retry-worthy failure. A copy-undo that leaves a replacement alone is a
	// deliberate protective skip, not a failure, and does not block the batch.
	var reversed, blocked int
	var firstErr string
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
		restore := false
		switch Verb(a.Verb) {
		case VerbTrash, VerbMove:
			// Bring it back where it came from — unless something new now
			// lives there; never silently overwrite. The move back takes the
			// same verified road as a forward move: across filesystems it is
			// the one step that can destroy the only remaining copy.
			restore = true
			if _, statErr := os.Lstat(a.Src); statErr == nil {
				err = fmt.Errorf("destination occupied: %s", a.Src)
			} else {
				err = e.move(a.Dst, a.Src)
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
		// With the action itself taken back, the file it displaced goes back
		// where it was. This is a restore too: the displaced file is the one
		// the overwrite promised was still recoverable.
		if err == nil && a.Displaced != "" {
			restore = true
			if rerr := e.move(a.Displaced, a.Dst); rerr != nil {
				err = fmt.Errorf("the displaced file was not restored: %w", rerr)
			}
		}
		if err != nil {
			rec.Outcome = journal.OutcomeError
			rec.Err = err.Error()
			if restore {
				blocked++
				if firstErr == "" {
					firstErr = err.Error()
				}
			}
		} else {
			rec.Outcome = journal.OutcomeOK
			reversed++
		}
		undo.Actions = append(undo.Actions, rec)
	}

	// An undo that put nothing back yet was blocked from doing so must not be
	// recorded: journalling it with UndoOf set would mark the batch undone
	// though the files are still where the batch left them, so the next Undo
	// would reverse an older batch and this one could never be retried. Leave it
	// as the undo target and report that it did not happen.
	if reversed == 0 && blocked > 0 {
		return fmt.Errorf("undo of %q reversed nothing: %s", b.Description, firstErr)
	}
	if err := e.Journal.Append(undo); err != nil {
		return err
	}
	// A partial undo is a real, journalled fact — the batch is spent — but the
	// caller is told, because some files could not be put back.
	if blocked > 0 {
		return fmt.Errorf("undo of %q could not restore %d file(s); %s", b.Description, blocked, firstErr)
	}
	return nil
}

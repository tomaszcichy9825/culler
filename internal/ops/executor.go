package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// Executor applies planned actions, journals every one with its real
// destination and outcome, and replays batches backwards for undo.
type Executor struct {
	Journal *journal.Journal
	Trasher platform.Trasher
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

// execute performs one action and returns where the file ended up.
func (e *Executor) execute(a FileAction) (string, error) {
	switch a.Verb {
	case VerbTrash:
		return e.Trasher.Trash(a.Src)
	case VerbMove:
		dst, err := platform.UniquePath(a.Dst)
		if err != nil {
			return "", err
		}
		if err := platform.MoveFile(a.Src, dst); err != nil {
			return "", err
		}
		return dst, nil
	case VerbCopy:
		dst, err := platform.UniquePath(a.Dst)
		if err != nil {
			return "", err
		}
		if err := platform.CopyFile(a.Src, dst); err != nil {
			return "", err
		}
		return dst, nil
	}
	return "", fmt.Errorf("unknown verb %q", a.Verb)
}

// Undo reverses a batch: successful actions are replayed backwards in reverse
// order; failed ones are skipped. The undo is itself journaled as a batch
// with UndoOf set.
func (e *Executor) Undo(b journal.Batch) error {
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

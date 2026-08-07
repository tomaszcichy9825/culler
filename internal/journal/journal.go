// Package journal is the append-only record of applied batches. It is the
// single most important reliability feature in the app: undo replays it
// backwards.
//
// Its guarantee is batch-granular. A batch is appended, and synced, as one
// line after its actions have run, carrying a per-action outcome for
// everything that was attempted — which is how a vanished volume mid-batch
// still leaves a record of partial completion. What it does not survive is
// the process dying mid-batch: a batch that never reached Append leaves no
// record of any of its actions. The journal holds every batch that finished,
// exactly as it finished, and nothing about a batch that did not.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Outcome of a single executed action.
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
	// OutcomeSkipped records an action the collision policy declined to
	// perform: nothing moved, nothing landed, and the source is exactly where
	// it was. It is neither a success (treating it as one would let an apply
	// clear a verdict for work that never happened) nor a failure worth
	// retrying unchanged. Journals written before it existed hold only ok and
	// error; a policy skip in those was recorded as ok with no destination.
	OutcomeSkipped = "skipped"
)

// Action is one executed FileAction with its result. Dst records where the
// file actually went (including the trash location), which is what makes the
// batch replayable backwards.
type Action struct {
	Verb    string `json:"verb"` // copy | move | trash | destroy
	Src     string `json:"src"`
	Dst     string `json:"dst,omitempty"`
	Outcome string `json:"outcome"`
	Err     string `json:"err,omitempty"`
	// Digest is the sha256 of a copy's destination as it was written, so undo
	// can tell the copy it made from a file the user has since changed and
	// refuse to delete the latter. Empty for non-copies and for journals
	// written before it was recorded.
	Digest string `json:"digest,omitempty"`
	// Displaced is where the file this action overwrote went — its path in the
	// trash under the overwrite collision policy. Undo restores it after taking
	// the action itself back; without the record the displaced file would be
	// recoverable only by digging through the trash by hand. Empty when nothing
	// was displaced, and in journals written before it was recorded.
	Displaced string `json:"displaced,omitempty"`
}

// Batch is the journal record for one applied operation.
type Batch struct {
	ID          string    `json:"id"`
	Time        time.Time `json:"time"`
	Description string    `json:"description"`
	Actions     []Action  `json:"actions"`
	UndoOf      string    `json:"undo_of,omitempty"` // batch ID this batch reverses
}

// Journal is an append-only JSON-lines file, one line per batch.
type Journal struct {
	mu   sync.Mutex
	path string
	f    *os.File
	// needsNewline says the file does not end in a newline — a write died
	// mid-line, here or in a previous run. The next append starts on a fresh
	// line rather than continuing the torn one: glued together, both lines
	// would be lost to ReadAll, including the batch that was fsynced and
	// reported durable.
	needsNewline bool
}

// Open opens (or creates) the journal at path for appending.
func Open(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	torn, err := endsMidLine(path)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Journal{path: path, f: f, needsNewline: torn}, nil
}

// endsMidLine reports whether the file at path has content that does not end
// in a newline — the tail a crash mid-append leaves behind.
func endsMidLine(path string) (bool, error) {
	r, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer r.Close()
	info, err := r.Stat()
	if err != nil || info.Size() == 0 {
		return false, err
	}
	last := make([]byte, 1)
	if _, err := r.ReadAt(last, info.Size()-1); err != nil {
		return false, err
	}
	return last[0] != '\n', nil
}

// Append writes one batch as a single JSON line and syncs it to disk before
// returning. A batch that reached the journal is durable.
func (j *Journal) Append(b Batch) error {
	line, err := json.Marshal(b)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := append(line, '\n')
	if j.needsNewline {
		out = append([]byte{'\n'}, out...)
	}
	if _, err := j.f.Write(out); err != nil {
		// The write may have emitted part of the line before failing; the next
		// append must not continue it.
		j.needsNewline = true
		return fmt.Errorf("journal append: %w", err)
	}
	j.needsNewline = false
	return j.f.Sync()
}

// ReadAll returns every intact batch in append order. A corrupt or truncated
// line (crash mid-write) is skipped rather than poisoning history, and there
// is no ceiling on a line's size: one enormous batch — a full-card import —
// must not put every batch after it out of reach.
func (j *Journal) ReadAll() ([]Batch, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var batches []Batch
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var b Batch
			if uerr := json.Unmarshal(line, &b); uerr == nil {
				batches = append(batches, b)
			}
		}
		if err == io.EOF {
			return batches, nil
		}
		if err != nil {
			return batches, err
		}
	}
}

// Close closes the underlying file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.f.Close()
}

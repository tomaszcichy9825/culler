// Package journal is the append-only record of every applied batch. It is the
// single most important reliability feature in the app: undo replays it
// backwards, and after a crash or a vanished volume it says exactly what
// happened to every file.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Outcome of a single executed action.
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
)

// Action is one executed FileAction with its result. Dst records where the
// file actually went (including the trash location), which is what makes the
// batch replayable backwards.
type Action struct {
	Verb    string `json:"verb"` // copy | move | trash
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
}

// Open opens (or creates) the journal at path for appending.
func Open(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Journal{path: path, f: f}, nil
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
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("journal append: %w", err)
	}
	return j.f.Sync()
}

// ReadAll returns every intact batch in append order. A corrupt or truncated
// line (crash mid-write) is skipped rather than poisoning history.
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
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var b Batch
		if err := json.Unmarshal(sc.Bytes(), &b); err != nil {
			continue
		}
		batches = append(batches, b)
	}
	return batches, sc.Err()
}

// Last returns the most recent batch, if any.
func (j *Journal) Last() (Batch, bool, error) {
	batches, err := j.ReadAll()
	if err != nil || len(batches) == 0 {
		return Batch{}, false, err
	}
	return batches[len(batches)-1], true, nil
}

// Close closes the underlying file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.f.Close()
}

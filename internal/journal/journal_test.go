package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBatch(id string) Batch {
	return Batch{
		ID:          id,
		Time:        time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		Description: "Drop RAW on 2 frames",
		Actions: []Action{
			{Verb: "trash", Src: "/photos/DSCF0001.RAF", Dst: "/trash/DSCF0001.RAF", Outcome: OutcomeOK},
			{Verb: "trash", Src: "/photos/DSCF0002.RAF", Outcome: OutcomeError, Err: "volume vanished"},
		},
	}
}

func TestAppendAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	if err := j.Append(testBatch("b1")); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(testBatch("b2")); err != nil {
		t.Fatal(err)
	}

	batches, err := j.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 {
		t.Fatalf("want 2 batches, got %d", len(batches))
	}
	if batches[0].ID != "b1" || batches[1].ID != "b2" {
		t.Errorf("order wrong: %s, %s", batches[0].ID, batches[1].ID)
	}
	got := batches[0]
	if got.Description != "Drop RAW on 2 frames" {
		t.Errorf("description lost: %q", got.Description)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("want 2 actions, got %d", len(got.Actions))
	}
	if got.Actions[0].Outcome != OutcomeOK || got.Actions[0].Dst != "/trash/DSCF0001.RAF" {
		t.Errorf("action 0 mangled: %+v", got.Actions[0])
	}
	// partial completion must be preserved verbatim
	if got.Actions[1].Outcome != OutcomeError || got.Actions[1].Err != "volume vanished" {
		t.Errorf("partial-completion record lost: %+v", got.Actions[1])
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(testBatch("b1")); err != nil {
		t.Fatal(err)
	}
	j.Close()

	j2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	batches, err := j2.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || batches[0].ID != "b1" {
		t.Fatalf("journal lost on reopen: %+v", batches)
	}
}

func TestAppendOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	j.Append(testBatch("b1"))
	j.Append(testBatch("b2"))

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one JSON line per batch, got %d lines", len(lines))
	}
}

func TestSkipsCorruptTrailingLine(t *testing.T) {
	// A crash mid-write must not make history unreadable.
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	j.Append(testBatch("b1"))
	j.Close()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"b2","tr`) // truncated write
	f.Close()

	j2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	batches, err := j2.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || batches[0].ID != "b1" {
		t.Fatalf("want the intact batch only, got %+v", batches)
	}
}

// A write that died mid-line — disk full, power gone — leaves the file without
// its trailing newline. The next append must not continue that line: gluing a
// durable batch onto a corrupt one makes both invisible to ReadAll, and the
// glued one was fsynced and reported durable.
func TestAppendAfterAPartialWriteDoesNotGlueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Batch{ID: "first", Description: "survives"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	// The tail a short write leaves: half a JSON object, no newline.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"torn","desc`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Append(Batch{ID: "second", Description: "must not be glued"}); err != nil {
		t.Fatal(err)
	}

	batches, err := reopened.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0].ID != "first" || batches[1].ID != "second" {
		t.Fatalf("batches = %+v, want first and second with the torn line skipped", batches)
	}
}

// One enormous batch — a full-card import runs to tens of thousands of actions
// — must not put every batch after it out of reach. The old reader capped a
// line at 16MiB and stopped dead there, taking undo and history with it.
func TestReadAllSurvivesABatchPastTheOldLineCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	// ~20MiB of actions in one batch, comfortably past the old cap.
	huge := Batch{ID: "huge", Description: "a very large import"}
	for i := 0; i < 100_000; i++ {
		huge.Actions = append(huge.Actions, Action{
			Verb: "copy",
			Src:  fmt.Sprintf("/cards/one/DSCF%06d.RAF", i),
			Dst:  fmt.Sprintf("/library/2026/keepers/DSCF%06d.RAF", i),
			// Padding stands in for the digests real actions carry.
			Digest:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Outcome: OutcomeOK,
		})
	}
	if err := j.Append(huge); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Batch{ID: "after", Description: "still reachable"}); err != nil {
		t.Fatal(err)
	}

	batches, err := j.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll over a huge batch: %v", err)
	}
	if len(batches) != 2 || batches[0].ID != "huge" || batches[1].ID != "after" {
		t.Fatalf("got %d batches, want the huge one and the one after it", len(batches))
	}
	if len(batches[0].Actions) != 100_000 {
		t.Errorf("the huge batch lost actions: %d", len(batches[0].Actions))
	}
}

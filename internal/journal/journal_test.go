package journal

import (
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

func TestLastBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	if _, ok, err := j.Last(); err != nil || ok {
		t.Fatalf("empty journal: want ok=false, got ok=%v err=%v", ok, err)
	}
	j.Append(testBatch("b1"))
	j.Append(testBatch("b2"))
	last, ok, err := j.Last()
	if err != nil || !ok {
		t.Fatalf("want a last batch, got ok=%v err=%v", ok, err)
	}
	if last.ID != "b2" {
		t.Errorf("want b2, got %s", last.ID)
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

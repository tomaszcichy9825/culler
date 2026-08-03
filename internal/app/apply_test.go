package app

import (
	"strings"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// pairedGroup is a RAW+JPEG frame with one sidecar on the RAW.
func pairedGroup(stem string) scan.PhotoGroup {
	return scan.PhotoGroup{
		Dir:      "/card/DCIM",
		Stem:     stem,
		Kind:     scan.KindPaired,
		Raw:      &scan.FileRef{Path: "/card/DCIM/" + stem + ".RAF", Size: 30_000_000},
		Jpeg:     &scan.FileRef{Path: "/card/DCIM/" + stem + ".JPG", Size: 6_000_000},
		Sidecars: []scan.FileRef{{Path: "/card/DCIM/" + stem + ".RAF.xmp", Size: 2_000}},
		Shot:     time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC),
	}
}

func TestOpForVerdict(t *testing.T) {
	tests := []struct {
		name   string
		record decide.Record
		cut    config.CutScope
		want   string
		ok     bool
	}{
		{"keep both", decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth}, config.CutRemovesBoth, "Keep all", true},
		{"keep the JPEG", decide.Record{Verdict: decide.Keep, Mask: decide.MaskJPEG}, config.CutRemovesBoth, "Drop RAW, keep JPEG", true},
		{"keep the RAW", decide.Record{Verdict: decide.Keep, Mask: decide.MaskRAW}, config.CutRemovesBoth, "Drop JPEG, keep RAW", true},
		{"cut", decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}, config.CutRemovesBoth, "Drop both", true},
		// With cuts scoped to both halves the mask does not soften them.
		{"masked cut, both scope", decide.Record{Verdict: decide.Cut, Mask: decide.MaskRAW}, config.CutRemovesBoth, "Drop both", true},
		// Scoped to the mask, a cut takes only the halves the mask leaves out.
		{"cut the masked JPEG", decide.Record{Verdict: decide.Cut, Mask: decide.MaskRAW}, config.CutRemovesMasked, "Drop JPEG, keep RAW", true},
		{"cut the masked RAW", decide.Record{Verdict: decide.Cut, Mask: decide.MaskJPEG}, config.CutRemovesMasked, "Drop RAW, keep JPEG", true},
		{"nothing is masked out", decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}, config.CutRemovesMasked, "Keep all", true},
		{"undecided", decide.Record{}, config.CutRemovesBoth, "", false},
		{"rated but undecided", decide.Record{Rating: 5}, config.CutRemovesBoth, "", false},
		{"nonsense", decide.Record{Verdict: decide.Verdict("burn"), Mask: decide.MaskBoth}, config.CutRemovesBoth, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ok := opFor(tt.record, tt.cut)
			if ok != tt.ok {
				t.Fatalf("opFor(%+v, %q) ok = %v, want %v", tt.record, tt.cut, ok, tt.ok)
			}
			if !ok {
				return
			}
			if got := op.Describe(); got != tt.want {
				t.Errorf("opFor(%+v, %q) describes %q, want %q", tt.record, tt.cut, got, tt.want)
			}
		})
	}
}

func TestBuildPlanMapsVerdictsToActions(t *testing.T) {
	items := []planned{
		{group: pairedGroup("DSCF0001"), hash: "h1", record: decide.Record{Verdict: decide.Keep, Mask: decide.MaskJPEG}},
		{group: pairedGroup("DSCF0002"), hash: "h2", record: decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}},
		{group: pairedGroup("DSCF0003"), hash: "h3", record: decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth}},
	}

	p, err := buildPlan(items, cullRules(config.CutRemovesBoth))
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}

	// keep/j: RAW + sidecar. cut: RAW + JPEG + sidecar. keep/rj: none.
	if len(p.actions) != 5 {
		t.Fatalf("planned %d actions, want 5: %v", len(p.actions), p.actions)
	}
	for _, a := range p.actions {
		if a.Verb != ops.VerbTrash {
			t.Errorf("verb %q, want %q — deletion is never anything else here", a.Verb, ops.VerbTrash)
		}
	}
	if p.actions[0].Src != "/card/DCIM/DSCF0001.RAF" {
		t.Errorf("first action is %q, want the RAW of the frame keeping its JPEG", p.actions[0].Src)
	}
	// Counts still speak the decision names the frontend renders.
	if got := p.dto.Counts["drop_raw"]; got != 1 {
		t.Errorf("drop_raw count = %d, want 1", got)
	}
	if got := p.dto.Counts["keep_all"]; got != 1 {
		t.Errorf("keep_all count = %d, want 1", got)
	}
	if got := p.dto.Counts["drop_all"]; got != 1 {
		t.Errorf("drop_all count = %d, want 1", got)
	}
	// 30MB RAW + 2KB sidecar, then 30MB + 6MB + 2KB.
	if want := int64(30_000_000 + 2_000 + 30_000_000 + 6_000_000 + 2_000); p.dto.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d", p.dto.TotalBytes, want)
	}
	if !strings.Contains(p.dto.Description, "Drop RAW, keep JPEG (1 frame)") {
		t.Errorf("description %q does not describe the frame keeping its JPEG", p.dto.Description)
	}
}

// A cut scoped to the mask never takes a half the mask says to keep.
func TestBuildPlanCutRemovesMaskedHalvesOnly(t *testing.T) {
	items := []planned{
		{group: pairedGroup("DSCF0001"), hash: "h1", record: decide.Record{Verdict: decide.Cut, Mask: decide.MaskRAW}},
		{group: pairedGroup("DSCF0002"), hash: "h2", record: decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}},
	}

	p, err := buildPlan(items, cullRules(config.CutRemovesMasked))
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(p.actions) != 1 {
		t.Fatalf("planned %d actions, want only the masked-out JPEG: %v", len(p.actions), p.actions)
	}
	if p.actions[0].Src != "/card/DCIM/DSCF0001.JPG" {
		t.Errorf("trashed %q, want the JPEG the mask leaves out", p.actions[0].Src)
	}

	// The same frames under the default scope lose everything.
	wide, err := buildPlan(items, cullRules(config.CutRemovesBoth))
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(wide.actions) != 6 {
		t.Errorf("planned %d actions, want both frames entirely: %v", len(wide.actions), wide.actions)
	}
}

func TestBuildPlanKeepsPerFrameActions(t *testing.T) {
	items := []planned{
		{group: pairedGroup("DSCF0001"), hash: "h1", record: decide.Record{Verdict: decide.Keep, Mask: decide.MaskRAW}},
		{group: pairedGroup("DSCF0002"), hash: "h2", record: decide.Record{Verdict: decide.Keep, Mask: decide.MaskRAW}},
	}
	p, err := buildPlan(items, cullRules(config.CutRemovesBoth))
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(p.planned) != 2 {
		t.Fatalf("kept %d frames, want 2", len(p.planned))
	}
	for _, it := range p.planned {
		if len(it.actions) != 1 {
			t.Fatalf("frame %s has %d actions, want 1", it.group.Stem, len(it.actions))
		}
		if it.actions[0].Src != it.group.Jpeg.Path {
			t.Errorf("frame %s trashes %q, want its JPEG", it.group.Stem, it.actions[0].Src)
		}
	}
}

func TestBuildPlanEmpty(t *testing.T) {
	p, err := buildPlan(nil, cullRules(config.CutRemovesBoth))
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(p.actions) != 0 {
		t.Errorf("planned %d actions for no frames", len(p.actions))
	}
	if p.dto.Description != "Nothing to apply" {
		t.Errorf("description = %q", p.dto.Description)
	}
	if p.dto.Actions == nil {
		t.Error("Actions is nil; the frontend expects an empty list")
	}
}

func TestPickUndoTarget(t *testing.T) {
	batches := []journal.Batch{
		{ID: "a", Description: "first"},
		{ID: "b", Description: "second"},
		{ID: "c", Description: "Undo: second", UndoOf: "b"},
	}
	got, ok := pickUndoTarget(batches)
	if !ok {
		t.Fatal("no undo target, want the first batch")
	}
	if got.ID != "a" {
		t.Errorf("undo target %q, want %q: b is already undone and c is itself an undo", got.ID, "a")
	}

	if _, ok := pickUndoTarget(nil); ok {
		t.Error("empty journal offered an undo target")
	}
	allUndone := []journal.Batch{{ID: "a"}, {ID: "b", UndoOf: "a"}}
	if _, ok := pickUndoTarget(allUndone); ok {
		t.Error("fully undone journal offered an undo target")
	}
}

func TestBatchDTO(t *testing.T) {
	b := journal.Batch{
		ID:          "42-1",
		Time:        time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC),
		Description: "Drop both (1 frame)",
		Actions: []journal.Action{
			{Verb: "trash", Src: "/card/a.RAF", Dst: "/trash/a.RAF", Outcome: journal.OutcomeOK},
			{Verb: "trash", Src: "/card/a.JPG", Outcome: journal.OutcomeError, Err: "device not configured"},
		},
	}
	dto := batchDTO(b)
	if dto.Time != "2026-05-01T09:30:00Z" {
		t.Errorf("Time = %q, want RFC3339", dto.Time)
	}
	if len(dto.Actions) != 2 {
		t.Fatalf("%d actions, want 2", len(dto.Actions))
	}
	if dto.Actions[1].Err != "device not configured" {
		t.Errorf("failed action lost its error: %+v", dto.Actions[1])
	}

	empty := batchDTO(journal.Batch{})
	if empty.Time != "" {
		t.Errorf("zero batch formatted a time: %q", empty.Time)
	}
}

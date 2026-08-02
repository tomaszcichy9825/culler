// Package ops is the single path through which every file operation flows.
// Ops produce pure plans; the Executor applies them, journals every action
// with its real outcome, and can replay a batch backwards for undo.
package ops

import (
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// Verb is what happens to one file.
type Verb string

const (
	VerbCopy  Verb = "copy"
	VerbMove  Verb = "move"
	VerbTrash Verb = "trash"
)

// FileAction is one planned file operation. Dst is empty for Trash — the
// trash location is decided at execution time and recorded in the journal.
type FileAction struct {
	Verb Verb
	Src  string
	Dst  string
}

// Op plans file actions for a set of groups. Plan is pure: no side effects,
// no filesystem access. Journaling, undo, and progress reporting live in the
// Executor, implemented exactly once.
type Op interface {
	Plan(groups []scan.PhotoGroup) ([]FileAction, error)
	Describe() string
}

// trashRefs plans a Trash for each non-nil ref.
func trashRefs(refs ...*scan.FileRef) []FileAction {
	var actions []FileAction
	for _, r := range refs {
		if r != nil {
			actions = append(actions, FileAction{Verb: VerbTrash, Src: r.Path})
		}
	}
	return actions
}

func trashSidecars(g scan.PhotoGroup) []FileAction {
	var actions []FileAction
	for _, s := range g.Sidecars {
		actions = append(actions, FileAction{Verb: VerbTrash, Src: s.Path})
	}
	return actions
}

// DropRAW trashes the RAW of each group that has one. Sidecars follow the RAW.
type DropRAW struct{}

func (DropRAW) Plan(groups []scan.PhotoGroup) ([]FileAction, error) {
	var actions []FileAction
	for _, g := range groups {
		if g.Raw == nil {
			continue
		}
		actions = append(actions, trashRefs(g.Raw)...)
		actions = append(actions, trashSidecars(g)...)
	}
	return actions, nil
}

func (DropRAW) Describe() string { return "Drop RAW, keep JPEG" }

// DropJPEG trashes the JPEG of each group that has one. Sidecars belong to
// the RAW and stay.
type DropJPEG struct{}

func (DropJPEG) Plan(groups []scan.PhotoGroup) ([]FileAction, error) {
	var actions []FileAction
	for _, g := range groups {
		actions = append(actions, trashRefs(g.Jpeg)...)
	}
	return actions, nil
}

func (DropJPEG) Describe() string { return "Drop JPEG, keep RAW" }

// DropBoth trashes every file of the group, sidecars included.
type DropBoth struct{}

func (DropBoth) Plan(groups []scan.PhotoGroup) ([]FileAction, error) {
	var actions []FileAction
	for _, g := range groups {
		actions = append(actions, trashRefs(g.Raw, g.Jpeg)...)
		actions = append(actions, trashSidecars(g)...)
	}
	return actions, nil
}

func (DropBoth) Describe() string { return "Drop both" }

// KeepAll plans nothing. It exists so a "keep" decision routes through the
// same pipeline and shows up in confirmation summaries.
type KeepAll struct{}

func (KeepAll) Plan([]scan.PhotoGroup) ([]FileAction, error) { return nil, nil }

func (KeepAll) Describe() string { return "Keep all" }

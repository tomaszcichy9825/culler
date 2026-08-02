package app

import (
	"fmt"

	"github.com/tomaszcichy9825/culler/internal/decide"
)

// DecisionItem is one frame's decision, as the frontend sends it.
type DecisionItem struct {
	Hash     string `json:"hash"`
	Dir      string `json:"dir"`
	Stem     string `json:"stem"`
	Decision string `json:"decision"`
}

// DecisionService records what the user decided about a frame. Decisions are
// cheap and reversible: they change nothing on disk until an apply.
type DecisionService struct {
	app *App
}

// NewDecisionService binds the service to the shared state.
func NewDecisionService(a *App) *DecisionService {
	return &DecisionService{app: a}
}

// Set records one decision. Passing "none" clears it.
func (s *DecisionService) Set(hash, dir, stem, decision string) error {
	item, err := toItem(DecisionItem{Hash: hash, Dir: dir, Stem: stem, Decision: decision})
	if err != nil {
		return err
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.Set(item.Hash, item.Dir, item.Stem, item.D)
}

// SetBatch records many decisions in one transaction. The grid marks frames
// far faster than it should touch the disk, so it collects them and flushes
// through here. Either the whole batch lands or none of it does.
func (s *DecisionService) SetBatch(items []DecisionItem) error {
	converted := make([]decide.Item, 0, len(items))
	for _, it := range items {
		item, err := toItem(it)
		if err != nil {
			return err
		}
		converted = append(converted, item)
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetBatch(converted)
}

// toItem validates one incoming decision and converts it for the store.
func toItem(it DecisionItem) (decide.Item, error) {
	d, err := parseDecision(it.Decision)
	if err != nil {
		return decide.Item{}, err
	}
	if it.Hash == "" {
		return decide.Item{}, fmt.Errorf("no frame identity for %q: its decision cannot be recorded", it.Stem)
	}
	return decide.Item{Hash: it.Hash, Dir: it.Dir, Stem: it.Stem, D: d}, nil
}

// parseDecision converts a decision string from the frontend, rejecting
// anything the store would not accept.
func parseDecision(s string) (decide.Decision, error) {
	switch d := decide.Decision(s); d {
	case decide.None, decide.KeepAll, decide.DropRAW, decide.DropJPEG, decide.DropAll:
		return d, nil
	}
	return "", fmt.Errorf("unknown decision %q: want %q, %q, %q, %q or %q",
		s, decide.None, decide.KeepAll, decide.DropRAW, decide.DropJPEG, decide.DropAll)
}

package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// ActionDTO is one planned file operation.
type ActionDTO struct {
	Verb string `json:"verb"` // copy | move | trash
	Src  string `json:"src"`
}

// PlanDTO is what an apply would do, with nothing done yet. Counts is keyed
// by decision and holds the number of frames each one contributes; the file
// count is the length of Actions.
type PlanDTO struct {
	Description string         `json:"description"`
	Actions     []ActionDTO    `json:"actions"`
	Counts      map[string]int `json:"counts"`
	TotalBytes  int64          `json:"totalBytes"`
}

// ResultDTO is one executed action and where the file ended up.
type ResultDTO struct {
	Verb    string `json:"verb"`
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	Outcome string `json:"outcome"` // ok | error
	Err     string `json:"err"`
}

// BatchDTO is the journal record of one applied batch, as the frontend sees
// it. A batch with failed actions is still a batch: partial completion is a
// journaled fact, not an exception.
type BatchDTO struct {
	ID          string      `json:"id"`
	Time        string      `json:"time"` // RFC3339
	Description string      `json:"description"`
	Actions     []ResultDTO `json:"actions"`
}

// ApplyService turns recorded decisions into file operations.
type ApplyService struct {
	app *App
}

// NewApplyService binds the service to the shared state.
func NewApplyService(a *App) *ApplyService {
	return &ApplyService{app: a}
}

// Plan reports what applying the decisions on hashes would do to dir. It has
// no side effects: nothing is trashed, moved or cleared. Passing no hashes
// plans every decided frame in the folder.
func (s *ApplyService) Plan(dir string, hashes []string) (PlanDTO, error) {
	items, err := s.collect(dir, hashes)
	if err != nil {
		return PlanDTO{}, err
	}
	p, err := buildPlan(items, s.app.Config().Behaviour.CutRemoves)
	if err != nil {
		return PlanDTO{}, err
	}
	return p.dto, nil
}

// Apply executes the plan for hashes over dir and clears the decisions it
// carried out. A frame whose files did not all move keeps its decision, so a
// failed action can be retried rather than silently forgotten.
func (s *ApplyService) Apply(dir string, hashes []string) (BatchDTO, error) {
	items, err := s.collect(dir, hashes)
	if err != nil {
		return BatchDTO{}, err
	}
	p, err := buildPlan(items, s.app.Config().Behaviour.CutRemoves)
	if err != nil {
		return BatchDTO{}, err
	}

	var batch journal.Batch
	if len(p.actions) > 0 {
		trasher, err := s.app.trasher(dir)
		if err != nil {
			return BatchDTO{}, err
		}
		jrnl, err := s.app.openJournal()
		if err != nil {
			return BatchDTO{}, err
		}
		executor := &ops.Executor{Journal: jrnl, Trasher: trasher}
		var applyErr error
		batch, applyErr = executor.Apply(p.dto.Description, p.actions)
		// Executor.Apply errors only on journal write failure, after the
		// actions have already run. Files have moved either way, so clear
		// the decisions for whatever succeeded before surfacing the error —
		// otherwise a retry would re-execute completed operations.
		if err := s.clearApplied(p.planned, batch); err != nil {
			return batchDTO(batch), err
		}
		if applyErr != nil {
			return batchDTO(batch), applyErr
		}
		return batchDTO(batch), nil
	}

	if err := s.clearApplied(p.planned, batch); err != nil {
		return batchDTO(batch), err
	}
	return batchDTO(batch), nil
}

// Undo reverses the most recent batch that has not already been undone.
func (s *ApplyService) Undo() error {
	jrnl, err := s.app.openJournal()
	if err != nil {
		return err
	}
	batches, err := jrnl.ReadAll()
	if err != nil {
		return err
	}
	target, ok := pickUndoTarget(batches)
	if !ok {
		return errors.New("nothing to undo")
	}
	// Undo needs no trasher: it moves files back to where the journal says
	// they came from and removes the copies it made.
	executor := &ops.Executor{Journal: jrnl}
	return executor.Undo(target)
}

// planned is one frame with the actions its verdict produces.
type planned struct {
	group   scan.PhotoGroup
	hash    string
	record  decide.Record
	actions []ops.FileAction
}

// plan is a whole batch: the actions to execute, the frames they came from,
// and the summary the confirmation UI shows.
type plan struct {
	actions []ops.FileAction
	planned []planned
	dto     PlanDTO
}

// judgement is a verdict and the mask it applies to, which is all of a record
// that decides what happens to the files. The rating plays no part.
type judgement struct {
	verdict decide.Verdict
	mask    decide.Mask
}

// planOrder fixes the order judgements are planned in, so the same set of
// frames always produces the same plan.
var planOrder = []judgement{
	{decide.Keep, decide.MaskJPEG},
	{decide.Keep, decide.MaskRAW},
	{decide.Cut, decide.MaskBoth},
	{decide.Cut, decide.MaskRAW},
	{decide.Cut, decide.MaskJPEG},
	{decide.Keep, decide.MaskBoth},
}

// collect rescans dir and pairs each decided frame with its decision. The
// scan is redone rather than trusted from the last open: files may have moved
// under the app, and a plan must describe the disk as it is now. An empty
// hashes list means every decided frame in the folder.
func (s *ApplyService) collect(dir string, hashes []string) ([]planned, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", resolved)
	}

	groups, err := scan.ScanDir(resolved, s.app.Config().ScanConfig())
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", resolved, err)
	}
	computed := hashGroups(groups, s.app.hashWorkers(platform.IsNetwork(resolved)), nil)

	wanted := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		wanted[h] = true
	}

	store, err := s.app.decisions()
	if err != nil {
		return nil, err
	}

	var items []planned
	for i, g := range groups {
		h := computed[i]
		if h == "" || (len(wanted) > 0 && !wanted[h]) {
			continue
		}
		rec, ok, err := store.Get(h)
		if err != nil {
			return nil, fmt.Errorf("read decisions: %w", err)
		}
		if !ok {
			continue
		}
		items = append(items, planned{group: g, hash: h, record: rec})
	}
	return items, nil
}

// buildPlan maps each frame's verdict onto its op and plans it. Planning one
// frame at a time costs nothing — Plan is pure — and keeps every action
// attributable to the frame that asked for it, which is what lets a failed
// action hold on to its verdict. cut says how far a cut reaches, which is the
// one part of the mapping the user can change.
func buildPlan(items []planned, cut config.CutScope) (plan, error) {
	p := plan{dto: PlanDTO{Counts: map[string]int{}}}
	sizes := map[string]int64{}
	var parts []string

	for _, j := range planOrder {
		record := decide.Record{Verdict: j.verdict, Mask: j.mask}
		op, ok := opFor(record, cut)
		if !ok {
			continue
		}
		var frames int
		for _, it := range items {
			if it.record.Verdict != j.verdict || it.record.Mask != j.mask {
				continue
			}
			actions, err := op.Plan([]scan.PhotoGroup{it.group})
			if err != nil {
				return plan{}, fmt.Errorf("plan %s for %s: %w", j.verdict, it.group.Stem, err)
			}
			it.actions = actions
			frames++
			collectSizes(sizes, it.group)
			p.planned = append(p.planned, it)
			p.actions = append(p.actions, actions...)
		}
		if frames > 0 {
			// Counts are keyed in the pre-verdict vocabulary for as long as
			// the frontend renders that field.
			key := legacyDecision(record)
			p.dto.Counts[key] += frames
			parts = append(parts, fmt.Sprintf("%s (%d %s)", op.Describe(), frames, pluralFrames(frames)))
		}
	}

	p.dto.Actions = make([]ActionDTO, 0, len(p.actions))
	for _, a := range p.actions {
		p.dto.Actions = append(p.dto.Actions, ActionDTO{Verb: string(a.Verb), Src: a.Src})
		p.dto.TotalBytes += sizes[a.Src]
	}
	p.dto.Description = "Nothing to apply"
	if len(parts) > 0 {
		p.dto.Description = strings.Join(parts, ", ")
	}
	return p, nil
}

// opFor maps a recorded verdict onto the op that carries it out. A keep holds
// on to the halves its mask names and trashes the rest; a cut takes the whole
// frame, unless the user has scoped cuts to the mask, in which case it takes
// only the halves the mask leaves out — which for a mask of both halves means
// it takes nothing at all. An undecided frame plans nothing, whatever its
// rating, and so does a record whose verdict makes no sense.
func opFor(r decide.Record, cut config.CutScope) (ops.Op, bool) {
	switch r.Verdict {
	case decide.Keep:
		return maskOp(r.Mask)
	case decide.Cut:
		if cut == config.CutRemovesMasked {
			return maskOp(r.Mask)
		}
		return ops.DropBoth{}, true
	}
	return nil, false
}

// maskOp is the op that leaves exactly the halves m names on disk.
func maskOp(m decide.Mask) (ops.Op, bool) {
	switch m {
	case decide.MaskBoth:
		return ops.KeepAll{}, true
	case decide.MaskRAW:
		return ops.DropJPEG{}, true
	case decide.MaskJPEG:
		return ops.DropRAW{}, true
	}
	return nil, false
}

// collectSizes records the size of every file in g, so the plan can total the
// bytes its actions move without stat-ing anything a second time.
func collectSizes(sizes map[string]int64, g scan.PhotoGroup) {
	for _, ref := range []*scan.FileRef{g.Raw, g.Jpeg} {
		if ref != nil {
			sizes[ref.Path] = ref.Size
		}
	}
	for _, ref := range g.Sidecars {
		sizes[ref.Path] = ref.Size
	}
}

func pluralFrames(n int) string {
	if n == 1 {
		return "frame"
	}
	return "frames"
}

// clearApplied clears the verdicts of the frames whose actions all succeeded.
// A frame that lost only some of its files keeps its verdict so the user can
// apply it again once the cause is fixed. Ratings are left alone: they judge
// the photograph, not the cull, and a frame that survived still carries its
// stars.
func (s *ApplyService) clearApplied(items []planned, batch journal.Batch) error {
	outcomes := make(map[string]string, len(batch.Actions))
	for _, a := range batch.Actions {
		outcomes[a.Src] = a.Outcome
	}

	var cleared []decide.VerdictItem
	for _, it := range items {
		done := true
		for _, a := range it.actions {
			if outcomes[a.Src] != journal.OutcomeOK {
				done = false
				break
			}
		}
		if !done {
			continue
		}
		cleared = append(cleared, decide.VerdictItem{
			Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem, Verdict: decide.Undecided,
		})
	}
	if len(cleared) == 0 {
		return nil
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetVerdictBatch(cleared)
}

// pickUndoTarget returns the most recent batch that is neither an undo itself
// nor already undone. Undo walks backwards through real work only, so
// pressing undo twice reverses the two batches before it rather than
// reversing its own reversal.
func pickUndoTarget(batches []journal.Batch) (journal.Batch, bool) {
	undone := make(map[string]bool)
	for _, b := range batches {
		if b.UndoOf != "" {
			undone[b.UndoOf] = true
		}
	}
	for i := len(batches) - 1; i >= 0; i-- {
		b := batches[i]
		if b.UndoOf == "" && !undone[b.ID] {
			return b, true
		}
	}
	return journal.Batch{}, false
}

// batchDTO flattens an executed batch for the frontend.
func batchDTO(b journal.Batch) BatchDTO {
	dto := BatchDTO{
		ID:          b.ID,
		Description: b.Description,
		Actions:     make([]ResultDTO, 0, len(b.Actions)),
	}
	if !b.Time.IsZero() {
		dto.Time = b.Time.Format(time.RFC3339)
	}
	for _, a := range b.Actions {
		dto.Actions = append(dto.Actions, ResultDTO{
			Verb:    a.Verb,
			Src:     a.Src,
			Dst:     a.Dst,
			Outcome: a.Outcome,
			Err:     a.Err,
		})
	}
	return dto
}

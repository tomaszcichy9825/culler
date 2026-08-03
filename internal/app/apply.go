package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// ActionDTO is one planned file operation.
type ActionDTO struct {
	Verb string `json:"verb"` // copy | move | trash
	Src  string `json:"src"`
	Dst  string `json:"dst"` // empty for trash, and for a plan that only removes
}

// DestinationPlanDTO is one destination's share of a plan, for the
// confirmation summary. Path is the destination as the user set it, tokens
// and all, because that is the thing they recognise — a template that expands
// per frame has no single expanded path to show.
type DestinationPlanDTO struct {
	Path   string `json:"path"`
	Verb   string `json:"verb"` // copy | move
	Frames int    `json:"frames"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// PlanDTO is what an apply would do, with nothing done yet. Counts is keyed
// by decision and holds the number of frames each one contributes; the file
// count is the length of Actions.
type PlanDTO struct {
	Description  string               `json:"description"`
	Actions      []ActionDTO          `json:"actions"`
	Counts       map[string]int       `json:"counts"`
	Destinations []DestinationPlanDTO `json:"destinations"`
	TotalBytes   int64                `json:"totalBytes"`
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
	rules, err := planRules(s.app.Config())
	if err != nil {
		return PlanDTO{}, err
	}
	p, err := buildPlan(items, rules)
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
	cfg := s.app.Config()
	rules, err := planRules(cfg)
	if err != nil {
		return BatchDTO{}, err
	}
	p, err := buildPlan(items, rules)
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
		executor := &ops.Executor{
			Journal:   jrnl,
			Trasher:   trasher,
			Collision: cfg.Behaviour.CollisionPolicy,
			Verify:    cfg.Behaviour.VerifyCopies,
		}
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

// rules is the part of the configuration that changes what a plan does, with
// the paths in it already resolved.
type rules struct {
	cut config.CutScope
	// libraryRoot is what a destination that is not an absolute path hangs
	// off. Already expanded, so nothing downstream has to know about ~.
	libraryRoot string
	// moveOnImport takes routed frames off the card instead of copying them.
	moveOnImport bool
}

// planRules resolves the configuration a plan is built against, failing early
// if the library root cannot be made sense of — better here than halfway
// through writing files into a path nobody meant.
func planRules(cfg config.Config) (rules, error) {
	root, err := expandPath(cfg.Behaviour.LibraryRoot)
	if err != nil {
		return rules{}, fmt.Errorf("library root: %w", err)
	}
	return rules{
		cut:          cfg.Behaviour.CutRemoves,
		libraryRoot:  root,
		moveOnImport: cfg.Behaviour.MoveOnImport,
	}, nil
}

// buildPlan maps each frame's decision onto its op and plans it. Planning one
// frame at a time costs nothing — Plan is pure — and keeps every action
// attributable to the frame that asked for it, which is what lets a failed
// action hold on to its verdict.
//
// A frame with a destination is an import and is planned separately: its
// surviving halves are copied into the library and nothing on the card is
// touched, whatever its mask says. Trashing half a frame is a cull of a folder
// you already own; an import reads the card and leaves it exactly as it found
// it, so the same card can be imported twice or imported to two places.
func buildPlan(items []planned, r rules) (plan, error) {
	p := plan{dto: PlanDTO{Counts: map[string]int{}}}
	sizes := map[string]int64{}
	var parts []string

	staying, routed := splitRouted(items)

	for _, j := range planOrder {
		record := decide.Record{Verdict: j.verdict, Mask: j.mask}
		op, ok := opFor(record, r.cut)
		if !ok {
			continue
		}
		var frames int
		for _, it := range staying {
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

	for _, dest := range sortedKeys(routed) {
		target, err := resolveDestination(dest, r.libraryRoot)
		if err != nil {
			return plan{}, err
		}
		summary := DestinationPlanDTO{Path: dest, Verb: routeVerb(r.moveOnImport)}
		for _, it := range routed[dest] {
			// The mask travels with the frame, so a keep on the RAW alone
			// imports the RAW alone. It is read per frame rather than per
			// destination because a plan can hold frames masked differently
			// and importing the wrong half is not recoverable by re-running.
			op := routeOp(target, ops.Halves(it.record.Mask), r.moveOnImport)
			actions, err := op.Plan([]scan.PhotoGroup{it.group})
			if err != nil {
				return plan{}, fmt.Errorf("plan %s for %s: %w", dest, it.group.Stem, err)
			}
			it.actions = actions
			summary.Frames++
			summary.Files += len(actions)
			collectSizes(sizes, it.group)
			for _, a := range actions {
				summary.Bytes += sizes[a.Src]
			}
			p.planned = append(p.planned, it)
			p.actions = append(p.actions, actions...)
		}
		if summary.Frames == 0 {
			continue
		}
		p.dto.Destinations = append(p.dto.Destinations, summary)
		parts = append(parts, fmt.Sprintf("%s %s (%d %s)",
			importVerb(r.moveOnImport), dest, summary.Frames, pluralFrames(summary.Frames)))
	}

	p.dto.Actions = make([]ActionDTO, 0, len(p.actions))
	for _, a := range p.actions {
		p.dto.Actions = append(p.dto.Actions, ActionDTO{Verb: string(a.Verb), Src: a.Src, Dst: a.Dst})
		p.dto.TotalBytes += sizes[a.Src]
	}
	p.dto.Description = "Nothing to apply"
	if len(parts) > 0 {
		p.dto.Description = strings.Join(parts, ", ")
	}
	return p, nil
}

// splitRouted separates the frames going somewhere from the frames staying
// where they are. A cut with a destination is a contradiction the store
// already refuses to create; if one turns up anyway the cut wins, because
// deleting something the user asked to keep is the worse mistake.
func splitRouted(items []planned) (staying []planned, routed map[string][]planned) {
	routed = map[string][]planned{}
	for _, it := range items {
		if it.record.Destination != "" && it.record.Verdict == decide.Keep {
			routed[it.record.Destination] = append(routed[it.record.Destination], it)
			continue
		}
		staying = append(staying, it)
	}
	return staying, routed
}

// routeOp builds the op that carries one frame's surviving halves to target.
// The op reads EXIF only if the destination template asks it to.
func routeOp(target string, halves ops.Halves, move bool) ops.Op {
	if move {
		return ops.MoveTo{Dest: target, Halves: halves, Metadata: frameMetadata}
	}
	return ops.CopyTo{Dest: target, Halves: halves, Metadata: frameMetadata}
}

// frameMetadata answers {camera} and {lens} for one frame. It is called only
// when a destination template names them, so the usual import pays nothing for
// it. A frame whose EXIF will not read answers with nothing, and those parts
// of the path collapse rather than the import failing.
func frameMetadata(g scan.PhotoGroup) (camera, lens string) {
	ref := g.Raw
	if ref == nil {
		ref = g.Jpeg
	}
	if ref == nil {
		return "", ""
	}
	fields, err := exif.Read(ref.Path)
	if err != nil {
		return "", ""
	}
	camera = strings.TrimSpace(fields.Model.Value)
	if camera == "" {
		camera = strings.TrimSpace(fields.Make.Value)
	}
	return camera, strings.TrimSpace(fields.LensModel.Value)
}

func routeVerb(move bool) string {
	if move {
		return string(ops.VerbMove)
	}
	return string(ops.VerbCopy)
}

func importVerb(move bool) string {
	if move {
		return "Move to"
	}
	return "Copy to"
}

// resolveDestination turns a recorded destination into a real path. An
// absolute path or one starting with ~ is taken at its word; anything else is
// library-relative, which is what makes "2026/portraits" mean the same thing
// on every machine the library is opened on.
func resolveDestination(dest, libraryRoot string) (string, error) {
	trimmed := strings.TrimSpace(dest)
	if trimmed == "" {
		return "", errors.New("a routed frame has no destination")
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "~") {
		return expandPath(trimmed)
	}
	return filepath.Join(libraryRoot, trimmed), nil
}

// sortedKeys fixes the order destinations are planned in, so the same set of
// frames always produces the same plan and the same summary.
func sortedKeys(m map[string][]planned) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	var pruned []string
	for _, it := range items {
		done := true
		removed := false
		for _, a := range it.actions {
			if outcomes[a.Src] != journal.OutcomeOK {
				done = false
				break
			}
			if a.Verb == ops.VerbTrash || a.Verb == ops.VerbMove {
				removed = true
			}
		}
		if !done {
			continue
		}
		cleared = append(cleared, decide.VerdictItem{
			Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem, Verdict: decide.Undecided,
		})
		if removed {
			pruned = append(pruned, it.hash)
		}
	}

	// The catalogue must not keep frames whose files just left their folder.
	// Best-effort: a prune failure never fails the apply — the files have
	// already moved — and the self-healing reindex catches anything missed.
	if len(pruned) > 0 {
		_ = NewLibraryIndexService(s.app).PruneApplied(pruned)
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

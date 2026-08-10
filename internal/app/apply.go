package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/catalog"
	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/hash"
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
//
// One folder can appear twice, once per verb: frames copied there and frames
// moved there are different promises about the card, and a summary that
// merged them would have to lie about one of them.
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
	Outcome string `json:"outcome"` // ok | error | skipped
	Err     string `json:"err"`
}

// FrameKeyDTO names one frame the way the grid holds it: the folder it was in,
// its stem, and the identity its decision was recorded under.
type FrameKeyDTO struct {
	Dir  string `json:"dir"`
	Stem string `json:"stem"`
	Hash string `json:"hash"`
}

// BatchDTO is the journal record of one applied batch, as the frontend sees
// it. A batch with failed actions is still a batch: partial completion is a
// journaled fact, not an exception.
//
// Removed and Unrouted are what the batch actually consumed, so the grid can
// be brought up to date in place rather than by reopening the folder — a
// rescan of a full card after culling six frames is the same cost all over
// again, for six frames' worth of news.
type BatchDTO struct {
	ID          string      `json:"id"`
	Time        string      `json:"time"` // RFC3339
	Description string      `json:"description"`
	Actions     []ResultDTO `json:"actions"`

	// Removed is the frames whose files all left the folder. They are gone
	// from disk and from the catalogue, so the grid drops them.
	Removed []FrameKeyDTO `json:"removed"`
	// Unrouted is the frames the batch copied somewhere and which stayed where
	// they were. They keep their verdict and lose their destination.
	Unrouted []FrameKeyDTO `json:"unrouted"`
}

// ApplyService turns recorded decisions into file operations.
type ApplyService struct {
	app *App

	// catalogue prunes applied frames out of the library index. It is the
	// same service the shell registers, so an apply reuses its open handle
	// instead of opening the catalogue afresh — and leaking the handle — on
	// every batch. nil is allowed: the prune then opens a short-lived handle
	// of its own and closes it before returning.
	catalogue *LibraryIndexService

	// hashFn reads a frame's identity. nil means hash.Content; a test replaces
	// it to count how many frames a plan actually opened, which is the whole
	// cost of planning a big folder and not something the result reveals.
	hashFn func(path string) (string, error)

	// emit publishes progress to the webview. nil means emitEvent; a test
	// replaces it to read the events without a running application.
	emit func(name string, data any)
}

// identity is the hash read this service plans with.
func (s *ApplyService) identity() func(path string) (string, error) {
	if s.hashFn != nil {
		return s.hashFn
	}
	return hash.Content
}

// report tells the webview how far the apply has got.
func (s *ApplyService) report(phase string, done, total int) {
	emit := s.emit
	if emit == nil {
		emit = emitEvent
	}
	emit(EventApplyProgress, ApplyProgress{Phase: phase, Done: done, Total: total})
}

// NewApplyService binds the service to the shared state. Pass the same
// LibraryIndexService the shell registers so the apply shares its one handle
// on the catalogue; nil is allowed and costs a transient handle per prune.
func NewApplyService(a *App, catalogue *LibraryIndexService) *ApplyService {
	return &ApplyService{app: a, catalogue: catalogue}
}

// FrameRef identifies one frame to apply: the folder it lives in and its
// content hash. A scope is a list of these, and it may name frames in several
// folders — a session spans whatever folders its shots were taken across.
type FrameRef struct {
	Dir  string `json:"dir"`
	Hash string `json:"hash"`
}

// Plan reports what applying the decisions on hashes would do to dir. It has
// no side effects: nothing is trashed, moved or cleared. Passing no hashes
// plans every decided frame in the folder.
func (s *ApplyService) Plan(dir string, hashes []string) (PlanDTO, error) {
	items, err := s.collectFolder(dir, hashes)
	if err != nil {
		return PlanDTO{}, err
	}
	return s.planItems(items)
}

// collectFolder is collect for the one-folder entry points, with the planning
// phase announced around it so a single big folder is as visibly busy as a
// session spanning several is.
func (s *ApplyService) collectFolder(dir string, hashes []string) ([]planned, error) {
	s.report(ApplyPhasePlanning, 0, 1)
	items, err := s.collect(dir, hashes)
	if err != nil {
		return nil, err
	}
	s.report(ApplyPhasePlanning, 1, 1)
	return items, nil
}

// PlanScope reports what applying the named frames would do, across whatever
// folders they live in. It is Plan generalised to a session: a scope is a set
// of frames, not a single folder.
func (s *ApplyService) PlanScope(refs []FrameRef) (PlanDTO, error) {
	items, err := s.collectScope(refs)
	if err != nil {
		return PlanDTO{}, err
	}
	return s.planItems(items)
}

// planItems builds the read-only plan for a set of collected frames.
func (s *ApplyService) planItems(items []planned) (PlanDTO, error) {
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
	// The path is resolved once and everything downstream uses the resolved
	// form, so the trasher and the scan agree about where the folder is. A ~
	// only the scan expanded would leave the rejected folder hanging off a
	// literal "~" beside the working directory.
	resolved, err := expandPath(dir)
	if err != nil {
		return BatchDTO{}, err
	}
	items, err := s.collectFolder(resolved, hashes)
	if err != nil {
		return BatchDTO{}, err
	}
	trasher, err := s.app.trasher(resolved)
	if err != nil {
		return BatchDTO{}, err
	}
	return s.run(items, trasher, []string{resolved})
}

// ApplyScope executes the plan for a set of frames spanning any number of
// folders, as a single journal batch. One batch is the point: a session cull
// touches many folders, and undo has to reverse the whole session at once, not
// just the last folder it happened to write. Rejects still go to each folder's
// own _Rejected, because the scope trasher routes by the file's parent.
func (s *ApplyService) ApplyScope(refs []FrameRef) (BatchDTO, error) {
	items, err := s.collectScope(refs)
	if err != nil {
		return BatchDTO{}, err
	}
	trasher, err := s.app.scopeTrasher()
	if err != nil {
		return BatchDTO{}, err
	}
	return s.run(items, trasher, distinctDirs(refs))
}

// run plans the collected frames, executes them as one batch through trasher,
// and clears the decisions the batch carried out. exportDirs are the folders an
// auto-export refreshes after the files have moved.
func (s *ApplyService) run(items []planned, trasher platform.Trasher, exportDirs []string) (BatchDTO, error) {
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
		jrnl, err := s.app.openJournal()
		if err != nil {
			return BatchDTO{}, err
		}
		executor := &ops.Executor{
			Journal:   jrnl,
			Trasher:   trasher,
			Collision: cfg.Behaviour.CollisionPolicy,
			Verify:    cfg.Behaviour.VerifyCopies,
			// The decisions the batch is about to consume ride on its journal
			// line, so an undo that brings the files back can bring the
			// judgements back with them.
			Annotate: func(b *journal.Batch) {
				b.Cleared = consumedBy(p.planned, *b).record
			},
			// Every few files, and always the last one: a copy of a 30MB RAW
			// over a share is slow enough that silence reads as a hang, and an
			// event per file on a card-sized cull is a needless flood.
			Progress: func(done, total int) {
				if done%8 == 0 || done == total {
					s.report(ApplyPhaseApplying, done, total)
				}
			},
		}
		var applyErr error
		batch, applyErr = executor.Apply(p.dto.Description, p.actions)
		spent := consumedBy(p.planned, batch)
		// Executor.Apply errors only on journal write failure, after the
		// actions have already run. Files have moved either way, so clear
		// the decisions for whatever succeeded before surfacing the error —
		// otherwise a retry would re-execute completed operations.
		if err := s.clearApplied(spent); err != nil {
			return consumedDTO(batch, spent), err
		}
		if applyErr != nil {
			return consumedDTO(batch, spent), applyErr
		}
		// With auto-export on, the surviving frames get fresh sidecars so a
		// library read in Lightroom stays in step with the cull. Best-effort:
		// a sidecar that could not be written never fails the apply that has
		// already moved the files.
		if cfg.Behaviour.XMPExport {
			for _, dir := range exportDirs {
				_, _ = NewXMPExportService(s.app).ExportFolder(dir)
			}
		}
		return consumedDTO(batch, spent), nil
	}

	spent := consumedBy(p.planned, batch)
	if err := s.clearApplied(spent); err != nil {
		return consumedDTO(batch, spent), err
	}
	return consumedDTO(batch, spent), nil
}

// distinctDirs is the folders a scope touches, in first-seen order, so an
// auto-export visits each folder once however many frames it holds.
func distinctDirs(refs []FrameRef) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, ref := range refs {
		if seen[ref.Dir] {
			continue
		}
		seen[ref.Dir] = true
		dirs = append(dirs, ref.Dir)
	}
	return dirs
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
	// they came from and removes the copies it made. Verification carries over
	// from the setting, because a cross-filesystem move back is as capable of
	// destroying the only intact copy as a forward move is.
	executor := &ops.Executor{Journal: jrnl, Verify: s.app.Config().Behaviour.VerifyCopies}
	undone, err := executor.Undo(target)
	// The files are back, so the decisions the batch consumed come back too —
	// for exactly the frames whose files all returned. A restore that fails
	// still reports: the user asked for their cull back, not just their files.
	return errors.Join(err, s.restoreCleared(target, undone))
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

// takesFilesAway reports whether anything in the plan removes a file from
// where it is now. Ordering the backup leg and warning about the card both
// hang off the plan rather than off the setting, because a frame routed with
// m moves whatever the setting says.
func (p plan) takesFilesAway() bool {
	for _, a := range p.actions {
		if a.Verb == ops.VerbMove {
			return true
		}
	}
	return false
}

// planVerb names what a plan does to the source as a whole. A plan that both
// copies and moves is neither, and says so: a screen that picked one would be
// promising something about the card that the import does not keep.
func planVerb(p plan) string {
	moves, copies := false, false
	for _, a := range p.actions {
		switch a.Verb {
		case ops.VerbMove:
			moves = true
		case ops.VerbCopy:
			copies = true
		}
	}
	switch {
	case moves && copies:
		return "mixed"
	case moves:
		return string(ops.VerbMove)
	}
	return string(ops.VerbCopy)
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
//
// Only the frames the store holds a judgement for are identified. The walk is
// cheap; reading 64KB off the head of every file on a full card is not, and an
// undecided frame can produce no action however it hashes. Identity itself is
// untouched by the narrowing: a candidate is still read and still looked up
// under the whole (hash, dir, stem) key, so a frame edited or replaced since it
// was judged still finds no decision and a twin still cannot inherit one.
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

	wanted := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		wanted[h] = true
	}

	store, err := s.app.decisions()
	if err != nil {
		return nil, err
	}
	judged, err := store.ActionableIn(resolved)
	if err != nil {
		return nil, fmt.Errorf("read decisions: %w", err)
	}
	// The stems worth identifying. A scope narrows them further by the hashes
	// it named, because a decision recorded against a hash the scope did not
	// ask for cannot be part of this apply either way.
	candidates := make(map[string]bool, len(judged))
	for _, d := range judged {
		if len(wanted) > 0 && !wanted[d.Hash] {
			continue
		}
		candidates[d.Stem] = true
	}

	shortlist := make([]scan.PhotoGroup, 0, len(candidates))
	for _, g := range groups {
		if candidates[g.Stem] {
			shortlist = append(shortlist, g)
		}
	}
	computed := hashGroupsWith(shortlist, s.app.hashWorkers(platform.IsNetwork(resolved)), s.identity(), nil)

	var items []planned
	for i, g := range shortlist {
		h := computed[i]
		if h == "" || (len(wanted) > 0 && !wanted[h]) {
			continue
		}
		rec, ok, err := store.Get(h, g.Dir, g.Stem)
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

// collectScope pairs each referenced frame with its decision, scanning each
// distinct folder once. It is collect generalised to a set of frames that may
// live in several folders, and it reuses collect so a scope over one folder and
// a plain single-folder apply see exactly the same frames.
func (s *ApplyService) collectScope(refs []FrameRef) ([]planned, error) {
	// Buckets key on the resolved folder, and hashes are deduplicated within
	// each: a scope can name the same folder in two spellings and the same
	// frame more than once, and planning a frame twice runs its trash twice —
	// the duplicate fails on files already gone, and that failure would keep a
	// verdict alive on a frame the first run already carried out.
	byDir := map[string]map[string]bool{}
	var order []string
	for _, ref := range refs {
		dir, err := expandPath(ref.Dir)
		if err != nil {
			return nil, err
		}
		if _, seen := byDir[dir]; !seen {
			order = append(order, dir)
			byDir[dir] = map[string]bool{}
		}
		byDir[dir][ref.Hash] = true
	}

	var items []planned
	s.report(ApplyPhasePlanning, 0, len(order))
	for i, dir := range order {
		hashes := make([]string, 0, len(byDir[dir]))
		for h := range byDir[dir] {
			hashes = append(hashes, h)
		}
		got, err := s.collect(dir, hashes)
		if err != nil {
			return nil, err
		}
		items = append(items, got...)
		s.report(ApplyPhasePlanning, i+1, len(order))
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
	// It is the fallback, not the rule: a frame routed with m or c says for
	// itself how it travels, and only a frame that did not say follows this.
	moveOnImport bool
}

// moves reports how a frame routed with verb v actually travels. The verb the
// user pressed wins; a frame with none follows the setting, which is what
// every route recorded before verbs existed means.
func (r rules) moves(v decide.Verb) bool {
	switch v {
	case decide.VerbMove:
		return true
	case decide.VerbCopy:
		return false
	}
	return r.moveOnImport
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

	staying, routed := splitRouted(items, r)

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

	for _, leg := range routeOrder(routed) {
		target, err := resolveDestination(leg.dest, r.libraryRoot)
		if err != nil {
			return plan{}, err
		}
		summary := DestinationPlanDTO{Path: leg.dest, Verb: routeVerb(leg.move)}
		for _, it := range routed[leg] {
			// The mask travels with the frame, so a keep on the RAW alone
			// imports the RAW alone. It is read per frame rather than per
			// destination because a plan can hold frames masked differently
			// and importing the wrong half is not recoverable by re-running.
			op := routeOp(target, ops.Halves(it.record.Mask), leg.move)
			actions, err := op.Plan([]scan.PhotoGroup{it.group})
			if err != nil {
				return plan{}, fmt.Errorf("plan %s for %s: %w", leg.dest, it.group.Stem, err)
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
			importVerb(leg.move), leg.dest, summary.Frames, pluralFrames(summary.Frames)))
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

// routeKey is one leg of an import: a destination and the verb the frames
// reaching it travel by. Two frames going to the same folder, one copied and
// one moved, are two legs — they plan different ops and promise different
// things about the card.
type routeKey struct {
	dest string
	move bool
}

// splitRouted separates the frames going somewhere from the frames staying
// where they are. A cut with a destination is a contradiction the store
// already refuses to create; if one turns up anyway the cut wins, because
// deleting something the user asked to keep is the worse mistake.
func splitRouted(items []planned, r rules) (staying []planned, routed map[routeKey][]planned) {
	routed = map[routeKey][]planned{}
	for _, it := range items {
		if it.record.Destination != "" && it.record.Verdict == decide.Keep {
			key := routeKey{dest: it.record.Destination, move: r.moves(it.record.Verb)}
			routed[key] = append(routed[key], it)
			continue
		}
		staying = append(staying, it)
	}
	return staying, routed
}

// routeOrder fixes the order the legs are planned in, so the same frames
// always produce the same plan: by destination, and copies before moves at the
// same one.
func routeOrder(routed map[routeKey][]planned) []routeKey {
	keys := make([]routeKey, 0, len(routed))
	for k := range routed {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dest != keys[j].dest {
			return keys[i].dest < keys[j].dest
		}
		return !keys[i].move && keys[j].move
	})
	return keys
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
	if standaloneDestination(trimmed) {
		return expandPath(trimmed)
	}
	return filepath.Join(libraryRoot, trimmed), nil
}

// standaloneDestination reports whether a recorded destination names a place
// of its own — an absolute path, or one rooted at ~ — rather than hanging off
// the library root. Absoluteness is the platform's own idea of it, so a
// drive-letter path is absolute on the platform that has drive letters and is
// never mistaken for a library-relative folder.
func standaloneDestination(dest string) bool {
	return strings.HasPrefix(dest, "~") || filepath.IsAbs(dest)
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

// consumed is what a batch spent from the decision store: the writes that
// clear it, the catalogue rows to prune, and the journal record that lets an
// undo restore it.
type consumed struct {
	cleared []decide.VerdictItem
	routed  []decide.DestinationItem
	pruned  []catalog.FrameKey
	record  []journal.ClearedDecision
}

// consumedBy works out which decisions a batch's outcomes consumed. A frame
// whose actions did not all succeed keeps everything so the user can apply it
// again once the cause is fixed; a frame whose plan moved nothing keeps its
// verdict too — clearing a judgement in exchange for no work at all would
// silently erase it, which is what a fully masked cut used to suffer.
func consumedBy(items []planned, batch journal.Batch) consumed {
	outcomes := make(map[string]string, len(batch.Actions))
	for _, a := range batch.Actions {
		// If the same source somehow appears twice, a success stands: the file
		// did move, and letting a later failure overwrite that would keep a
		// verdict alive on a frame whose files are already gone.
		if existing, seen := outcomes[a.Src]; seen && existing == journal.OutcomeOK {
			continue
		}
		outcomes[a.Src] = a.Outcome
	}

	// A keep is a judgement, not an instruction: applying it costs nothing
	// on disk, so the verdict survives the apply — and the next open, and
	// the catalogue — like a rating does. What an apply consumes is the
	// work it performed: a cut frame's decision goes with its files, and a
	// routed frame's destination is cleared once the copy has landed.
	var c consumed
	for _, it := range items {
		done := true
		removed := false
		var files []string
		for _, a := range it.actions {
			if outcomes[a.Src] != journal.OutcomeOK {
				done = false
				break
			}
			if a.Verb == ops.VerbTrash || a.Verb == ops.VerbMove {
				removed = true
			}
			files = append(files, a.Src)
		}
		if !done {
			continue
		}
		if removed {
			// Clearing a verdict takes the destination with it in the store,
			// so the record carries both halves for the restore.
			c.cleared = append(c.cleared, decide.VerdictItem{
				Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem, Verdict: decide.Undecided,
			})
			c.record = append(c.record, journal.ClearedDecision{
				Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem,
				Verdict: string(it.record.Verdict), Mask: string(it.record.Mask),
				Destination: it.record.Destination, Verb: string(it.record.Verb), Files: files,
			})
			c.pruned = append(c.pruned, catalog.FrameKey{Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem})
		} else if it.record.Destination != "" && len(files) > 0 {
			c.routed = append(c.routed, decide.DestinationItem{
				Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem, Destination: "",
			})
			c.record = append(c.record, journal.ClearedDecision{
				Hash: it.hash, Dir: it.group.Dir, Stem: it.group.Stem,
				Destination: it.record.Destination, Verb: string(it.record.Verb), Files: files,
			})
		}
	}
	return c
}

// clearApplied clears the verdicts of the frames whose actions all succeeded.
// A frame that lost only some of its files keeps its verdict so the user can
// apply it again once the cause is fixed. Ratings are left alone: they judge
// the photograph, not the cull, and a frame that survived still carries its
// stars.
func (s *ApplyService) clearApplied(c consumed) error {
	// The catalogue must not keep frames whose files just left their folder.
	// Best-effort: a prune failure never fails the apply — the files have
	// already moved — and the self-healing reindex catches anything missed.
	// A frame that lost only one half — a drop-RAW that leaves the JPEG — is
	// deliberately pruned whole here and re-catalogued from what survived on
	// disk by the next UpsertDir or index pass.
	if len(c.pruned) > 0 {
		_ = s.pruneApplied(c.pruned)
	}

	if len(c.cleared) == 0 && len(c.routed) == 0 {
		return nil
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	if len(c.routed) > 0 {
		if err := store.SetDestinationBatch(c.routed); err != nil {
			return err
		}
	}
	if len(c.cleared) == 0 {
		return nil
	}
	return store.SetVerdictBatch(c.cleared)
}

// pruneApplied forgets applied frames through the shared catalogue handle, or
// through a short-lived one — closed before returning — when the service was
// built without one, so no apply ever leaks an open catalogue.
func (s *ApplyService) pruneApplied(keys []catalog.FrameKey) error {
	if s.catalogue != nil {
		return s.catalogue.PruneApplied(keys)
	}
	transient := NewLibraryIndexService(s.app)
	defer transient.Close()
	return transient.PruneApplied(keys)
}

// restoreCleared writes back the decisions the undone batch had consumed, for
// exactly the frames whose files all came back. A frame only partly restored
// regains nothing — half its files are still gone, and a verdict over files
// that are not there is what clearing on apply exists to prevent. A decision
// the user has recorded since the apply is left alone: undo restores files,
// it does not overrule the person.
func (s *ApplyService) restoreCleared(target, undone journal.Batch) error {
	if len(target.Cleared) == 0 {
		return nil
	}
	reversed := reversedSources(target, undone)
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	var verdicts []decide.VerdictItem
	var routed []decide.DestinationItem
	for _, c := range target.Cleared {
		if len(c.Files) == 0 {
			continue
		}
		back := true
		for _, f := range c.Files {
			if !reversed[f] {
				back = false
				break
			}
		}
		if !back {
			continue
		}
		rec, ok, err := store.Get(c.Hash, c.Dir, c.Stem)
		if err != nil {
			return err
		}
		if c.Verdict != "" {
			if ok && rec.Verdict != "" {
				continue // re-judged since the apply; the person wins
			}
			verdicts = append(verdicts, decide.VerdictItem{
				Hash: c.Hash, Dir: c.Dir, Stem: c.Stem,
				Verdict: decide.Verdict(c.Verdict), Mask: decide.Mask(c.Mask),
			})
			// A cut never carries a destination, so only a keep's routing is
			// worth putting back alongside its verdict.
			if c.Destination != "" && c.Verdict == string(decide.Keep) {
				routed = append(routed, decide.DestinationItem{
					Hash: c.Hash, Dir: c.Dir, Stem: c.Stem, Destination: c.Destination,
					Verb: decide.Verb(c.Verb),
				})
			}
			continue
		}
		if c.Destination != "" {
			if ok && rec.Destination != "" {
				continue // re-routed since the apply
			}
			routed = append(routed, decide.DestinationItem{
				Hash: c.Hash, Dir: c.Dir, Stem: c.Stem, Destination: c.Destination,
				Verb: decide.Verb(c.Verb),
			})
		}
	}
	if len(verdicts) > 0 {
		if err := store.SetVerdictBatch(verdicts); err != nil {
			return err
		}
	}
	if len(routed) > 0 {
		return store.SetDestinationBatch(routed)
	}
	return nil
}

// reversedSources is the set of original source paths an undo actually put
// back: an original action that succeeded, whose recorded destination the undo
// batch reversed with an ok of its own. The undo records its work keyed on the
// forward action's destination, which is what ties the two batches together.
func reversedSources(target, undone journal.Batch) map[string]bool {
	ok := make(map[string]bool, len(undone.Actions))
	for _, u := range undone.Actions {
		if u.Outcome == journal.OutcomeOK {
			ok[u.Src] = true
		}
	}
	out := map[string]bool{}
	for _, a := range target.Actions {
		if a.Outcome == journal.OutcomeOK && a.Dst != "" && ok[a.Dst] {
			out[a.Src] = true
		}
	}
	return out
}

// pickUndoTarget returns the most recent batch that is neither an undo itself,
// nor already undone, nor a batch that destroyed files. Undo walks backwards
// through reversible work only, so pressing undo twice reverses the two
// batches before it rather than reversing its own reversal — and an
// empty-rejects batch is stepped over to the last batch that can still come
// back, rather than stopping undo dead at a record nothing can act on.
func pickUndoTarget(batches []journal.Batch) (journal.Batch, bool) {
	undone := make(map[string]bool)
	for _, b := range batches {
		if b.UndoOf != "" {
			undone[b.UndoOf] = true
		}
	}
	for i := len(batches) - 1; i >= 0; i-- {
		b := batches[i]
		if b.UndoOf == "" && !undone[b.ID] && !destroyed(b) && !unrestorable(b) {
			return b, true
		}
	}
	return journal.Batch{}, false
}

// unrestorable reports whether a batch's successful work all went somewhere
// undo cannot reach: every action that succeeded was a trash whose destination
// the platform never reported, which is how the Windows Recycle Bin records.
// Undo would have nothing to act on, so such a batch is stepped over like a
// destroy is — otherwise it would jam the undo stack for good, with every undo
// picking it, failing, and journalling nothing. A batch with even one
// restorable action stays a target: part of it can still come back.
func unrestorable(b journal.Batch) bool {
	succeeded := 0
	for _, a := range b.Actions {
		if a.Outcome != journal.OutcomeOK {
			continue
		}
		succeeded++
		if ops.Verb(a.Verb) != ops.VerbTrash || a.Dst != "" {
			return false
		}
	}
	return succeeded > 0
}

// destroyed reports whether a batch permanently deleted anything. The verb is
// the fact the journal recorded, so this holds for journals written before the
// command existed and for anything else that ever destroys.
func destroyed(b journal.Batch) bool {
	for _, a := range b.Actions {
		if ops.Verb(a.Verb) == ops.VerbDestroy {
			return true
		}
	}
	return false
}

// consumedDTO flattens an executed batch together with what it spent, which is
// what lets the grid patch itself instead of rescanning the folder.
func consumedDTO(b journal.Batch, c consumed) BatchDTO {
	dto := batchDTO(b)
	for _, key := range c.pruned {
		dto.Removed = append(dto.Removed, FrameKeyDTO{Dir: key.Dir, Stem: key.Stem, Hash: key.Hash})
	}
	for _, item := range c.routed {
		dto.Unrouted = append(dto.Unrouted, FrameKeyDTO{Dir: item.Dir, Stem: item.Stem, Hash: item.Hash})
	}
	return dto
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

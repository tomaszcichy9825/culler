package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/catalog"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventImportProgress carries ImportProgress while an import is running.
const EventImportProgress = "import:progress"

// The phases an import passes through, in the order they happen. They name
// what the app is doing to the user's files, which is the one thing a progress
// bar over an irreversible operation has to be honest about.
const (
	ImportPhaseScan   = "scan"   // reading the card and working out what is there
	ImportPhaseCopy   = "copy"   // writing frames into the library
	ImportPhaseMove   = "move"   // taking frames off the card into the library
	ImportPhaseBackup = "backup" // writing the optional second copy
)

// ImportProgress reports how far an import has got.
//
// Files counts files written; Total is every file the batch will touch, both
// legs of a second copy included. A move writes nothing through the copier, so
// on a moving import Files stands still until the batch finishes — the phase is
// what the panel should be reading in that case, not the bar.
type ImportProgress struct {
	Dir      string `json:"dir"`
	Phase    string `json:"phase"`
	Files    int    `json:"files"`
	Total    int    `json:"total"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
	Error    string `json:"error"`
}

func init() {
	// Registration gives the binding generator a typed JS/TS API for the event.
	application.RegisterEvent[ImportProgress](EventImportProgress)
}

// CardDTO is one removable volume with a shallow look at what is on it.
//
// The look is deliberately shallow: this list is drawn the moment a card is
// plugged in, and walking a 128 GB card to draw a row would make the screen
// arrive minutes after the card did. Frames is exact only when the card has a
// single image folder; see Estimated.
type CardDTO struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Total   int64  `json:"total"`
	Free    int64  `json:"free"`
	Network bool   `json:"network"`

	// HasDCIM is whether the card is laid out the way a camera writes one.
	HasDCIM bool `json:"hasDcim"`
	// Dir is the folder to open in CULL first: the first image folder on a
	// camera card, the volume itself on anything else.
	Dir string `json:"dir"`
	// Folders is how many image folders the card holds.
	Folders int `json:"folders"`
	// Frames is what the card holds, counted in the first folder and
	// multiplied by the number of folders when there is more than one.
	Frames int `json:"frames"`
	// Estimated is whether Frames was extrapolated rather than counted.
	Estimated bool `json:"estimated"`
	// Error is why the card could not be probed, empty when it could. A card
	// pulled between the volume list and the probe is a row with a note on it,
	// not a failed call.
	Error string `json:"error"`
}

// CardDirDTO is one image folder on a card.
type CardDirDTO struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Frames int    `json:"frames"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	First  string `json:"first"` // RFC3339 of the earliest frame, empty when none
	Last   string `json:"last"`
}

// CardSummaryDTO is what a card holds, worked out on demand.
//
// This one does read every image folder, because it backs the screen the user
// asked for by selecting a card. It still never walks the card: a camera writes
// frames into DCIM subfolders and nowhere else, so the folders are listed and
// each is read once.
type CardSummaryDTO struct {
	Path    string       `json:"path"`
	Name    string       `json:"name"`
	Network bool         `json:"network"`
	HasDCIM bool         `json:"hasDcim"`
	Dirs    []CardDirDTO `json:"dirs"`
	Frames  int          `json:"frames"`
	Files   int          `json:"files"`
	Bytes   int64        `json:"bytes"`

	// Sessions is how many shoots the frames fall into, clustered on shot time
	// with the same gap the catalogue uses. It is a guess from a bounded
	// sample of the card's timestamps, which is why it is not called a count.
	Sessions int    `json:"sessions"`
	First    string `json:"first"` // RFC3339 of the earliest frame on the card
	Last     string `json:"last"`

	// Imported is how many of Sampled frames the catalogue already holds, which
	// is what tells the user this card has been imported before. Sampled is
	// zero when there is no catalogue to ask.
	Imported int `json:"imported"`
	Sampled  int `json:"sampled"`
}

// ImportRouteDTO is one destination the folder's frames are routed to.
type ImportRouteDTO struct {
	// Destination is the route as the user recorded it: library-relative or
	// absolute, and possibly a token template rather than a literal path.
	Destination string `json:"destination"`
	// Path is Destination resolved against the library root, before any tokens
	// in it expand.
	Path   string `json:"path"`
	Frames int    `json:"frames"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// ImportPlanDTO is the routing state of one folder: where its frames are going
// and how many of them are going nowhere.
//
// Routed, Cut and Unrouted partition Frames. Undecided is the part of Unrouted
// nobody has looked at, which is the number the warning is really about — a
// frame the user judged and left on the card is a decision, an untouched one is
// an oversight.
type ImportPlanDTO struct {
	Dir         string           `json:"dir"`
	Frames      int              `json:"frames"`
	Routed      int              `json:"routed"`
	Cut         int              `json:"cut"`
	Unrouted    int              `json:"unrouted"`
	Undecided   int              `json:"undecided"`
	Routes      []ImportRouteDTO `json:"routes"`
	Files       int              `json:"files"`
	Bytes       int64            `json:"bytes"`
	Verb        string           `json:"verb"` // copy | move
	LibraryRoot string           `json:"libraryRoot"`
	Network     bool             `json:"network"`

	// Space is the volume behind each route. It rides along with the plan
	// rather than being its own call because working it out needs the plan,
	// and the plan needs a scan and a hash of the whole folder — asking twice
	// would read the card twice to draw one screen.
	Space []DestinationSpaceDTO `json:"space"`
}

// DestinationSpaceDTO is one route and the volume it lands on.
type DestinationSpaceDTO struct {
	Destination string `json:"destination"`
	Path        string `json:"path"`
	Frames      int    `json:"frames"`
	Bytes       int64  `json:"bytes"`

	// Volume is the mount point the route resolves onto, empty when no listed
	// volume contains it — a path under a mount the lister did not enumerate.
	Volume     string `json:"volume"`
	VolumeName string `json:"volumeName"`
	Free       int64  `json:"free"`
	Total      int64  `json:"total"`
	Network    bool   `json:"network"`
	Removable  bool   `json:"removable"`

	// Fits is whether everything this import routes onto the volume still has
	// room, counting every destination that lands there rather than this one
	// alone. Unknown capacity fits: a bar the app cannot draw must not turn
	// into a warning it cannot justify.
	Fits bool `json:"fits"`
}

// How much of a card the summary is allowed to read.
const (
	// maxCardFolders caps the image folders a summary reads. A card with more
	// than this is a card somebody has been filling for years; the figures
	// stop being exact and the screen says so through the folder list itself.
	maxCardFolders = 64
	// maxSampleHashes caps the frames whose identity is checked against the
	// catalogue. Each one reads the head of a file off the card, which is the
	// slowest thing on this screen.
	maxSampleHashes = 200
	// maxSampleTimes caps the timestamps the session guess clusters.
	maxSampleTimes = 4000
)

// ImportService is IMPORT mode's end of the app: the cards plugged in, what is
// on them, where the review in CULL routed it, and the execution that carries
// it into the library.
//
// It owns no persistence. The routing it reports is the decision store's, the
// copies it makes go through the same op engine and journal an apply uses, and
// the catalogue it asks about already-imported frames is LIBRARY's.
type ImportService struct {
	app *App

	// catalogue answers whether a frame is already in the library. It is the
	// same service the shell registers, so that both ends share one handle on
	// the index. A nil catalogue leaves the already-imported figure unclaimed
	// rather than guessed.
	catalogue *LibraryIndexService

	// volumes replaces the platform lister in tests, which cannot plug a card
	// into the machine running them.
	volumes func() ([]platform.Volume, error)

	// onProgress replaces the event emission in tests, which run without a
	// Wails application to emit into.
	onProgress func(ImportProgress)

	// running holds the one import at a time. Two imports over one folder would
	// race each other for the same destination names.
	running atomic.Bool

	// reportMu serialises progress reports: the scan phase is hashed in
	// parallel and reports from several workers at once.
	reportMu sync.Mutex
}

// NewImportService binds the service to the shared state. Pass the same
// LibraryIndexService the shell registers so both ends share one handle on the
// catalogue; nil is allowed and costs only the already-imported figure.
func NewImportService(a *App, catalogue *LibraryIndexService) *ImportService {
	return &ImportService{app: a, catalogue: catalogue, volumes: platform.Volumes}
}

// DetectCards lists the removable volumes with a shallow look at each.
//
// Network volumes are left out however they are formatted: a share is not a
// card, and offering to import one into the library is offering to copy a disk
// onto itself over SMB.
func (s *ImportService) DetectCards() ([]CardDTO, error) {
	vols, err := s.volumes()
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	out := []CardDTO{}
	for _, v := range vols {
		if !v.Removable || v.Network {
			continue
		}
		out = append(out, s.probe(v))
	}
	return out, nil
}

// probe takes one look at a card. Every failure it can meet — the card was
// pulled, the folder is unreadable — comes back on the row rather than as an
// error, because one bad card must not empty the list of the others.
func (s *ImportService) probe(v platform.Volume) CardDTO {
	card := CardDTO{
		Path:    v.Path,
		Name:    v.Name,
		Total:   v.Total,
		Free:    v.Free,
		Network: v.Network,
		Dir:     v.Path,
	}
	folders, hasDCIM, err := imageFolders(v.Path)
	if err != nil {
		card.Error = err.Error()
		return card
	}
	card.HasDCIM = hasDCIM
	card.Folders = len(folders)
	if len(folders) == 0 {
		return card
	}
	card.Dir = folders[0]

	groups, err := scan.ScanDir(folders[0], s.app.Config().ScanConfig())
	if err != nil {
		card.Error = err.Error()
		return card
	}
	card.Frames = len(groups) * len(folders)
	card.Estimated = len(folders) > 1
	return card
}

// CardSummary reads every image folder on a card and reports what is there.
func (s *ImportService) CardSummary(path string) (CardSummaryDTO, error) {
	resolved, err := expandPath(path)
	if err != nil {
		return CardSummaryDTO{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return CardSummaryDTO{}, fmt.Errorf("open card: %w", err)
	}
	if !info.IsDir() {
		return CardSummaryDTO{}, fmt.Errorf("%s is not a folder", resolved)
	}

	folders, hasDCIM, err := imageFolders(resolved)
	if err != nil {
		return CardSummaryDTO{}, err
	}
	if len(folders) > maxCardFolders {
		folders = folders[:maxCardFolders]
	}

	out := CardSummaryDTO{
		Path:    resolved,
		Name:    filepath.Base(resolved),
		Network: platform.IsNetwork(resolved),
		HasDCIM: hasDCIM,
		Dirs:    []CardDirDTO{},
	}
	cfg := s.app.Config().ScanConfig()
	var all []scan.PhotoGroup
	var shots []time.Time
	for _, dir := range folders {
		groups, err := scan.ScanDir(dir, cfg)
		if err != nil {
			continue // a folder that vanished under the read is not the card
		}
		entry := CardDirDTO{Path: dir, Name: filepath.Base(dir), Frames: len(groups)}
		for _, g := range groups {
			entry.Files += frameFiles(g)
			entry.Bytes += frameBytes(g)
			if !g.Shot.IsZero() {
				shots = append(shots, g.Shot)
			}
		}
		entry.First, entry.Last = spanOf(groups)
		out.Dirs = append(out.Dirs, entry)
		out.Frames += entry.Frames
		out.Files += entry.Files
		out.Bytes += entry.Bytes
		all = append(all, groups...)
	}

	out.Sessions = countSessions(shots)
	out.First, out.Last = spanOf(all)
	out.Imported, out.Sampled, err = s.sampleImported(all)
	if err != nil {
		return CardSummaryDTO{}, err
	}
	return out, nil
}

// sampleImported checks a spread of the card's frames against the catalogue and
// reports how many of them the library already holds.
//
// It is a sample, not a count: proving a whole card has been imported means
// reading the head of every file on it, which is most of the cost of the import
// itself. The frames are taken at an even stride so that a card holding one
// already-imported folder and one fresh one is not read as either extreme.
func (s *ImportService) sampleImported(groups []scan.PhotoGroup) (imported, sampled int, err error) {
	if len(groups) == 0 || s.catalogue == nil {
		return 0, 0, nil
	}
	known, err := s.cataloguedHashes()
	if err != nil {
		return 0, 0, err
	}
	if len(known) == 0 {
		// Nothing is catalogued, so nothing on the card can be in it. Reading
		// the card to prove that would be work for an answer already known.
		return 0, 0, nil
	}

	sample := stride(groups, maxSampleHashes)
	hashes := hashGroups(sample, s.app.hashWorkers(platform.IsNetwork(sample[0].Dir)), nil)
	for _, h := range hashes {
		if h == "" {
			continue // unreadable file: counted in neither direction
		}
		sampled++
		if known[h] {
			imported++
		}
	}
	return imported, sampled, nil
}

// cataloguedHashes is the identity of every frame the library holds.
//
// One row per catalogued frame, which is why it happens behind a card the user
// selected rather than on a redraw.
func (s *ImportService) cataloguedHashes() (map[string]bool, error) {
	if s.catalogue == nil {
		return nil, nil
	}
	store, err := s.catalogue.catalogue()
	if err != nil {
		return nil, err
	}
	roots, err := store.Roots()
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, root := range roots {
		hashes, err := store.HashesUnder(root.Path)
		if err != nil {
			return nil, err
		}
		for _, h := range hashes {
			known[h] = true
		}
	}
	return known, nil
}

// ImportPlan reports where a folder's frames are routed, how much of it is
// going nowhere, and what the routes land on. It reads the decision store and
// changes nothing.
func (s *ImportService) ImportPlan(dir string) (ImportPlanDTO, error) {
	resolved, groups, items, err := s.folder(dir, false)
	if err != nil {
		return ImportPlanDTO{}, err
	}
	r, err := planRules(s.app.Config())
	if err != nil {
		return ImportPlanDTO{}, err
	}
	p, err := buildPlan(items, r)
	if err != nil {
		return ImportPlanDTO{}, err
	}

	out := ImportPlanDTO{
		Dir:         resolved,
		Frames:      len(groups),
		Routes:      []ImportRouteDTO{},
		Verb:        routeVerb(r.moveOnImport),
		LibraryRoot: r.libraryRoot,
		Network:     platform.IsNetwork(resolved),
	}
	for _, it := range items {
		switch {
		case it.record.Destination != "" && it.record.Verdict == decide.Keep:
			out.Routed++
		case it.record.Verdict == decide.Cut:
			out.Cut++
		}
	}
	out.Unrouted = out.Frames - out.Routed - out.Cut
	out.Undecided = out.Frames - len(items)

	for _, d := range p.dto.Destinations {
		target, err := resolveDestination(d.Path, r.libraryRoot)
		if err != nil {
			return ImportPlanDTO{}, err
		}
		out.Routes = append(out.Routes, ImportRouteDTO{
			Destination: d.Path,
			Path:        target,
			Frames:      d.Frames,
			Files:       d.Files,
			Bytes:       d.Bytes,
		})
		out.Files += d.Files
		out.Bytes += d.Bytes
	}

	out.Space, err = s.destinationSpace(out.Routes)
	if err != nil {
		return ImportPlanDTO{}, err
	}
	return out, nil
}

// destinationSpace reports the volume behind every route, so the import screen
// can show what is about to land where before it lands.
func (s *ImportService) destinationSpace(routes []ImportRouteDTO) ([]DestinationSpaceDTO, error) {
	vols, err := s.volumes()
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	// Everything landing on one volume is weighed together: two destinations
	// that each fit but together do not is exactly the case worth catching.
	landing := map[string]int64{}
	out := make([]DestinationSpaceDTO, 0, len(routes))
	for _, route := range routes {
		row := DestinationSpaceDTO{
			Destination: route.Destination,
			Path:        route.Path,
			Frames:      route.Frames,
			Bytes:       route.Bytes,
			Fits:        true,
		}
		if v, ok := volumeFor(route.Path, vols); ok {
			row.Volume = v.Path
			row.VolumeName = v.Name
			row.Free = v.Free
			row.Total = v.Total
			row.Network = v.Network
			row.Removable = v.Removable
			landing[v.Path] += route.Bytes
		}
		out = append(out, row)
	}
	for i, row := range out {
		if row.Volume != "" && row.Free > 0 {
			out[i].Fits = landing[row.Volume] <= row.Free
		}
	}
	return out, nil
}

// Execute carries the folder's routed frames into the library, and optionally
// into a second copy at backupDest.
//
// Both copies are one journaled batch, so one undo takes the whole import back.
// The copying itself is the op engine's, unchanged: this composes the plan an
// apply would build, adds the backup leg to it, and hands the lot to the same
// executor with the same collision policy and the same verification.
func (s *ImportService) Execute(dir, backupDest string) (BatchDTO, error) {
	if !s.running.CompareAndSwap(false, true) {
		return BatchDTO{}, errors.New("an import is already running")
	}
	defer s.running.Store(false)

	batch, err := s.execute(dir, backupDest)
	if err != nil {
		s.report(ImportProgress{Dir: dir, Phase: ImportPhaseScan, Complete: true, Error: err.Error()})
	}
	return batch, err
}

func (s *ImportService) execute(dir, backupDest string) (BatchDTO, error) {
	resolved, _, items, err := s.folder(dir, true)
	if err != nil {
		return BatchDTO{}, err
	}
	cfg := s.app.Config()
	r, err := planRules(cfg)
	if err != nil {
		return BatchDTO{}, err
	}
	p, err := buildPlan(items, r)
	if err != nil {
		return BatchDTO{}, err
	}
	if len(p.actions) == 0 {
		// Nothing routed. The decisions still get their clearing pass, which is
		// what an apply of an empty plan does, and no batch is journaled.
		if err := NewApplyService(s.app).clearApplied(p.planned, journal.Batch{}); err != nil {
			return BatchDTO{}, err
		}
		s.report(ImportProgress{Dir: resolved, Phase: ImportPhaseCopy, Complete: true})
		return BatchDTO{Description: "Nothing to import"}, nil
	}

	backup, err := backupActions(p, backupDest)
	if err != nil {
		return BatchDTO{}, err
	}

	// A moving import takes the file off the card, so the second copy has to be
	// read before that happens: the backup leg goes first, and only then the
	// library leg. A copying import leaves the source alone and reads better
	// the other way round.
	libraryPhase := ImportPhaseCopy
	if r.moveOnImport {
		libraryPhase = ImportPhaseMove
	}
	actions, libraryAt := p.actions, 0
	firstPhase, lastPhase := libraryPhase, ImportPhaseBackup
	if r.moveOnImport && len(backup) > 0 {
		actions, libraryAt = append(append([]ops.FileAction{}, backup...), p.actions...), len(backup)
		firstPhase, lastPhase = ImportPhaseBackup, libraryPhase
	} else if len(backup) > 0 {
		actions = append(append([]ops.FileAction{}, p.actions...), backup...)
	} else {
		lastPhase = libraryPhase
	}

	trasher, err := s.app.trasher(resolved)
	if err != nil {
		return BatchDTO{}, err
	}
	jrnl, err := s.app.openJournal()
	if err != nil {
		return BatchDTO{}, err
	}

	sizes := map[string]int64{}
	for _, it := range p.planned {
		collectSizes(sizes, it.group)
	}
	firstLeg := countCopies(actions[:legEnd(actions, libraryAt, len(p.actions))])

	var files int
	var copied int64
	executor := &ops.Executor{
		Journal:   jrnl,
		Trasher:   trasher,
		Collision: cfg.Behaviour.CollisionPolicy,
		Verify:    cfg.Behaviour.VerifyCopies,
		// The executor has no progress hook, and the copier is the honest place
		// for one: it is called once per file, after the collision policy has
		// picked the name and before the batch records the outcome.
		Copier: func(src, dst string) error {
			err := platform.CopyFile(src, dst)
			files++
			if err == nil {
				copied += sizes[src]
			}
			phase := firstPhase
			if files > firstLeg {
				phase = lastPhase
			}
			s.report(ImportProgress{
				Dir: resolved, Phase: phase, Files: files, Total: len(actions), Bytes: copied,
			})
			return err
		},
	}

	description := importDescription(p, len(backup) > 0)
	batch, applyErr := executor.Apply(description, actions)

	// Only the library leg decides whether a frame is done: a backup copy that
	// landed for a frame whose library copy failed leaves the frame routed, so
	// the user can apply it again.
	library := journal.Batch{Actions: legRecords(batch, libraryAt, len(p.actions))}
	clearErr := NewApplyService(s.app).clearApplied(p.planned, library)

	s.report(ImportProgress{
		Dir: resolved, Phase: lastPhase, Files: len(actions), Total: len(actions),
		Bytes: copied, Complete: true, Error: errText(applyErr, clearErr),
	})
	if applyErr != nil {
		return batchDTO(batch), applyErr
	}
	return batchDTO(batch), clearErr
}

// folder scans one directory and pairs every frame with whatever the decision
// store holds for it, including the frames it holds nothing for — those are the
// count the unrouted warning is about.
//
// report is on only for an import that is running. Drawing the routing table
// is a read, and a progress bar that fills every time the user looks at a
// folder would be a bar that means nothing when files are actually moving.
func (s *ImportService) folder(dir string, report bool) (string, []scan.PhotoGroup, []planned, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return "", nil, nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, nil, fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return "", nil, nil, fmt.Errorf("%s is not a folder", resolved)
	}

	groups, err := scan.ScanDir(resolved, s.app.Config().ScanConfig())
	if err != nil {
		return "", nil, nil, fmt.Errorf("scan %s: %w", resolved, err)
	}
	network := platform.IsNetwork(resolved)
	var onHashed func(int)
	if report {
		s.report(ImportProgress{Dir: resolved, Phase: ImportPhaseScan, Total: len(groups)})
		onHashed = func(done int) {
			s.report(ImportProgress{Dir: resolved, Phase: ImportPhaseScan, Files: done, Total: len(groups)})
		}
	}
	hashes := hashGroups(groups, s.app.hashWorkers(network), onHashed)

	store, err := s.app.decisions()
	if err != nil {
		return "", nil, nil, err
	}
	var items []planned
	for i, g := range groups {
		h := hashes[i]
		if h == "" {
			continue
		}
		rec, ok, err := store.Get(h)
		if err != nil {
			return "", nil, nil, fmt.Errorf("read decisions: %w", err)
		}
		if !ok {
			continue
		}
		items = append(items, planned{group: g, hash: h, record: rec})
	}
	return resolved, groups, items, nil
}

// report publishes one progress report. It is serialised because the scan
// phase hashes in parallel and several workers report at once.
func (s *ImportService) report(p ImportProgress) {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	if s.onProgress != nil {
		s.onProgress(p)
		return
	}
	emitEvent(EventImportProgress, p)
}

// backupActions plans the second copy: the same frames, laid out the same way,
// rooted at the backup folder instead of the library.
//
// A library-relative destination keeps its shape, so a frame filed under
// 2026/portraits is found under 2026/portraits on the backup too, tokens
// expanding the same way on both. An absolute destination has no shape to
// keep and lands under its own last element, which is the part of it the user
// named.
func backupActions(p plan, backupDest string) ([]ops.FileAction, error) {
	if strings.TrimSpace(backupDest) == "" {
		return nil, nil
	}
	root, err := expandPath(backupDest)
	if err != nil {
		return nil, fmt.Errorf("backup destination: %w", err)
	}
	var out []ops.FileAction
	for _, it := range p.planned {
		if it.record.Destination == "" || it.record.Verdict != decide.Keep {
			continue
		}
		target, err := backupTarget(it.record.Destination, root)
		if err != nil {
			return nil, err
		}
		// Always a copy, whatever the import does to the card: a file can only
		// be moved once, and the second copy is the point of the backup.
		op := ops.CopyTo{Dest: target, Halves: ops.Halves(it.record.Mask), Metadata: frameMetadata}
		actions, err := op.Plan([]scan.PhotoGroup{it.group})
		if err != nil {
			return nil, fmt.Errorf("plan backup of %s: %w", it.group.Stem, err)
		}
		out = append(out, actions...)
	}
	return out, nil
}

// backupTarget re-roots one recorded destination under the backup folder.
func backupTarget(dest, backupRoot string) (string, error) {
	trimmed := strings.TrimSpace(dest)
	if trimmed == "" {
		return "", errors.New("a routed frame has no destination")
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "~") {
		expanded, err := expandPath(trimmed)
		if err != nil {
			return "", err
		}
		return filepath.Join(backupRoot, filepath.Base(expanded)), nil
	}
	return filepath.Join(backupRoot, trimmed), nil
}

// importDescription names the batch in the journal, which is what the undo
// prompt shows.
func importDescription(p plan, backup bool) string {
	description := p.dto.Description
	if backup {
		return description + ", with a second copy"
	}
	return description
}

// legEnd is where the first leg of a batch ends. The legs are contiguous:
// libraryAt is where the library actions start, so a library leg that starts at
// zero ends where it ends and one that starts later means the backup leg came
// first.
func legEnd(actions []ops.FileAction, libraryAt, libraryLen int) int {
	if libraryAt == 0 {
		return libraryLen
	}
	return libraryAt
}

// legRecords is the batch's records for the library leg. The executor records
// one action per action, in order, so the leg is a slice.
func legRecords(b journal.Batch, at, length int) []journal.Action {
	if at+length > len(b.Actions) {
		return b.Actions
	}
	return b.Actions[at : at+length]
}

// countCopies is how many of these actions go through the copier, which is what
// the progress counter can see.
func countCopies(actions []ops.FileAction) int {
	n := 0
	for _, a := range actions {
		if a.Verb == ops.VerbCopy {
			n++
		}
	}
	return n
}

func errText(errs ...error) string {
	joined := errors.Join(errs...)
	if joined == nil {
		return ""
	}
	return joined.Error()
}

// imageFolders is where a card keeps its frames: the subfolders of DCIM on a
// camera card, the volume itself on anything else. It reads two directories at
// most and never descends further — a camera writes into DCIM/<n>NAME and
// nowhere else, and a card is not a tree to be walked.
func imageFolders(root string) ([]string, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", root, err)
	}
	dcim := ""
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), "DCIM") {
			dcim = filepath.Join(root, e.Name())
			break
		}
	}
	if dcim == "" {
		return []string{root}, false, nil
	}

	inner, err := os.ReadDir(dcim)
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", dcim, err)
	}
	folders := []string{}
	for _, e := range inner {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		folders = append(folders, filepath.Join(dcim, e.Name()))
	}
	if len(folders) == 0 {
		// A DCIM with loose files in it. Rare, but a card is what it is.
		return []string{dcim}, true, nil
	}
	return folders, true, nil
}

// stride takes at most n items spread evenly through groups, so a sample of a
// card covers all of it rather than the front of it.
func stride(groups []scan.PhotoGroup, n int) []scan.PhotoGroup {
	if len(groups) <= n {
		return groups
	}
	step := float64(len(groups)) / float64(n)
	out := make([]scan.PhotoGroup, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, groups[int(float64(i)*step)])
	}
	return out
}

// countSessions clusters shot times into shoots, using the gap the catalogue
// uses so that a card and the library it lands in agree about what a session
// is. The sample is bounded: a card with more timestamps than this is clustered
// on an even spread of them, which moves the boundaries by at most one frame.
func countSessions(shots []time.Time) int {
	if len(shots) == 0 {
		return 0
	}
	sorted := append([]time.Time{}, shots...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	if len(sorted) > maxSampleTimes {
		step := float64(len(sorted)) / float64(maxSampleTimes)
		sample := make([]time.Time, 0, maxSampleTimes)
		for i := 0; i < maxSampleTimes; i++ {
			sample = append(sample, sorted[int(float64(i)*step)])
		}
		sorted = sample
	}

	sessions := 1
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Sub(sorted[i-1]) >= catalog.DefaultSessionGap {
			sessions++
		}
	}
	return sessions
}

// spanOf is the earliest and latest shot time in a set of frames, as RFC3339.
func spanOf(groups []scan.PhotoGroup) (first, last string) {
	var lo, hi time.Time
	for _, g := range groups {
		if g.Shot.IsZero() {
			continue
		}
		if lo.IsZero() || g.Shot.Before(lo) {
			lo = g.Shot
		}
		if hi.IsZero() || g.Shot.After(hi) {
			hi = g.Shot
		}
	}
	if lo.IsZero() {
		return "", ""
	}
	return lo.Format(time.RFC3339), hi.Format(time.RFC3339)
}

// frameFiles is how many files one frame is, sidecars included.
func frameFiles(g scan.PhotoGroup) int {
	n := len(g.Sidecars)
	for _, ref := range []*scan.FileRef{g.Raw, g.Jpeg} {
		if ref != nil {
			n++
		}
	}
	return n
}

// frameBytes is what one frame occupies, sidecars included.
func frameBytes(g scan.PhotoGroup) int64 {
	var total int64
	for _, ref := range []*scan.FileRef{g.Raw, g.Jpeg} {
		if ref != nil {
			total += ref.Size
		}
	}
	for _, ref := range g.Sidecars {
		total += ref.Size
	}
	return total
}

// volumeFor is the volume a path lands on: the longest mount point that
// contains it, since every path on a Unix-alike is also under "/".
func volumeFor(path string, vols []platform.Volume) (platform.Volume, bool) {
	var best platform.Volume
	found := false
	for _, v := range vols {
		if v.Path == "" || !underAnyPath(path, v.Path) {
			continue
		}
		if !found || len(v.Path) > len(best.Path) {
			best, found = v, true
		}
	}
	return best, found
}

// underAnyPath reports whether path is root or sits inside it.
func underAnyPath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	if root == string(filepath.Separator) {
		return strings.HasPrefix(path, root)
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// EventRejectsProgress carries RejectsProgress while rejects are being emptied.
const EventRejectsProgress = "rejects:progress"

// RejectsProgress reports how far an empty has got, so a folder holding
// thousands of rejects does not look like a hung window.
type RejectsProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// emptyRejectsDescription is what the journal calls the batch. It is written to
// be unmistakable in a history that is otherwise entirely reversible.
const emptyRejectsDescription = "EMPTY REJECTS — unrecoverable"

// howManyErrorsToReport caps what comes back to the dialog. A run that fails
// wholesale — a card pulled mid-empty — would otherwise return a line per file.
const howManyErrorsToReport = 20

// RejectedDirDTO is one folder's rejects: the counts and the bytes that
// emptying would destroy.
type RejectedDirDTO struct {
	// Dir is the culled folder; Path is its rejected subfolder, which is the
	// only thing this service ever reads or removes.
	Dir      string `json:"dir"`
	Path     string `json:"path"`
	Raw      int    `json:"raw"`
	Jpeg     int    `json:"jpeg"`
	Pairs    int    `json:"pairs"`
	Sidecars int    `json:"sidecars"`
	Other    int    `json:"other"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

// RejectsSurveyDTO is what emptying would destroy, in total and per folder.
// Nothing has been touched when it is returned.
type RejectsSurveyDTO struct {
	// Folder is the rejected folder name the survey looked for, so the dialog
	// can name it rather than assuming the stock one.
	Folder     string           `json:"folder"`
	Dirs       []RejectedDirDTO `json:"dirs"`
	Raw        int              `json:"raw"`
	Jpeg       int              `json:"jpeg"`
	Pairs      int              `json:"pairs"`
	Sidecars   int              `json:"sidecars"`
	Other      int              `json:"other"`
	Files      int              `json:"files"`
	TotalBytes int64            `json:"totalBytes"`
}

// RejectsResultDTO is what an empty actually did. BatchID is empty when there
// was nothing to destroy and so nothing was journalled.
type RejectsResultDTO struct {
	BatchID string   `json:"batchId"`
	Deleted int      `json:"deleted"`
	Failed  int      `json:"failed"`
	Bytes   int64    `json:"bytes"`
	Errors  []string `json:"errors"`
}

// RejectsService surveys and empties the _Rejected folders.
//
// This is the only destructive code in the application. Everything else moves
// files: a cull sends them to the OS trash or to a rejected folder, an import
// copies them, and undo puts them back. Emptying the rejects is the single
// sanctioned exception to the "deletion is never os.Remove" rule in CLAUDE.md,
// which is why it is a command of its own, shows the user what it is about to
// destroy first, and journals every file it removes even though no undo can
// act on that record.
//
// It reads and writes nothing outside the rejected subfolders of the folders it
// is handed.
type RejectsService struct {
	app *App

	// onProgress replaces the event emission in tests, the same seam the import
	// and catalogue passes use.
	onProgress func(RejectsProgress)

	// emptying holds the one empty at a time, the same single-flight guard
	// Import and Reindex carry. Two overlapping empties would have the second
	// surveying files the first is mid-way through destroying.
	emptying atomic.Bool
}

// NewRejectsService binds the service to the shared state.
func NewRejectsService(a *App) *RejectsService {
	return &RejectsService{app: a}
}

// Survey reports what emptying the rejects of dirs would destroy: counts by
// class, matched pairs, and the total bytes, per folder and overall. It has no
// side effects, and it never looks anywhere but the rejected subfolder of each
// folder it is given.
func (s *RejectsService) Survey(dirs []string) (RejectsSurveyDTO, error) {
	name, err := rejectedFolderName(s.app.Config())
	if err != nil {
		return RejectsSurveyDTO{}, err
	}
	folders, err := s.collectRejects(dirs, name)
	if err != nil {
		return RejectsSurveyDTO{}, err
	}

	out := RejectsSurveyDTO{Folder: name, Dirs: make([]RejectedDirDTO, 0, len(folders))}
	for _, f := range folders {
		out.Dirs = append(out.Dirs, f.dto)
		out.Raw += f.dto.Raw
		out.Jpeg += f.dto.Jpeg
		out.Pairs += f.dto.Pairs
		out.Sidecars += f.dto.Sidecars
		out.Other += f.dto.Other
		out.Files += f.dto.Files
		out.TotalBytes += f.dto.Bytes
	}
	return out, nil
}

// Empty permanently deletes every file inside the rejected folders of dirs and
// removes the folders themselves. The files do not go to the trash and they do
// not come back.
//
// A failed removal is recorded and the run carries on, in the same spirit as an
// apply: partial completion is a journalled fact rather than an exception.
func (s *RejectsService) Empty(dirs []string) (RejectsResultDTO, error) {
	if !s.emptying.CompareAndSwap(false, true) {
		return RejectsResultDTO{}, errors.New("the rejects are already being emptied")
	}
	defer s.emptying.Store(false)

	name, err := rejectedFolderName(s.app.Config())
	if err != nil {
		return RejectsResultDTO{}, err
	}
	folders, err := s.collectRejects(dirs, name)
	if err != nil {
		return RejectsResultDTO{}, err
	}

	total := 0
	for _, f := range folders {
		total += len(f.files)
	}
	if total == 0 {
		return RejectsResultDTO{}, nil
	}

	batch := journal.Batch{
		ID:          fmt.Sprintf("%d-rejects", time.Now().UnixNano()),
		Time:        time.Now(),
		Description: emptyRejectsDescription,
		Actions:     make([]journal.Action, 0, total),
	}
	var res RejectsResultDTO
	s.report(RejectsProgress{Done: 0, Total: total})
	for _, f := range folders {
		for _, file := range f.files {
			// The one sanctioned os.Remove in the application. Everything else
			// that removes a file moves it somewhere it can be recovered from;
			// this is the command whose whole purpose is that it cannot.
			rec := journal.Action{Verb: string(ops.VerbDestroy), Src: file.path}
			if err := os.Remove(file.path); err != nil {
				rec.Outcome = journal.OutcomeError
				rec.Err = err.Error()
				res.Failed++
				if len(res.Errors) < howManyErrorsToReport {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", file.path, err))
				}
			} else {
				rec.Outcome = journal.OutcomeOK
				res.Deleted++
				res.Bytes += file.size
			}
			batch.Actions = append(batch.Actions, rec)
			s.report(RejectsProgress{Done: res.Deleted + res.Failed, Total: total})
		}
		// The folders go once their contents have, deepest first. A folder that
		// still holds something — a file that would not remove — stays, and the
		// failure it stayed for is already in the batch.
		for _, d := range f.dirs {
			_ = os.Remove(d)
		}
	}

	jrnl, err := s.app.openJournal()
	if err != nil {
		return res, err
	}
	// The files have already gone, so a journal that will not write is reported
	// after the fact rather than instead of it.
	if err := jrnl.Append(batch); err != nil {
		return res, err
	}
	res.BatchID = batch.ID
	return res, nil
}

// report publishes one progress report.
func (s *RejectsService) report(p RejectsProgress) {
	if s.onProgress != nil {
		s.onProgress(p)
		return
	}
	emitEvent(EventRejectsProgress, p)
}

// rejectFile is one file inside a rejected folder, and its size, read once
// during the survey so the confirmation and the reclaimed total agree.
type rejectFile struct {
	path string
	size int64
}

// rejectsFolder is one folder's rejects: the files to destroy, the directories
// to remove afterwards (deepest first), and the counts the dialog shows.
type rejectsFolder struct {
	files []rejectFile
	dirs  []string
	dto   RejectedDirDTO
}

// rejectedFolderName is the subfolder the rejects live in, refusing any name
// that could reach outside the folder being emptied. The name comes from a
// hand-editable JSON file and this command deletes what it points at, so it is
// checked here rather than trusted from validation that ran at load time.
func rejectedFolderName(cfg config.Config) (string, error) {
	name := cfg.Behaviour.RejectedFolderName
	if name == "" {
		// Rejected-folder mode is a setting the user can turn off, and turning
		// it off does not empty the folders it already filled. The stock name is
		// what those folders are called.
		name = config.Default().Behaviour.RejectedFolderName
	}
	if name != filepath.Clean(name) || name == "." || name == ".." ||
		filepath.IsAbs(name) || strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return "", fmt.Errorf("rejected folder name %q is not a single folder name: refusing to empty anything", name)
	}
	return name, nil
}

// collectRejects surveys the rejected subfolder of each folder in dirs. A
// folder without one contributes nothing and is not an error — the command is
// offered over every open folder, and most have never been culled.
//
// Only dir/name is ever read. Nothing walks the folder being culled, and a
// dir/name that is a symlink rather than a real directory is left alone
// entirely: following it would count, and then destroy, files somewhere the
// user never pointed at.
func (s *RejectsService) collectRejects(dirs []string, name string) ([]rejectsFolder, error) {
	sc := s.app.Config().ScanConfig()
	seen := make(map[string]bool, len(dirs))
	var out []rejectsFolder
	for _, dir := range dirs {
		resolved, err := expandPath(dir)
		if err != nil {
			continue // an empty or unresolvable entry is not a folder to empty
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true

		path := filepath.Join(resolved, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsDir() {
			continue
		}
		folder, err := surveyRejected(resolved, path, sc)
		if err != nil {
			return nil, err
		}
		if folder.dto.Files == 0 && len(folder.dirs) <= 1 {
			// Nothing in it and nothing under it: there is nothing to show and
			// nothing to destroy. The empty folder itself is left where it is.
			continue
		}
		out = append(out, folder)
	}
	return out, nil
}

// surveyRejected walks one rejected folder and counts what is in it. Every
// file is counted, including the ones the app does not recognise as frames:
// the totals describe what emptying destroys, not what it understands.
func surveyRejected(dir, path string, sc scan.Config) (rejectsFolder, error) {
	folder := rejectsFolder{dto: RejectedDirDTO{Dir: dir, Path: path}}
	// Stems that hold each half, so a pair can be counted as a pair.
	raws := map[string]bool{}
	jpegs := map[string]bool{}

	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A folder that cannot be read is reported rather than silently
			// leaving files out of a total the user is about to agree to.
			return fmt.Errorf("read %s: %w", p, err)
		}
		if d.IsDir() {
			folder.dirs = append(folder.dirs, p)
			return nil
		}
		var size int64
		if info, err := d.Info(); err == nil {
			size = info.Size()
		}
		folder.files = append(folder.files, rejectFile{path: p, size: size})
		folder.dto.Files++
		folder.dto.Bytes += size

		name := d.Name()
		stem, class := classify(name, sc)
		switch class {
		case "raw":
			folder.dto.Raw++
			raws[stem] = true
		case "jpeg":
			folder.dto.Jpeg++
			jpegs[stem] = true
		case "sidecar":
			folder.dto.Sidecars++
		default:
			folder.dto.Other++
		}
		return nil
	})
	if err != nil {
		return rejectsFolder{}, err
	}

	for stem := range raws {
		if jpegs[stem] {
			folder.dto.Pairs++
		}
	}
	// Deepest first, so the folders come away once their contents have.
	sort.Slice(folder.dirs, func(i, j int) bool { return len(folder.dirs[i]) > len(folder.dirs[j]) })
	return folder, nil
}

// classify names the class of one file and the stem it would group under, from
// the configured extension lists — the same lists the scanner classifies by, so
// a RAW extension the user has added is a RAW here too.
//
// The stem is case-insensitive and drops the inner extension of a sidecar named
// after a whole file (DSCF1234.RAF.xmp), which is what the scanner groups by.
func classify(name string, sc scan.Config) (stem, class string) {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if inner := strings.ToLower(filepath.Ext(base)); inner != "" {
		if classOf(inner, sc) == "raw" || classOf(inner, sc) == "jpeg" {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return strings.ToLower(base), classOf(ext, sc)
}

// classOf is which extension list ext appears in, or "" for one the app does
// not recognise.
func classOf(ext string, sc scan.Config) string {
	for _, e := range sc.RawExts {
		if strings.EqualFold(e, ext) {
			return "raw"
		}
	}
	for _, e := range sc.JpegExts {
		if strings.EqualFold(e, ext) {
			return "jpeg"
		}
	}
	for _, e := range sc.SidecarExts {
		if strings.EqualFold(e, ext) {
			return "sidecar"
		}
	}
	return ""
}

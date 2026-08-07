package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
	"github.com/tomaszcichy9825/culler/internal/xmpexport"
)

// XMPExportResultDTO is what one export run did.
type XMPExportResultDTO struct {
	Dir string `json:"dir"`
	// Frames is how many frames the folder holds, Written how many sidecars
	// were created or updated, Cleared how many kept their file but lost our
	// fields, Skipped how many were left exactly as they were, and Failed how
	// many could not be written.
	Frames  int `json:"frames"`
	Written int `json:"written"`
	Cleared int `json:"cleared"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
	// Errors is one line per frame that could not be exported or could not be
	// read, capped so a whole unreadable card cannot flood the UI. Failed and
	// Skipped are the true counts.
	Errors []string `json:"errors"`
	// Description is the single line the toast shows.
	Description string `json:"description"`
}

// maxExportErrors is how many per-frame reasons come back. Enough to show a
// pattern, not enough to be a log file.
const maxExportErrors = 20

// XMPExportService writes culling decisions out as XMP sidecars.
//
// Nothing here runs on its own. The export is invoked, never automatic: the
// design is explicit that a sidecar written on every keystroke is slow and
// litters the folder (docs/DESIGN.md §3.3), so this exists as one folder-wide
// command and there is no hook from the decision path into it.
//
// It is also the one thing in this package that writes into the folder being
// culled, alongside the sidecar ExifService writes and the _Rejected folder.
// Its defences: the run is always explicit, and the writer only ever touches
// two fields of one file per frame.
//
// The run is not journalled and so is not undoable. There is nothing to undo:
// re-exporting writes the same bytes, and clearing a frame's decision takes
// our fields back out of the sidecar on the next run. Nothing this service
// writes can remove a file or a field that belongs to anything else.
type XMPExportService struct {
	app *App
}

// NewXMPExportService binds the service to the shared state.
func NewXMPExportService(a *App) *XMPExportService {
	return &XMPExportService{app: a}
}

// ExportFolder writes a sidecar beside every decided or rated frame in dir and
// reports what it did. It is always an explicit action — a button or a palette
// command — so the call is the consent, and nothing here consults a setting to
// allow it. The setting decides only whether an apply does this on its own.
//
// Every frame is visited, not only the decided ones: a frame whose decision
// has since been cleared has to have our fields taken back out of the sidecar
// it was given, and a frame that never had one is left with no file at all.
// One frame that cannot be written does not stop the rest; its reason comes
// back in the result.
func (s *XMPExportService) ExportFolder(dir string) (XMPExportResultDTO, error) {
	cfg := s.app.Config()

	resolved, err := expandPath(dir)
	if err != nil {
		return XMPExportResultDTO{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return XMPExportResultDTO{}, fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return XMPExportResultDTO{}, fmt.Errorf("%s is not a folder", resolved)
	}

	groups, err := scan.ScanDir(resolved, cfg.ScanConfig())
	if err != nil {
		return XMPExportResultDTO{}, fmt.Errorf("scan %s: %w", resolved, err)
	}
	store, err := s.app.decisions()
	if err != nil {
		return XMPExportResultDTO{}, err
	}
	// The same identity the grid reads decisions by, so the export writes the
	// verdicts the user is looking at rather than what a stem lookup guesses.
	hashes := hashGroups(groups, s.app.hashWorkers(platform.IsNetwork(resolved)), nil)

	out := XMPExportResultDTO{Dir: resolved, Frames: len(groups)}
	for i, g := range groups {
		if hashes[i] == "" {
			// Without an identity there is no way to know what was decided,
			// and an empty record would strip a sidecar this app wrote on a
			// guess. Leaving the frame alone is the only safe reading.
			out.Skipped++
			out.note(fmt.Sprintf("%s: could not be read, so its decision is unknown", g.Stem))
			continue
		}
		var rec decide.Record
		recorded, ok, err := store.Get(hashes[i], g.Dir, g.Stem)
		if err != nil {
			return out, fmt.Errorf("read decisions: %w", err)
		}
		if ok {
			rec = recorded
		}

		res, err := xmpexport.Write(g, rec)
		if err != nil {
			// The sidecar's name is what the user has to go and look at; a
			// frame with no file to write beside has only its stem.
			name := g.Stem
			if res.Path != "" {
				name = filepath.Base(res.Path)
			}
			out.Failed++
			out.note(fmt.Sprintf("%s: %v", name, err))
			continue
		}
		switch res.Action {
		case xmpexport.ActionWritten:
			out.Written++
		case xmpexport.ActionCleared:
			out.Cleared++
		default:
			out.Skipped++
		}
	}
	out.Description = describeExport(out)
	return out, nil
}

// note records a per-frame reason, up to the cap.
func (r *XMPExportResultDTO) note(line string) {
	if len(r.Errors) < maxExportErrors {
		r.Errors = append(r.Errors, line)
	}
}

// describeExport is the sentence the toast shows: what was written, and what
// went wrong if anything did.
func describeExport(r XMPExportResultDTO) string {
	if r.Written == 0 && r.Cleared == 0 && r.Failed == 0 {
		return "No decisions to export"
	}
	line := fmt.Sprintf("Exported %d %s", r.Written, pluralSidecars(r.Written))
	if r.Cleared > 0 {
		line += fmt.Sprintf(", cleared %d", r.Cleared)
	}
	if r.Failed > 0 {
		line += fmt.Sprintf(", %d could not be written", r.Failed)
	}
	return line
}

func pluralSidecars(n int) string {
	if n == 1 {
		return "sidecar"
	}
	return "sidecars"
}

package app

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/hash"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// FolderDTO is one opened directory and its frames.
type FolderDTO struct {
	Dir     string     `json:"dir"`
	Network bool       `json:"network"` // lives on a network volume
	Groups  []GroupDTO `json:"groups"`
}

// GroupDTO is a PhotoGroup flattened for the frontend: paths instead of file
// references, a formatted timestamp, and what is currently recorded for the
// frame.
type GroupDTO struct {
	Dir      string   `json:"dir"`
	Stem     string   `json:"stem"`
	Kind     string   `json:"kind"` // paired | jpeg-only | raw-only
	HasRaw   bool     `json:"hasRaw"`
	HasJpeg  bool     `json:"hasJpeg"`
	RawPath  string   `json:"rawPath"`  // empty when there is no RAW
	JpegPath string   `json:"jpegPath"` // empty when there is no JPEG
	Sidecars int      `json:"sidecars"`
	Shot     string   `json:"shot"` // RFC3339
	Warnings []string `json:"warnings"`
	Verdict  string   `json:"verdict"` // empty | keep | cut
	Mask     string   `json:"mask"`    // rj | r | j — which halves a keep holds on to
	Rating   int      `json:"rating"`  // 0-5, 0 is unrated
	Hash     string   `json:"hash"`    // identity of the primary file, empty if unreadable

	// Destination is where an apply would route this frame, empty when it
	// stays where it is. Library-relative or absolute, and possibly a token
	// template rather than a literal path.
	Destination string `json:"destination"`

	// Decision is the verdict and mask named in the pre-verdict vocabulary,
	// kept so the grid keeps rendering until it is restyled onto verdicts.
	Decision string `json:"decision"` // none | keep_all | drop_raw | drop_jpeg | drop_all
}

// LibraryService opens folders for the grid.
type LibraryService struct {
	app *App
}

// NewLibraryService binds the service to the shared state.
func NewLibraryService(a *App) *LibraryService {
	return &LibraryService{app: a}
}

// OpenFolder scans dir and returns its frames with the decision recorded for
// each. The path may be relative or start with ~.
func (s *LibraryService) OpenFolder(dir string) (FolderDTO, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return FolderDTO{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FolderDTO{}, fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return FolderDTO{}, fmt.Errorf("%s is not a folder", resolved)
	}

	network := platform.IsNetwork(resolved)
	groups, err := scan.ScanDir(resolved, s.app.Config().ScanConfig())
	if err != nil {
		return FolderDTO{}, fmt.Errorf("scan %s: %w", resolved, err)
	}
	hashes := hashGroups(groups, s.app.hashWorkers(network), func(done int) {
		emitEvent(EventScanProgress, ScanProgress{Dir: resolved, Done: done, Total: len(groups)})
	})

	store, err := s.app.decisions()
	if err != nil {
		return FolderDTO{}, err
	}

	out := FolderDTO{
		Dir:     resolved,
		Network: network,
		Groups:  make([]GroupDTO, 0, len(groups)),
	}
	for i, g := range groups {
		var rec decide.Record
		if hashes[i] != "" {
			recorded, ok, err := store.Get(hashes[i])
			if err != nil {
				return FolderDTO{}, fmt.Errorf("read decisions: %w", err)
			}
			if ok {
				rec = recorded
			}
		}
		out.Groups = append(out.Groups, groupDTO(g, hashes[i], rec))
	}
	return out, nil
}

// groupDTO flattens one group. A frame whose primary file could not be
// hashed still shows up — it can be moved and deleted like any other — but it
// carries a warning, because without an identity its decision cannot be
// remembered across a reopen.
func groupDTO(g scan.PhotoGroup, hash string, rec decide.Record) GroupDTO {
	dto := GroupDTO{
		Dir:         g.Dir,
		Stem:        g.Stem,
		Kind:        g.Kind.String(),
		HasRaw:      g.Raw != nil,
		HasJpeg:     g.Jpeg != nil,
		Sidecars:    len(g.Sidecars),
		Shot:        g.Shot.Format(time.RFC3339),
		Warnings:    append([]string{}, g.Warnings...),
		Verdict:     string(rec.Verdict),
		Mask:        string(rec.Mask),
		Rating:      rec.Rating,
		Destination: rec.Destination,
		Hash:        hash,
		Decision:    legacyDecision(rec),
	}
	if g.Raw != nil {
		dto.RawPath = g.Raw.Path
	}
	if g.Jpeg != nil {
		dto.JpegPath = g.Jpeg.Path
	}
	if hash == "" {
		dto.Warnings = append(dto.Warnings, "could not read this frame's primary file; its decision will not be remembered")
	}
	return dto
}

// primaryRef is the file a frame is identified by: the JPEG when there is
// one, because that is the frame the user is looking at, otherwise the RAW.
func primaryRef(g scan.PhotoGroup) *scan.FileRef {
	if g.Jpeg != nil {
		return g.Jpeg
	}
	return g.Raw
}

// hashGroups returns the identity hash of every group's primary file, aligned
// with groups, using the empty string where the file could not be read. The
// caller picks the worker count: CPUs for local sources, a low configured cap
// for network volumes where parallel head reads stall the share. progress is
// called with the completed count, throttled to every few frames.
func hashGroups(groups []scan.PhotoGroup, workers int, progress func(done int)) []string {
	if workers < 1 {
		workers = 1
	}
	hashes := make([]string, len(groups))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var done atomic.Int64
	for i, g := range groups {
		ref := primaryRef(g)
		if ref == nil {
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if h, err := hash.Content(path); err == nil {
				hashes[i] = h
			}
			if n := done.Add(1); progress != nil && (n%16 == 0 || int(n) == len(groups)) {
				progress(int(n))
			}
		}(i, ref.Path)
	}
	wg.Wait()
	return hashes
}

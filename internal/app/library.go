package app

import (
	"context"
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

	// The streamed open reaches the outside world through these, so it can be
	// exercised without a running Wails application or a real card. Production
	// values are set in NewLibraryService.
	emit   func(name string, data any)
	hashFn func(path string) (string, error)
	// workers overrides the identity-hash concurrency; zero takes the cap that
	// suits the volume the folder is on.
	workers int
	// batch bounds how many frames one emitted batch carries.
	batch streamBatching

	// mu guards the open currently in flight. Only the newest open is allowed
	// to emit, so a folder switch cannot have a slow scan land on top of it.
	mu     sync.Mutex
	seq    int64
	cancel context.CancelFunc
}

// NewLibraryService binds the service to the shared state.
func NewLibraryService(a *App) *LibraryService {
	return &LibraryService{app: a, emit: emitEvent, hashFn: hash.Content}
}

// OpenFolder scans dir and returns its frames with the decision recorded for
// each. The path may be relative or start with ~.
func (s *LibraryService) OpenFolder(dir string) (FolderDTO, error) {
	resolved, err := resolveFolder(dir)
	if err != nil {
		return FolderDTO{}, err
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

// resolveFolder turns what the user typed into an absolute directory. It is
// the one filesystem call a folder open makes before it commits to anything.
func resolveFolder(dir string) (string, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", resolved)
	}
	return resolved, nil
}

// frameDTO flattens what the walk alone knows about a group: everything
// except the frame's identity and whatever has been recorded against it. It is
// what the grid paints from while the hashes are still being read.
func frameDTO(g scan.PhotoGroup) GroupDTO {
	dto := GroupDTO{
		Dir:      g.Dir,
		Stem:     g.Stem,
		Kind:     g.Kind.String(),
		HasRaw:   g.Raw != nil,
		HasJpeg:  g.Jpeg != nil,
		Sidecars: len(g.Sidecars),
		Shot:     g.Shot.Format(time.RFC3339),
		Warnings: append([]string{}, g.Warnings...),
		Decision: legacyDecision(decide.Record{}),
	}
	if g.Raw != nil {
		dto.RawPath = g.Raw.Path
	}
	if g.Jpeg != nil {
		dto.JpegPath = g.Jpeg.Path
	}
	return dto
}

// frameIdentity is the half of a frame that only exists once its primary file
// has been read: the hash and what the store remembers under it. A frame whose
// primary file could not be hashed still shows up — it can be moved and
// deleted like any other — but it carries a warning, because without an
// identity its decision cannot be remembered across a reopen.
func frameIdentity(g scan.PhotoGroup, hash string, rec decide.Record) FrameHash {
	id := FrameHash{
		Dir:         g.Dir,
		Stem:        g.Stem,
		Hash:        hash,
		Verdict:     string(rec.Verdict),
		Mask:        string(rec.Mask),
		Rating:      rec.Rating,
		Destination: rec.Destination,
		Decision:    legacyDecision(rec),
		Warnings:    append([]string{}, g.Warnings...),
	}
	if hash == "" {
		id.Warnings = append(id.Warnings, "could not read this frame's primary file; its decision will not be remembered")
	}
	return id
}

// groupDTO flattens one group with its identity already resolved, which is
// what the unstreamed open hands back.
func groupDTO(g scan.PhotoGroup, hash string, rec decide.Record) GroupDTO {
	dto := frameDTO(g)
	id := frameIdentity(g, hash, rec)
	dto.Hash = id.Hash
	dto.Verdict = id.Verdict
	dto.Mask = id.Mask
	dto.Rating = id.Rating
	dto.Destination = id.Destination
	dto.Decision = id.Decision
	dto.Warnings = id.Warnings
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

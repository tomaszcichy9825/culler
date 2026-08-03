package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/catalog"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// catalogFile is the library index, in the app data directory. It is never
// written to the card: the catalogue is a cache of what was seen, and a lost
// one costs a reindex and nothing else.
const catalogFile = "catalog.db"

// EventCatalogProgress carries CatalogProgress while a root is being indexed.
const EventCatalogProgress = "catalog:progress"

// CatalogProgress reports how far an index pass has got. Dirs and Frames are
// cumulative and only climb. There is no total: counting the tree first would
// mean reading every directory twice, and on a card reader that is the
// expensive half of the whole operation, so the UI shows an indeterminate
// INDEXING chip rather than a bar that lies.
//
// The last report of a pass carries Done, and Error when the pass failed.
type CatalogProgress struct {
	Root   string `json:"root"`
	Dir    string `json:"dir"`
	Dirs   int    `json:"dirs"`
	Frames int    `json:"frames"`
	Done   bool   `json:"done"`
	Error  string `json:"error"`
}

func init() {
	// Registration gives the binding generator a typed JS/TS API for the event.
	application.RegisterEvent[CatalogProgress](EventCatalogProgress)
}

// RootDTO is one folder the catalogue covers.
type RootDTO struct {
	Path        string `json:"path"`
	Volume      string `json:"volume"`
	Added       string `json:"added"`       // RFC3339
	LastIndexed string `json:"lastIndexed"` // RFC3339, empty when never indexed
	Frames      int    `json:"frames"`
	RawBytes    int64  `json:"rawBytes"`
	JpegBytes   int64  `json:"jpegBytes"`
	Bytes       int64  `json:"bytes"`
}

// FrameDTO is one catalogued frame. It is deliberately not a GroupDTO: the
// catalogue knows what it recorded at index time, not what is on the disk this
// second, and the two must not be mistaken for each other.
//
// The paths carry the same fields the preview route needs, under the same
// names GroupDTO uses, so the frontend's previewURL reads a catalogued frame
// the same way it reads an open one.
type FrameDTO struct {
	Hash      string `json:"hash"`
	Dir       string `json:"dir"`
	Stem      string `json:"stem"`
	Kind      string `json:"kind"` // paired | jpeg-only | raw-only
	Shot      string `json:"shot"` // RFC3339
	HasRaw    bool   `json:"hasRaw"`
	HasJpeg   bool   `json:"hasJpeg"`
	RawPath   string `json:"rawPath"`
	JpegPath  string `json:"jpegPath"`
	Verdict   string `json:"verdict"`
	Rating    int    `json:"rating"`
	RawBytes  int64  `json:"rawBytes"`
	JpegBytes int64  `json:"jpegBytes"`
	Bytes     int64  `json:"bytes"`
}

// FacetsDTO is the facet chip row, as the frontend holds it. Every empty field
// is "no opinion".
type FacetsDTO struct {
	Kind      string `json:"kind"`
	Verdict   string `json:"verdict"` // keep | cut | undecided
	MinRating int    `json:"minRating"`
	Root      string `json:"root"`
	From      string `json:"from"` // RFC3339, inclusive
	To        string `json:"to"`   // RFC3339, exclusive
}

// SearchDTO is one page of results plus what the title bar prints above them.
type SearchDTO struct {
	Frames []FrameDTO `json:"frames"`
	Total  int        `json:"total"`
	Offset int        `json:"offset"`
	// Elapsed is the query's own time in milliseconds — the "38 ms" beside the
	// result count, which is a measurement and never an estimate.
	Elapsed int64 `json:"elapsed"`
}

// FacetCountDTO is one row of a facet list: the value, what it is called and
// how many frames are behind it.
type FacetCountDTO struct {
	Value  string `json:"value"`
	Label  string `json:"label"`
	Frames int    `json:"frames"`
}

// CountsDTO is what the facet lists are drawn from. Every value the facet can
// take is listed, including the ones holding nothing, so the list does not
// reflow under the pointer as the user narrows the search.
type CountsDTO struct {
	Total    int             `json:"total"`
	Kinds    []FacetCountDTO `json:"kinds"`
	Verdicts []FacetCountDTO `json:"verdicts"`
	Ratings  []FacetCountDTO `json:"ratings"`
}

// SessionDTO is one shoot.
type SessionDTO struct {
	ID          string `json:"id"`
	Start       string `json:"start"` // RFC3339
	End         string `json:"end"`
	SpanMinutes int    `json:"spanMinutes"`
	Frames      int    `json:"frames"`
	Kept        int    `json:"kept"`
	Cut         int    `json:"cut"`
	Undecided   int    `json:"undecided"`
	RawBytes    int64  `json:"rawBytes"`
	JpegBytes   int64  `json:"jpegBytes"`
	Bytes       int64  `json:"bytes"`
	Source      string `json:"source"` // the folder's own name
	Dir         string `json:"dir"`    // the folder it came from, in full
	Dirs        int    `json:"dirs"`
}

// StorageRootDTO is what one root holds.
type StorageRootDTO struct {
	Root      string `json:"root"`
	Volume    string `json:"volume"`
	Frames    int    `json:"frames"`
	RawBytes  int64  `json:"rawBytes"`
	JpegBytes int64  `json:"jpegBytes"`
	Bytes     int64  `json:"bytes"`
}

// StorageVolumeDTO is the card the storage view draws per volume.
type StorageVolumeDTO struct {
	Volume    string   `json:"volume"`
	Frames    int      `json:"frames"`
	RawBytes  int64    `json:"rawBytes"`
	JpegBytes int64    `json:"jpegBytes"`
	Bytes     int64    `json:"bytes"`
	Roots     []string `json:"roots"`
}

// StorageDTO is the whole catalogue's footprint.
//
// Every number is of what the catalogue holds, not of what the disk holds:
// nothing here stats the filesystem, so there is no free space and no total
// capacity, and the view must not invent either.
type StorageDTO struct {
	Frames    int                `json:"frames"`
	RawBytes  int64              `json:"rawBytes"`
	JpegBytes int64              `json:"jpegBytes"`
	Bytes     int64              `json:"bytes"`
	Roots     []StorageRootDTO   `json:"roots"`
	Volumes   []StorageVolumeDTO `json:"volumes"`
}

// LibraryIndexService is LIBRARY mode's end of the catalogue: the roots it
// covers, the searches over them, the sessions they group into and what they
// are holding on disk.
//
// It is separate from LibraryService, which opens one folder for the grid.
// That service answers "what is in front of me"; this one answers "what have I
// got", and the two share nothing but the app.
type LibraryIndexService struct {
	app *App

	mu    sync.Mutex
	store *catalog.Store

	// indexing guards the walk: one pass at a time, because two walks over the
	// same card only make both slower.
	indexing atomic.Bool
	running  sync.WaitGroup

	// onProgress replaces the event emission in tests, which run without a
	// Wails application to emit into.
	onProgress func(CatalogProgress)
}

// NewLibraryIndexService binds the service to the shared state.
func NewLibraryIndexService(a *App) *LibraryIndexService {
	return &LibraryIndexService{app: a}
}

// catalogue opens the index on first use, so an app that never visits LIBRARY
// never creates the file.
func (s *LibraryIndexService) catalogue() (*catalog.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil {
		return s.store, nil
	}
	if err := os.MkdirAll(s.app.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create app data dir %s: %w", s.app.dataDir, err)
	}
	store, err := catalog.Open(filepath.Join(s.app.dataDir, catalogFile))
	if err != nil {
		return nil, err
	}
	s.store = store
	return store, nil
}

// Close waits for a running index pass and releases the catalogue. It is safe
// on a service whose catalogue was never opened.
func (s *LibraryIndexService) Close() error {
	s.running.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return nil
	}
	err := s.store.Close()
	s.store = nil
	return err
}

// RegisterRoot adds a folder to the catalogue and returns the new root list.
// It does not index: indexing is a walk over a card, and the UI starts it
// deliberately with Reindex so it can show the progress.
func (s *LibraryIndexService) RegisterRoot(dir string) ([]RootDTO, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	store, err := s.catalogue()
	if err != nil {
		return nil, err
	}
	if _, err := store.AddRoot(resolved); err != nil {
		return nil, err
	}
	return s.Roots()
}

// RemoveRoot forgets a root and the frames only it covered, and returns what
// is left. Nothing on disk is touched.
func (s *LibraryIndexService) RemoveRoot(dir string) ([]RootDTO, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	store, err := s.catalogue()
	if err != nil {
		return nil, err
	}
	if err := store.RemoveRoot(resolved); err != nil {
		return nil, err
	}
	return s.Roots()
}

// Roots is every folder the catalogue covers, with what it currently holds.
func (s *LibraryIndexService) Roots() ([]RootDTO, error) {
	store, err := s.catalogue()
	if err != nil {
		return nil, err
	}
	roots, err := store.Roots()
	if err != nil {
		return nil, err
	}
	out := make([]RootDTO, 0, len(roots))
	for _, r := range roots {
		out = append(out, RootDTO{
			Path:        r.Path,
			Volume:      r.Volume,
			Added:       stamp(r.AddedAt),
			LastIndexed: stamp(r.LastIndexedAt),
			Frames:      r.Frames,
			RawBytes:    r.RawBytes,
			JpegBytes:   r.JpegBytes,
			Bytes:       r.RawBytes + r.JpegBytes,
		})
	}
	return out, nil
}

// Indexing reports whether a pass is running, which is what puts the amber
// INDEXING chip in the title bar.
func (s *LibraryIndexService) Indexing() bool {
	return s.indexing.Load()
}

// Reindex walks a root in the background and returns at once. Progress arrives
// on EventCatalogProgress, and so does the outcome: the last report carries
// Done, with Error set when the pass failed. An empty dir reindexes every
// registered root.
//
// Only one pass runs at a time; asking for a second while one is going is an
// error rather than a queue.
func (s *LibraryIndexService) Reindex(dir string) error {
	root := ""
	if dir != "" {
		resolved, err := expandPath(dir)
		if err != nil {
			return err
		}
		root = resolved
	}
	if _, err := s.catalogue(); err != nil {
		return err
	}
	if !s.indexing.CompareAndSwap(false, true) {
		return errors.New("an index pass is already running")
	}

	s.running.Add(1)
	go func() {
		defer s.running.Done()
		defer s.indexing.Store(false)
		if err := s.reindex(root); err != nil {
			s.report(CatalogProgress{Root: root, Done: true, Error: err.Error()})
		}
	}()
	return nil
}

// reindex walks one root, or every registered root when root is empty, and
// reports as it goes. It runs on the caller's goroutine; Reindex is the
// version the frontend calls.
func (s *LibraryIndexService) reindex(root string) error {
	store, err := s.catalogue()
	if err != nil {
		return err
	}
	decisions, err := s.app.decisions()
	if err != nil {
		return err
	}

	opts := catalog.IndexOptions{
		Scan: s.app.Config().ScanConfig(),
		// A network share stalls under parallel head reads, so the catalogue
		// takes the same low worker cap a folder open does.
		Workers: s.app.hashWorkers(root != "" && platform.IsNetwork(root)),
		Lookup: func(hash string) (string, int) {
			rec, ok, err := decisions.Get(hash)
			if err != nil || !ok {
				return "", 0
			}
			return string(rec.Verdict), rec.Rating
		},
		Progress: func(p catalog.Progress) {
			s.report(CatalogProgress{
				Root:   p.Root,
				Dir:    p.Dir,
				Dirs:   p.Dirs,
				Frames: p.Frames,
				Done:   p.Done,
			})
		},
	}

	if root == "" {
		_, err = store.IndexAll(nil, opts)
	} else {
		_, err = store.Index(root, opts)
	}
	return err
}

// report publishes one progress record, through the test seam when there is
// one and to the webview otherwise.
func (s *LibraryIndexService) report(p CatalogProgress) {
	if s.onProgress != nil {
		s.onProgress(p)
		return
	}
	emitEvent(EventCatalogProgress, p)
}

// Search returns one page of frames, newest first. A limit of zero returns
// everything, which the facet counts want and the results grid does not.
func (s *LibraryIndexService) Search(query string, facets FacetsDTO, limit, offset int) (SearchDTO, error) {
	store, err := s.catalogue()
	if err != nil {
		return SearchDTO{}, err
	}
	f, err := parseFacets(facets)
	if err != nil {
		return SearchDTO{}, err
	}

	started := time.Now()
	res, err := store.Search(query, f, catalog.Page{Limit: limit, Offset: offset})
	if err != nil {
		return SearchDTO{}, err
	}

	out := SearchDTO{
		Frames:  make([]FrameDTO, 0, len(res.Frames)),
		Total:   res.Total,
		Offset:  res.Offset,
		Elapsed: time.Since(started).Milliseconds(),
	}
	for _, f := range res.Frames {
		out.Frames = append(out.Frames, FrameDTO{
			Hash:      f.Hash,
			Dir:       f.Dir,
			Stem:      f.Stem,
			Kind:      f.Kind,
			Shot:      stamp(f.Shot),
			HasRaw:    f.RawPath != "",
			HasJpeg:   f.JpegPath != "",
			RawPath:   f.RawPath,
			JpegPath:  f.JpegPath,
			Verdict:   f.Verdict,
			Rating:    f.Rating,
			RawBytes:  f.RawBytes,
			JpegBytes: f.JpegBytes,
			Bytes:     f.Bytes(),
		})
	}
	return out, nil
}

// The facet values the lists offer, in the order they are drawn.
var (
	facetKinds    = []string{"paired", "raw-only", "jpeg-only"}
	facetVerdicts = []string{"keep", "cut", "undecided"}
	facetRatings  = []int{1, 2, 3, 4, 5}
)

// Counts totals each facet's values under the current query, for the meters in
// the facet lists.
func (s *LibraryIndexService) Counts(query string, facets FacetsDTO) (CountsDTO, error) {
	store, err := s.catalogue()
	if err != nil {
		return CountsDTO{}, err
	}
	f, err := parseFacets(facets)
	if err != nil {
		return CountsDTO{}, err
	}
	counts, err := store.Counts(query, f)
	if err != nil {
		return CountsDTO{}, err
	}

	out := CountsDTO{Total: counts.Total}
	for _, kind := range facetKinds {
		out.Kinds = append(out.Kinds, FacetCountDTO{Value: kind, Label: kind, Frames: counts.Kinds[kind]})
	}
	for _, verdict := range facetVerdicts {
		out.Verdicts = append(out.Verdicts,
			FacetCountDTO{Value: verdict, Label: verdict, Frames: counts.Verdicts[verdict]})
	}
	for _, rating := range facetRatings {
		out.Ratings = append(out.Ratings, FacetCountDTO{
			Value:  strconv.Itoa(rating),
			Label:  strconv.Itoa(rating) + "+",
			Frames: counts.Ratings[rating],
		})
	}
	return out, nil
}

// Sessions groups the catalogue into shoots, newest first. gapHours is how
// long a break has to be to end one; zero takes the four-hour default.
func (s *LibraryIndexService) Sessions(gapHours float64) ([]SessionDTO, error) {
	store, err := s.catalogue()
	if err != nil {
		return nil, err
	}
	sessions, err := store.Sessions(time.Duration(gapHours * float64(time.Hour)))
	if err != nil {
		return nil, err
	}
	out := make([]SessionDTO, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, SessionDTO{
			ID:          sess.ID,
			Start:       stamp(sess.Start),
			End:         stamp(sess.End),
			SpanMinutes: int(sess.Span().Minutes()),
			Frames:      sess.Frames,
			Kept:        sess.Kept,
			Cut:         sess.Cut,
			Undecided:   sess.Undecided,
			RawBytes:    sess.RawBytes,
			JpegBytes:   sess.JpegBytes,
			Bytes:       sess.RawBytes + sess.JpegBytes,
			Source:      sess.Source,
			Dir:         sess.Dir,
			Dirs:        sess.Dirs,
		})
	}
	return out, nil
}

// Storage totals the catalogue by root and rolls those up by volume.
func (s *LibraryIndexService) Storage() (StorageDTO, error) {
	store, err := s.catalogue()
	if err != nil {
		return StorageDTO{}, err
	}
	summary, err := store.StorageSummary()
	if err != nil {
		return StorageDTO{}, err
	}

	out := StorageDTO{
		Frames:    summary.Frames,
		RawBytes:  summary.RawBytes,
		JpegBytes: summary.JpegBytes,
		Bytes:     summary.RawBytes + summary.JpegBytes,
		Roots:     make([]StorageRootDTO, 0, len(summary.Roots)),
		Volumes:   make([]StorageVolumeDTO, 0, len(summary.Volumes)),
	}
	for _, r := range summary.Roots {
		out.Roots = append(out.Roots, StorageRootDTO{
			Root:      r.Root,
			Volume:    r.Volume,
			Frames:    r.Frames,
			RawBytes:  r.RawBytes,
			JpegBytes: r.JpegBytes,
			Bytes:     r.Bytes(),
		})
	}
	for _, v := range summary.Volumes {
		out.Volumes = append(out.Volumes, StorageVolumeDTO{
			Volume:    v.Volume,
			Frames:    v.Frames,
			RawBytes:  v.RawBytes,
			JpegBytes: v.JpegBytes,
			Bytes:     v.Bytes(),
			Roots:     append([]string{}, v.Roots...),
		})
	}
	return out, nil
}

// parseFacets turns the frontend's strings into the catalogue's types. A date
// that cannot be read is an error rather than an ignored filter: silently
// searching everything when the user asked for a week is worse than saying no.
func parseFacets(f FacetsDTO) (catalog.Facets, error) {
	out := catalog.Facets{
		Kind:      f.Kind,
		Verdict:   f.Verdict,
		MinRating: f.MinRating,
		Root:      f.Root,
	}
	if f.Root != "" {
		resolved, err := expandPath(f.Root)
		if err != nil {
			return catalog.Facets{}, err
		}
		out.Root = resolved
	}
	for _, bound := range []struct {
		name  string
		text  string
		field *time.Time
	}{
		{"from", f.From, &out.From},
		{"to", f.To, &out.To},
	} {
		if bound.text == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, bound.text)
		if err != nil {
			return catalog.Facets{}, fmt.Errorf("catalogue %s date %q: %w", bound.name, bound.text, err)
		}
		*bound.field = t
	}
	return out, nil
}

// stamp formats a time for the frontend, with the zero time as the empty
// string rather than the year 1.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

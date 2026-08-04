package app

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventMapProgress carries MapProgress while a folder's positions are read.
const EventMapProgress = "map:progress"

// MapProgress reports how far a Positions call has got. Total is the number of
// frames the scan found, Done the number whose metadata has been read. Unlike
// an index pass this one does know its total, because the folder is read
// before a single file is opened, so the UI can draw a real bar.
type MapProgress struct {
	Dir   string `json:"dir"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func init() {
	// Registration gives the binding generator a typed JS/TS API for the event.
	application.RegisterEvent[MapProgress](EventMapProgress)
}

// PositionDTO is one frame that carries coordinates.
//
// The path fields use the names GroupDTO uses, so the frontend's previewURL
// reads a positioned frame exactly as it reads an open one. There is no hash:
// working one out is a full read of every file in the folder, which is the
// expensive half of opening a card and far too much to pay for a map. A pane
// that wants the grid-sized thumbnail joins these back onto the open folder's
// frames on (dir, stem), which is the same key the scanner groups on.
type PositionDTO struct {
	Dir      string `json:"dir"`
	Stem     string `json:"stem"`
	Kind     string `json:"kind"` // paired | jpeg-only | raw-only
	HasRaw   bool   `json:"hasRaw"`
	HasJpeg  bool   `json:"hasJpeg"`
	RawPath  string `json:"rawPath"`
	JpegPath string `json:"jpegPath"`
	// Shot is RFC3339: the capture time the camera recorded, falling back to
	// the modification time the scan found. It is what orders the track.
	Shot string `json:"shot"`
	// Latitude and Longitude are already out of degrees/minutes/seconds and
	// signed by their hemisphere reference.
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// Altitude is metres above sea level, negative below it, and only means
	// anything when HasAltitude says the frame carried one.
	Altitude    float64 `json:"altitude"`
	HasAltitude bool    `json:"hasAltitude"`
}

// PositionsDTO is one folder's frames that have a position, and an account of
// the ones that do not.
//
// Total is every frame the scan found. Positioned, Unpositioned and Unreadable
// add up to it: a frame either gave up coordinates, was read and had none, or
// could not be read at all. The map draws the first, counts the second — the
// "n frames have no GPS" chip — and must not silently fold the third into
// either, because "no position recorded" and "this file would not open" are
// different things to be told.
type PositionsDTO struct {
	Dir          string        `json:"dir"`
	Frames       []PositionDTO `json:"frames"`
	Total        int           `json:"total"`
	Positioned   int           `json:"positioned"`
	Unpositioned int           `json:"unpositioned"`
	Unreadable   int           `json:"unreadable"`
}

// MapService answers where a folder's photographs were taken.
//
// It reads one folder at a time, on demand. The catalogue records what it saw
// at index time and coordinates are not among the things it records, so there
// is no catalogue-wide answer to give: a map over forty thousand indexed frames
// would mean opening forty thousand files. Reading GPS at index time and
// storing it beside the rest of a catalogued frame is the way that gets fixed,
// and it belongs to whoever owns the index — until then MAP mode covers the
// open folder and says so.
//
// Nothing here writes: geotagging, stripping GPS and the write plan behind them
// all go through ExifService, which already backs up what it replaces.
type MapService struct {
	app *App

	// onProgress replaces the event emission in tests, which run without a
	// Wails application to emit into.
	onProgress func(MapProgress)
}

// NewMapService binds the service to the shared state.
func NewMapService(a *App) *MapService {
	return &MapService{app: a}
}

// Positions reads the coordinates of every frame in dir. The path may be
// relative or start with ~.
//
// Frames come back in capture order, oldest first, which is the order the track
// layout draws them in and the order two frames from the same second have to
// agree on between calls. A frame whose file cannot be read is counted and
// dropped rather than taking the folder down with it.
//
// Progress arrives on EventMapProgress as the reads complete.
func (s *MapService) Positions(dir string) (PositionsDTO, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return PositionsDTO{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return PositionsDTO{}, fmt.Errorf("open folder: %w", err)
	}
	if !info.IsDir() {
		return PositionsDTO{}, fmt.Errorf("%s is not a folder", resolved)
	}

	groups, err := scan.ScanDir(resolved, s.app.Config().ScanConfig())
	if err != nil {
		return PositionsDTO{}, fmt.Errorf("scan %s: %w", resolved, err)
	}

	out := PositionsDTO{Dir: resolved, Total: len(groups), Frames: []PositionDTO{}}
	if len(groups) == 0 {
		s.report(MapProgress{Dir: resolved, Done: 0, Total: 0})
		return out, nil
	}

	// One read per frame, the same head read the EXIF pane makes, at the same
	// concurrency a folder open hashes with: a network share stalls under
	// parallel head reads whatever is being read for.
	workers := s.app.hashWorkers(platform.IsNetwork(resolved))
	read := readPositions(groups, workers, func(done int) {
		s.report(MapProgress{Dir: resolved, Done: done, Total: len(groups)})
	})

	for i, r := range read {
		switch {
		case r.err != nil:
			out.Unreadable++
		case !r.gps.Present:
			out.Unpositioned++
		default:
			out.Positioned++
			out.Frames = append(out.Frames, positionDTO(groups[i], r))
		}
	}

	sort.SliceStable(out.Frames, func(a, b int) bool {
		if out.Frames[a].Shot != out.Frames[b].Shot {
			return out.Frames[a].Shot < out.Frames[b].Shot
		}
		return out.Frames[a].Stem < out.Frames[b].Stem
	})
	return out, nil
}

// reading is what one frame's metadata read produced.
type reading struct {
	gps  exif.GPS
	shot time.Time
	err  error
}

// readPositions reads every group's primary file, aligned with groups. The
// caller picks the worker count; progress is called with the completed count,
// throttled the way a folder open throttles its hashing.
func readPositions(groups []scan.PhotoGroup, workers int, progress func(done int)) []reading {
	if workers < 1 {
		workers = 1
	}
	out := make([]reading, len(groups))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var done atomic.Int64

	for i, g := range groups {
		ref := primaryRef(g)
		if ref == nil {
			// A group with neither half is not a frame anybody can read; it
			// counts as unreadable rather than as having no position.
			out[i] = reading{err: fmt.Errorf("%s has no file to read", g.Stem)}
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fields, err := exif.Read(path)
			if err != nil {
				out[i] = reading{err: err}
			} else {
				out[i] = reading{gps: fields.GPS}
				if fields.DateTimeOriginal.Present {
					out[i].shot = fields.DateTimeOriginal.Value
				}
			}
			if n := done.Add(1); progress != nil && (n%16 == 0 || int(n) == len(groups)) {
				progress(int(n))
			}
		}(i, ref.Path)
	}
	wg.Wait()
	return out
}

// positionDTO flattens one positioned frame.
func positionDTO(g scan.PhotoGroup, r reading) PositionDTO {
	shot := r.shot
	if shot.IsZero() {
		// The scan's own timestamp, which is the modification time when the
		// file carried no capture time.
		shot = g.Shot
	}
	dto := PositionDTO{
		Dir:         g.Dir,
		Stem:        g.Stem,
		Kind:        g.Kind.String(),
		HasRaw:      g.Raw != nil,
		HasJpeg:     g.Jpeg != nil,
		Shot:        shot.Format(time.RFC3339),
		Latitude:    r.gps.Latitude,
		Longitude:   r.gps.Longitude,
		Altitude:    r.gps.Altitude,
		HasAltitude: r.gps.HasAltitude,
	}
	if g.Raw != nil {
		dto.RawPath = g.Raw.Path
	}
	if g.Jpeg != nil {
		dto.JpegPath = g.Jpeg.Path
	}
	return dto
}

// report publishes one progress record, through the test seam when there is
// one and to the webview otherwise.
func (s *MapService) report(p MapProgress) {
	if s.onProgress != nil {
		s.onProgress(p)
		return
	}
	emitEvent(EventMapProgress, p)
}

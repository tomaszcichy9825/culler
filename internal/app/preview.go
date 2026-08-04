package app

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/preview"
	"github.com/tomaszcichy9825/culler/internal/rawdev"
	"github.com/tomaszcichy9825/culler/internal/thumbs"
)

// PreviewRoute is the asset-server path the preview handler owns. The webview
// asks for /preview?path=…&tier=… and gets JPEG bytes back.
const PreviewRoute = "/preview"

// Preview tiers, in the order the pipeline resolves them.
const (
	// TierThumb is the EXIF thumbnail embedded in a JPEG: instant, tiny.
	TierThumb = "thumb"
	// TierJPEG is the JPEG on disk, streamed through untouched so the
	// embedded ICC profile reaches the webview.
	TierJPEG = "jpeg"
	// TierEmbedded is the full-size preview embedded in a RAW, which is what
	// makes RAW-only frames viewable without a demosaicer.
	TierEmbedded = "embedded"
	// TierDevelop is the demosaiced RAW itself, for 1:1 zoom on a RAW-only
	// frame where the embedded preview runs out of pixels. It costs seconds
	// rather than milliseconds, it is only ever asked for on demand, and it
	// exists at all only in a build made with -tags libraw.
	TierDevelop = "develop"
)

// SizeDevelop is the cache bucket a developed frame is filed under. It is a
// ceiling, not a target: thumbs.Store leaves any image already inside it at
// its own resolution, which is exactly what 1:1 zoom needs, and only the
// largest medium-format sensors are shrunk at all. Reusing the store buys
// atomic writes, LRU eviction and a shared size cap for free; the price is one
// decode and one re-encode of a full-resolution JPEG on the way in, which is
// small beside the demosaic that produced it.
const SizeDevelop = thumbs.Size(8192)

// previewCacheControl is long because a preview is requested many times per
// session while the file it came from does not change. An apply replaces the
// path rather than the bytes at it.
const previewCacheControl = "public, max-age=31536000, immutable"

// Concurrent preview reads are capped so a folder of tiles cannot saturate
// the source — network shares stall badly under parallel reads of large RAW
// files. The caps come from Behaviour.LocalReadSlots/NetworkReadSlots.

// PreviewService serves preview bytes over the asset server rather than
// through a binding: the webview can then use an ordinary <img src>, and the
// bytes never take the trip through JSON.
type PreviewService struct {
	app      *App
	localSem chan struct{}
	netSem   chan struct{}

	mu     sync.Mutex
	netDir map[string]bool // directory -> lives on a network volume

	thumbsOnce sync.Once
	thumbs     *thumbs.Store // lazily opened; nil when the cache dir is unusable
	thumbDir   string        // overrides the OS cache dir; tests point it at a temp dir
}

// NewPreviewService binds the service to the shared state. The semaphores are
// sized from the config at startup; changing the limits needs a relaunch.
func NewPreviewService(a *App) *PreviewService {
	b := a.Config().Behaviour
	return &PreviewService{
		app:      a,
		localSem: make(chan struct{}, max(1, b.LocalReadSlots)),
		netSem:   make(chan struct{}, max(1, b.NetworkReadSlots)),
		netDir:   make(map[string]bool),
	}
}

// acquire takes a read slot for path's volume class, or reports false when
// the request went away while waiting — a tile that scrolled out of view
// must not cost a network read.
func (s *PreviewService) acquire(r *http.Request, path string) (release func(), ok bool) {
	sem := s.localSem
	if s.isNetworkDir(filepath.Dir(path)) {
		sem = s.netSem
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	case <-r.Context().Done():
		return nil, false
	}
}

// isNetworkDir caches one statfs per directory; a folder's tiles all share it.
func (s *PreviewService) isNetworkDir(dir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, hit := s.netDir[dir]; hit {
		return v
	}
	v := platform.IsNetwork(dir)
	s.netDir[dir] = v
	return v
}

// Middleware answers preview requests and passes everything else to the
// asset server. It is the value for application.AssetOptions.Middleware.
func (s *PreviewService) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PreviewRoute {
			next.ServeHTTP(w, r)
			return
		}
		s.ServeHTTP(w, r)
	})
}

// ServeHTTP handles one preview request.
func (s *PreviewService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	tier := query.Get("tier")
	if tier == "" {
		tier = TierJPEG
	}

	if tier == TierDevelop {
		s.serveDevelop(w, r, query.Get("path"), query.Get("hash"))
		return
	}

	// A grid-size request with a content hash is answered from the local
	// thumb cache before the source is even stat'd: a hit costs one local
	// disk read no matter how slow — or how gone — the folder's volume is,
	// and it survives folder switches and relaunches. The extension check
	// still runs first; the cache is not a way around the whitelist.
	key := query.Get("hash")
	if query.Get("size") == "grid" && key != "" && allowedExt(s.app.Config(), query.Get("path"), tier) {
		if store := s.thumbStore(); store != nil {
			if cached, ok := store.Path(key, thumbs.SizeGrid); ok {
				store.Touch(key, thumbs.SizeGrid)
				w.Header().Set("Cache-Control", previewCacheControl)
				w.Header().Set("Content-Type", "image/jpeg")
				http.ServeFile(w, r, cached)
				return
			}
		}
	}

	path, info, status, err := resolvePreview(s.app.Config(), query.Get("path"), tier)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	if query.Get("size") == "grid" && key != "" {
		if store := s.thumbStore(); store != nil {
			release, ok := s.acquire(r, path)
			if !ok {
				return
			}
			defer release()
			cached, err := s.fillThumb(store, key, path, tier)
			if err == nil {
				w.Header().Set("Cache-Control", previewCacheControl)
				w.Header().Set("Content-Type", "image/jpeg")
				http.ServeFile(w, r, cached)
				return
			}
			// unresizable source: fall through and serve it uncached, with
			// the slot already held
			w.Header().Set("Cache-Control", previewCacheControl)
			if tier == TierJPEG {
				servePassthrough(w, r, path, info)
				return
			}
			serveExtracted(w, r, path, info, tier)
			return
		}
	}

	release, ok := s.acquire(r, path)
	if !ok {
		return // client gone; nothing to write
	}
	defer release()

	w.Header().Set("Cache-Control", previewCacheControl)
	if tier == TierJPEG {
		servePassthrough(w, r, path, info)
		return
	}
	serveExtracted(w, r, path, info, tier)
}

// serveDevelop demosaics the RAW and serves the result, filed in the
// thumbnail cache so the seconds it costs are paid once per frame rather than
// once per zoom. It resolves the request itself rather than sharing the grid
// path above: a develop is never a grid tile, and the cache bucket it reads
// and writes is a different one.
//
// Failure here is not fatal. The loupe falls back to the embedded preview on
// any non-success, so the only thing the status has to be is unambiguous.
func (s *PreviewService) serveDevelop(w http.ResponseWriter, r *http.Request, reqPath, key string) {
	path, info, status, err := resolvePreview(s.app.Config(), reqPath, TierDevelop)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	// The demosaicer is a build-time choice. Checking it after the request has
	// been validated rather than before keeps every rejection identical across
	// the two build variants, so only a request that would genuinely have been
	// served ever sees a 501. The loupe reads it as its cue to fall back to
	// the embedded preview, silently.
	if !rawdev.Available() {
		http.Error(w, rawdev.ErrUnavailable.Error(), http.StatusNotImplemented)
		return
	}

	store := s.thumbStore()
	cacheable := store != nil && key != ""
	if cacheable {
		if cached, ok := store.Path(key, SizeDevelop); ok {
			store.Touch(key, SizeDevelop)
			w.Header().Set("Cache-Control", previewCacheControl)
			w.Header().Set("Content-Type", "image/jpeg")
			http.ServeFile(w, r, cached)
			return
		}
	}

	release, ok := s.acquire(r, path)
	if !ok {
		return // client gone; nothing to write
	}
	defer release()

	data, err := rawdev.Develop(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Cache-Control", previewCacheControl)
	w.Header().Set("Content-Type", "image/jpeg")
	if cacheable {
		// Upright already: the demosaicer applies the camera's flip, and the
		// bytes it returns carry no EXIF for the store to read one from.
		if cached, err := store.PutOriented(key, SizeDevelop, data, 1); err == nil {
			http.ServeFile(w, r, cached)
			return
		}
		// Uncacheable — a full volume, say. The develop itself is still good.
	}
	http.ServeContent(w, r, filepath.Base(path)+".jpg", info.ModTime(), bytes.NewReader(data))
}

// thumbStore lazily opens the on-disk thumbnail cache in the OS cache dir.
// A machine where that fails just serves previews uncached.
func (s *PreviewService) thumbStore() *thumbs.Store {
	s.thumbsOnce.Do(func() {
		dir := s.thumbDir
		if dir == "" {
			base, err := os.UserCacheDir()
			if err != nil {
				return
			}
			dir = filepath.Join(base, "culler", "thumbs2")
		}
		store, err := thumbs.NewStore(dir, 0)
		if err != nil {
			return
		}
		s.thumbs = store
	})
	return s.thumbs
}

// fillThumb reads the source once, downsizes it, and files it under the
// frame's content hash. The caller holds a read slot.
func (s *PreviewService) fillThumb(store *thumbs.Store, key, path, tier string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	src := data
	orientation := 0
	if tier == TierEmbedded {
		src, err = preview.ExtractLargestJPEG(data)
		if err != nil {
			return "", err
		}
		// The embedded preview often has no EXIF of its own; the container's
		// orientation is the one that applies.
		if f, err := exif.Parse(data); err == nil && f.Orientation.Present {
			orientation = int(f.Orientation.Value)
		}
	}
	return store.PutOriented(key, thumbs.SizeGrid, src, orientation)
}

// allowedExt is the stat-free half of the request guard: an absolute, clean
// path whose extension the configuration serves for this tier.
func allowedExt(cfg config.Config, path, tier string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	var allowed []string
	switch tier {
	case TierJPEG, TierThumb:
		allowed = cfg.JpegExts
	case TierEmbedded, TierDevelop:
		allowed = cfg.RawExts
	default:
		return false
	}
	return contains(allowed, strings.ToLower(filepath.Ext(path)))
}

// resolvePreview is the whole guard on what the handler will read. The
// webview can ask for any path at all, so a request must name an existing
// regular file by an exact absolute path whose extension the configuration
// recognises for the tier being asked for. Relative paths, unclean paths and
// unknown extensions are refused rather than resolved.
func resolvePreview(cfg config.Config, path, tier string) (string, os.FileInfo, int, error) {
	if path == "" {
		return "", nil, http.StatusBadRequest, errors.New("missing path")
	}
	if !filepath.IsAbs(path) {
		return "", nil, http.StatusBadRequest, errors.New("path must be absolute")
	}
	if filepath.Clean(path) != path {
		return "", nil, http.StatusBadRequest, errors.New("path must be clean")
	}

	var allowed []string
	switch tier {
	case TierJPEG, TierThumb:
		allowed = cfg.JpegExts
	case TierEmbedded, TierDevelop:
		allowed = cfg.RawExts
	default:
		return "", nil, http.StatusBadRequest, fmt.Errorf("unknown tier %q", tier)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !contains(allowed, ext) {
		return "", nil, http.StatusForbidden, fmt.Errorf("extension %q is not served for tier %q", ext, tier)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", nil, http.StatusNotFound, errors.New("file not found")
	}
	if !info.Mode().IsRegular() {
		return "", nil, http.StatusNotFound, errors.New("not a file")
	}
	return path, info, http.StatusOK, nil
}

// servePassthrough streams the file as it is on disk, so the embedded ICC
// profile and the original quantisation reach the webview untouched.
func servePassthrough(w http.ResponseWriter, r *http.Request, path string, info os.FileInfo) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType(path))
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// serveExtracted pulls the embedded JPEG out of the file and serves that. A
// file with no usable preview is a 404: the grid shows a placeholder tile
// rather than blocking or failing the whole folder.
func serveExtracted(w http.ResponseWriter, r *http.Request, path string, info os.FileInfo, tier string) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	var jpeg []byte
	if tier == TierEmbedded {
		jpeg, err = preview.ExtractLargestJPEG(data)
	} else {
		jpeg, err = preview.ExtractEXIFThumb(data)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", etag(tier, info))
	http.ServeContent(w, r, filepath.Base(path)+".jpg", info.ModTime(), bytes.NewReader(jpeg))
}

// contentType is the media type of the file being passed through. The JPEG
// class covers HEIC, PNG and TIFF as well, and mislabelling those would stop
// the webview rendering them.
func contentType(path string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); t != "" {
		return t
	}
	return "image/jpeg"
}

// etag identifies an extracted preview by the tier plus the source file's
// size and modification time, which is exactly what would change the bytes.
func etag(tier string, info os.FileInfo) string {
	return fmt.Sprintf(`"%s-%d-%d"`, tier, info.Size(), info.ModTime().UnixNano())
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

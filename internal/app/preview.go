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
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/preview"
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
)

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
	path, info, status, err := resolvePreview(s.app.Config(), query.Get("path"), tier)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
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
	case TierEmbedded:
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

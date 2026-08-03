package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
)

// previewApp returns a service over stock configuration; nothing it does
// touches the decision store or the journal.
func previewApp(t *testing.T) *PreviewService {
	t.Helper()
	return NewPreviewService(newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), config.Default()))
}

// getPreview issues a preview request for path and tier.
func getPreview(t *testing.T, s *PreviewService, path, tier string) *httptest.ResponseRecorder {
	t.Helper()
	target := PreviewRoute + "?path=" + url.QueryEscape(path)
	if tier != "" {
		target += "&tier=" + url.QueryEscape(tier)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestPreviewRejectsBadRequests(t *testing.T) {
	s := previewApp(t)
	dir := t.TempDir()
	jpeg := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(jpeg, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "album.jpg")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		tier string
		want int
	}{
		{"no path", "", TierJPEG, http.StatusBadRequest},
		{"relative path", "DSCF0001.JPG", TierJPEG, http.StatusBadRequest},
		{"traversal", dir + "/../" + filepath.Base(dir) + "/DSCF0001.JPG", TierJPEG, http.StatusBadRequest},
		{"trailing slash", jpeg + "/", TierJPEG, http.StatusBadRequest},
		{"unknown tier", jpeg, "full", http.StatusBadRequest},
		{"extension not configured", filepath.Join(dir, "notes.txt"), TierJPEG, http.StatusForbidden},
		{"raw extension on jpeg tier", filepath.Join(dir, "DSCF0001.RAF"), TierJPEG, http.StatusForbidden},
		{"jpeg extension on embedded tier", jpeg, TierEmbedded, http.StatusForbidden},
		{"missing file", filepath.Join(dir, "gone.JPG"), TierJPEG, http.StatusNotFound},
		{"directory", subdir, TierJPEG, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getPreview(t, s, tt.path, tt.tier).Code; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPreviewServesJPEGBytesUnchanged(t *testing.T) {
	s := previewApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "DSCF0001.JPG")
	want := []byte{0xFF, 0xD8, 'i', 'c', 'c', 0xFF, 0xD9}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}

	// No tier at all defaults to the JPEG on disk.
	rec := getPreview(t, s, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); string(got) != string(want) {
		t.Errorf("body = %v, want the file's own bytes %v", got, want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != previewCacheControl {
		t.Errorf("Cache-Control = %q, want %q", cc, previewCacheControl)
	}
}

func TestPreviewExtractionFailureIsNotFound(t *testing.T) {
	s := previewApp(t)
	dir := t.TempDir()

	raf := filepath.Join(dir, "DSCF0001.RAF")
	if err := os.WriteFile(raf, []byte("not a fujifilm container"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getPreview(t, s, raf, TierEmbedded).Code; got != http.StatusNotFound {
		t.Errorf("unparseable RAW: status = %d, want 404", got)
	}

	jpeg := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(jpeg, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getPreview(t, s, jpeg, TierThumb).Code; got != http.StatusNotFound {
		t.Errorf("JPEG with no EXIF thumbnail: status = %d, want 404", got)
	}
}

func TestPreviewRejectsNonGET(t *testing.T) {
	s := previewApp(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, PreviewRoute+"?path=/card/a.JPG", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestPreviewMiddlewarePassesOtherRoutesThrough(t *testing.T) {
	s := previewApp(t)
	var reached bool
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !reached || rec.Code != http.StatusTeapot {
		t.Errorf("asset request did not reach the next handler: reached=%v status=%d", reached, rec.Code)
	}

	reached = false
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PreviewRoute+"?path=/card/a.JPG", nil))
	if reached {
		t.Error("preview request was passed to the asset server")
	}
}

func TestPreviewHonoursConfiguredExtensions(t *testing.T) {
	cfg := config.Default()
	cfg.JpegExts = []string{".jpg"}
	s := NewPreviewService(newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), cfg))

	dir := t.TempDir()
	path := filepath.Join(dir, "DSCF0001.heic")
	if err := os.WriteFile(path, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getPreview(t, s, path, TierJPEG).Code; got != http.StatusForbidden {
		t.Errorf("extension removed from the config: status = %d, want 403", got)
	}
}

func TestGridThumbSurvivesSourceRemoval(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "DSCF0001.JPG")
	writeTestJPEG(t, src, 1200, 800)

	s := previewApp(t)
	s.thumbDir = t.TempDir()

	target := PreviewRoute + "?path=" + url.QueryEscape(src) + "&tier=jpeg&size=grid&hash=deadbeef"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: %d %s", rec.Code, rec.Body.String())
	}
	first := rec.Body.Bytes()
	if len(first) == 0 {
		t.Fatal("empty thumb")
	}

	// The source is gone; a revisit must still serve the cached thumb.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cached request after source removal: %d", rec.Code)
	}
	if !bytes.Equal(first, rec.Body.Bytes()) {
		t.Fatal("cached thumb differs from the first render")
	}
}

// writeTestJPEG writes a real decodable JPEG of the given size.
func writeTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 31)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

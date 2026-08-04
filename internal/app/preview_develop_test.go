package app

import (
	"bytes"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/rawdev"
)

// getDevelop issues a develop-tier request, optionally carrying the frame's
// content hash so the cache is in play.
func getDevelop(t *testing.T, s *PreviewService, path, hash string) *httptest.ResponseRecorder {
	t.Helper()
	target := PreviewRoute + "?tier=" + TierDevelop + "&path=" + url.QueryEscape(path)
	if hash != "" {
		target += "&hash=" + url.QueryEscape(hash)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// The guard on the develop tier must be the same guard as every other tier,
// and must answer the same way whether or not the demosaicer was compiled in.
// A build variant that validates requests differently is a build variant that
// gets tested once.
func TestDevelopTierGuardsRequests(t *testing.T) {
	s := previewApp(t)
	s.thumbDir = t.TempDir()
	dir := t.TempDir()
	jpg := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(jpg, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	// Named like a RAW so it clears the extension check and is refused for
	// what it actually is.
	notAFile := filepath.Join(dir, "burst.RAF")
	if err := os.Mkdir(notAFile, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want int
	}{
		{"no path", "", http.StatusBadRequest},
		{"relative path", "DSCF0001.RAF", http.StatusBadRequest},
		{"unclean path", dir + "/../" + filepath.Base(dir) + "/DSCF0001.RAF", http.StatusBadRequest},
		{"jpeg extension", jpg, http.StatusForbidden},
		{"missing file", filepath.Join(dir, "gone.RAF"), http.StatusNotFound},
		{"directory", notAFile, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getDevelop(t, s, tt.path, "").Code; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

// A RAW the demosaicer cannot make sense of — here, one that is not a RAW at
// all — must come back as a plain failure, so the loupe falls back to the
// embedded preview. A build without LibRaw says so with a 501 instead. Both
// are non-success and the frontend treats them the same; the point of
// asserting them separately is that neither is a 200 and neither hangs.
func TestDevelopTierAnswersAccordingToTheBuild(t *testing.T) {
	s := previewApp(t)
	s.thumbDir = t.TempDir()
	raw := filepath.Join(t.TempDir(), "DSCF0001.RAF")
	if err := os.WriteFile(raw, bytes.Repeat([]byte{0x00, 0xFF}, 512), 0o644); err != nil {
		t.Fatal(err)
	}

	want := http.StatusNotImplemented
	if rawdev.Available() {
		want = http.StatusUnprocessableEntity
	}
	rec := getDevelop(t, s, raw, "")
	if rec.Code != want {
		t.Errorf("status = %d, want %d (rawdev available: %v)", rec.Code, want, rawdev.Available())
	}
	if rec.Body.Len() == 0 {
		t.Error("failure carried no explanation")
	}
}

// The develop of a real frame is the expensive thing the cache exists for, so
// this asserts both halves: that the bytes come back at sensor resolution, and
// that the second request is served from the cache rather than demosaiced
// again. There is no RAW in the repository — see internal/rawdev for why — so
// the fixture comes from the environment or the test skips.
func TestDevelopTierServesAndCachesARealRAW(t *testing.T) {
	if !rawdev.Available() {
		t.Skip("built without -tags libraw: nothing to develop")
	}
	path := os.Getenv("CULLER_RAW_FIXTURE")
	if path == "" {
		t.Skip("no RAW fixture: set CULLER_RAW_FIXTURE=/path/to/a.raf")
	}

	s := previewApp(t)
	s.thumbDir = t.TempDir()
	const hash = "deadbeefcafe"

	rec := getDevelop(t, s, path, hash)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("develop tier served something that is not a JPEG: %v", err)
	}
	if long := max(cfg.Width, cfg.Height); long < 1000 {
		t.Errorf("served %dx%d, want something sensor-sized", cfg.Width, cfg.Height)
	}

	// Now it must be on disk under the develop bucket, and served from there.
	store := s.thumbStore()
	if store == nil {
		t.Fatal("no thumb store")
	}
	if _, ok := store.Path(hash, SizeDevelop); !ok {
		t.Fatal("develop was not filed in the cache")
	}
	again := getDevelop(t, s, path, hash)
	if again.Code != http.StatusOK {
		t.Fatalf("cached status = %d, want 200", again.Code)
	}
	if again.Body.Len() == 0 {
		t.Error("cached response was empty")
	}
	t.Logf("developed %s to %dx%d", filepath.Base(path), cfg.Width, cfg.Height)
}

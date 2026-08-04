//go:build libraw

package rawdev

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// rawExts are the containers a fixture may plausibly be in; the develop path
// does not care which, LibRaw sorts that out.
var rawExts = []string{".raf", ".arw", ".cr2", ".cr3", ".nef", ".orf", ".rw2", ".dng", ".pef"}

// fixture finds a real RAW file to develop. There is none in the repository —
// the smallest honest one is tens of megabytes, and a synthesised container
// would prove nothing about a demosaicer. So the test reads CULLER_RAW_FIXTURE
// or anything dropped in testdata/, and skips loudly otherwise rather than
// passing on a file it never opened.
func fixture(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CULLER_RAW_FIXTURE"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("CULLER_RAW_FIXTURE=%s: %v", p, err)
		}
		return p
	}
	for _, ext := range rawExts {
		matches, _ := filepath.Glob(filepath.Join("testdata", "*"+ext))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	t.Skip("no RAW fixture: set CULLER_RAW_FIXTURE=/path/to/a.raf or drop one in testdata/")
	return ""
}

func TestAvailableWithTheTag(t *testing.T) {
	if !Available() {
		t.Fatal("Available() = false in a build with -tags libraw")
	}
}

// Whatever the webview asks for eventually reaches LibRaw, so the things it
// can be handed — nothing, a path that is not there, a file that is not a RAW
// — have to come back as errors and not as a signal.
func TestDevelopRejectsRubbish(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "DSCF0001.RAF")
	if err := os.WriteFile(junk, bytes.Repeat([]byte{0x00, 0xFF}, 512), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "DSCF0002.RAF")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"", filepath.Join(dir, "gone.RAF"), junk, empty, dir} {
		if _, err := Develop(path); err == nil {
			t.Errorf("Develop(%q) succeeded, want an error", path)
		}
	}
}

func TestDevelopProducesFullResolutionJPEG(t *testing.T) {
	path := fixture(t)

	data, err := Develop(path)
	if err != nil {
		t.Fatalf("Develop(%s): %v", path, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("develop output is not a decodable JPEG: %v", err)
	}
	// Half-size demosaicing is off, so this must be sensor-sized rather than a
	// preview: no camera LibRaw can read shoots a long edge under 1000px.
	if long := max(cfg.Width, cfg.Height); long < 1000 {
		t.Errorf("developed %dx%d, want something sensor-sized", cfg.Width, cfg.Height)
	}
	t.Logf("developed %s to %dx%d, %d bytes of JPEG", filepath.Base(path), cfg.Width, cfg.Height, len(data))
}

// The handler serves concurrent requests, so two zooms landing together must
// queue rather than race. Run this one under -race.
func TestDevelopIsConcurrencySafe(t *testing.T) {
	path := fixture(t)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Develop(path); err != nil {
				t.Errorf("Develop: %v", err)
			}
		}()
	}
	wg.Wait()
}

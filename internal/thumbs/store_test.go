package thumbs

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- fixture helpers -------------------------------------------------------

// makeJPEG encodes a w×h JPEG of deterministic noise. Noise rather than a flat
// fill because a flat image compresses to a few hundred bytes, which would
// make the size-cap tests meaningless.
func makeJPEG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := range h {
		for x := range w {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

// decodedBounds decodes the JPEG at path and returns its pixel dimensions.
func decodedBounds(t *testing.T, path string) (w, h int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

// cacheBytes totals the size of every regular file under dir.
func cacheBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return total
}

// thumbBytes returns the on-disk size of one grid thumbnail of the given
// source, so size-cap tests can be written in units of whole entries.
func thumbBytes(t *testing.T, src []byte) int64 {
	t.Helper()
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	path, err := s.Put("0000000000000000", SizeGrid, src)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Size()
}

// key returns a plausible content hash: the caller's label padded out to the
// 64 hex characters internal/hash produces.
func key(label string) string {
	return label + strings.Repeat("0", 64-len(label))
}

// --- store -----------------------------------------------------------------

func TestPutThenPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	k := key("aa")
	if _, ok := s.Path(k, SizeGrid); ok {
		t.Fatal("Path reported a hit before anything was written")
	}

	written, err := s.Put(k, SizeGrid, makeJPEG(t, 1024, 768))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := s.Path(k, SizeGrid)
	if !ok {
		t.Fatal("Path reported a miss after Put")
	}
	if got != written {
		t.Fatalf("Path = %q, Put returned %q", got, written)
	}
	if !strings.HasPrefix(written, dir) {
		t.Fatalf("thumbnail %q written outside cache dir %q", written, dir)
	}
	if _, ok := s.Path(k, SizeLoupe); ok {
		t.Fatal("Path hit for a size that was never written")
	}
}

func TestPutResizesToLongEdge(t *testing.T) {
	cases := []struct {
		name         string
		srcW, srcH   int
		size         Size
		wantW, wantH int
	}{
		{"landscape grid", 1024, 768, SizeGrid, 512, 384},
		{"portrait grid", 600, 1200, SizeGrid, 256, 512},
		{"square grid", 900, 900, SizeGrid, 512, 512},
		{"landscape loupe", 3200, 2000, SizeLoupe, 2560, 1600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewStore(t.TempDir(), 0)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			path, err := s.Put(key("bb"), tc.size, makeJPEG(t, tc.srcW, tc.srcH))
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			w, h := decodedBounds(t, path)
			if w != tc.wantW || h != tc.wantH {
				t.Fatalf("thumbnail is %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
			if max(w, h) != int(tc.size) {
				t.Fatalf("long edge %d, want %d", max(w, h), int(tc.size))
			}
		})
	}
}

func TestPutNeverUpscales(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	path, err := s.Put(key("cc"), SizeGrid, makeJPEG(t, 200, 100))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if w, h := decodedBounds(t, path); w != 200 || h != 100 {
		t.Fatalf("thumbnail is %dx%d, want the source 200x100 untouched", w, h)
	}
}

func TestPutLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, label := range []string{"d1", "d2", "d3"} {
		if _, err := s.Put(key(label), SizeGrid, makeJPEG(t, 800, 600)); err != nil {
			t.Fatalf("Put %s: %v", label, err)
		}
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Ext(path) != ".jpg" {
			t.Errorf("leftover non-thumbnail file %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

func TestPutRejectsCorruptJPEG(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	src := makeJPEG(t, 400, 300)
	cases := map[string][]byte{
		"not a jpeg": []byte("this is not an image at all"),
		"empty":      {},
		"truncated":  src[:len(src)/3],
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Put(key("ee"), SizeGrid, bad); err == nil {
				t.Fatal("Put accepted a corrupt source")
			}
			if _, ok := s.Path(key("ee"), SizeGrid); ok {
				t.Fatal("failed Put left a cache entry behind")
			}
			if n := cacheBytes(t, dir); n != 0 {
				t.Fatalf("failed Put wrote %d bytes", n)
			}
		})
	}
}

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	src := makeJPEG(t, 900, 600)
	one := thumbBytes(t, src)

	dir := t.TempDir()
	limit := 2*one + one/2 // room for two entries, not three
	s, err := NewStore(dir, limit)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, label := range []string{"f1", "f2", "f3"} {
		if _, err := s.Put(key(label), SizeGrid, src); err != nil {
			t.Fatalf("Put %s: %v", label, err)
		}
	}

	if _, ok := s.Path(key("f1"), SizeGrid); ok {
		t.Error("least-recently-used entry survived eviction")
	}
	for _, label := range []string{"f2", "f3"} {
		path, ok := s.Path(key(label), SizeGrid)
		if !ok {
			t.Fatalf("recent entry %s was evicted", label)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Path hit but file missing: %v", err)
		}
	}
	if n := cacheBytes(t, dir); n > limit {
		t.Fatalf("cache holds %d bytes, cap is %d", n, limit)
	}
}

func TestTouchChangesEvictionOrder(t *testing.T) {
	src := makeJPEG(t, 900, 600)
	one := thumbBytes(t, src)

	s, err := NewStore(t.TempDir(), 2*one+one/2)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, label := range []string{"g1", "g2"} {
		if _, err := s.Put(key(label), SizeGrid, src); err != nil {
			t.Fatalf("Put %s: %v", label, err)
		}
	}
	s.Touch(key("g1"), SizeGrid) // g2 is now the least recently used

	if _, err := s.Put(key("g3"), SizeGrid, src); err != nil {
		t.Fatalf("Put g3: %v", err)
	}
	if _, ok := s.Path(key("g1"), SizeGrid); !ok {
		t.Error("touched entry was evicted")
	}
	if _, ok := s.Path(key("g2"), SizeGrid); ok {
		t.Error("untouched entry survived eviction")
	}
}

func TestNewStoreRebuildsIndexFromDisk(t *testing.T) {
	src := makeJPEG(t, 900, 600)
	one := thumbBytes(t, src)
	dir := t.TempDir()

	first, err := NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	paths := map[string]string{}
	for _, label := range []string{"h1", "h2"} {
		p, err := first.Put(key(label), SizeGrid, src)
		if err != nil {
			t.Fatalf("Put %s: %v", label, err)
		}
		paths[label] = p
	}
	// Pin the mtimes so the rebuilt index has an unambiguous recency order.
	base := time.Now().Add(-time.Hour)
	for i, label := range []string{"h1", "h2"} {
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(paths[label], when, when); err != nil {
			t.Fatalf("chtimes %s: %v", label, err)
		}
	}

	reopened, err := NewStore(dir, 2*one+one/2)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for _, label := range []string{"h1", "h2"} {
		if _, ok := reopened.Path(key(label), SizeGrid); !ok {
			t.Fatalf("entry %s missing after reopen", label)
		}
	}

	// The rebuilt index must also account for the existing bytes, so the next
	// Put evicts the oldest file rather than blowing through the cap.
	if _, err := reopened.Put(key("h3"), SizeGrid, src); err != nil {
		t.Fatalf("Put h3: %v", err)
	}
	if _, ok := reopened.Path(key("h1"), SizeGrid); ok {
		t.Error("oldest rebuilt entry survived eviction")
	}
	if _, ok := reopened.Path(key("h2"), SizeGrid); !ok {
		t.Error("newer rebuilt entry was evicted first")
	}
	if n := cacheBytes(t, dir); n > 2*one+one/2 {
		t.Fatalf("cache holds %d bytes, cap is %d", n, 2*one+one/2)
	}
}

// BenchmarkPutGrid measures one decode-plus-resize of a 24MP frame, the step
// the cgo decision in DESIGN.md §5 hangs on.
func BenchmarkPutGrid(b *testing.B) {
	src := makeJPEG(b, 6000, 4000)
	s, err := NewStore(b.TempDir(), 0)
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}
	b.ResetTimer()
	for i := range b.N {
		if _, err := s.Put(key(strconv.Itoa(i)), SizeGrid, src); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
}

func TestPutOverwriteKeepsOneEntry(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	src := makeJPEG(t, 900, 600)
	one := thumbBytes(t, src)
	for range 3 {
		if _, err := s.Put(key("ii"), SizeGrid, src); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if n := cacheBytes(t, dir); n > one+one/2 {
		t.Fatalf("repeated Put of one key holds %d bytes, one entry is %d", n, one)
	}
}

// withOrientation splices a minimal EXIF APP1 segment carrying the given
// orientation straight after the SOI marker of a JPEG.
func withOrientation(t *testing.T, jpegData []byte, orientation uint16) []byte {
	t.Helper()
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		t.Fatal("not a JPEG")
	}
	// TIFF block: II header, one IFD with a single SHORT orientation entry.
	tiff := []byte{
		'I', 'I', 42, 0, 8, 0, 0, 0, // header, IFD at offset 8
		1, 0, // one entry
		0x12, 0x01, 3, 0, 1, 0, 0, 0, // tag 0x0112, SHORT, count 1
		byte(orientation), byte(orientation >> 8), 0, 0,
		0, 0, 0, 0, // no next IFD
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{0xFF, 0xD8}, append(seg, payload...)...)
	return append(out, jpegData[2:]...)
}

func TestPutHonoursEXIFOrientation(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// A landscape image tagged orientation 6 (rotate 90° clockwise) is a
	// portrait photo; the cached thumb must come out portrait or every
	// sideways camera shot renders sideways forever.
	src := withOrientation(t, makeJPEG(t, 800, 400), 6)
	path, err := s.Put(key("or6"), SizeGrid, src)
	if err != nil {
		t.Fatal(err)
	}
	w, h := decodedBounds(t, path)
	if !(h > w) {
		t.Fatalf("orientation 6 must rotate: got %dx%d, want portrait", w, h)
	}
	if w != 256 || h != 512 {
		t.Fatalf("rotated long edge must still be the size cap: got %dx%d", w, h)
	}
}

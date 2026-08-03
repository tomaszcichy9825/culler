// Package thumbs is the thumbnail cache: a content-addressed directory of
// resized JPEGs keyed on the identity hash from internal/hash, plus a decode
// worker pool that fills it. Two sizes are cached, one for the grid and one
// for the loupe. The cache lives wherever the caller puts it — never on the
// source card — and evicts least-recently-used entries once it passes its
// size cap.
//
// EXIF orientation is deliberately not applied here. Put stores the pixels the
// JPEG decoder produced; rotating them is the display layer's job, and doing
// it in the cache would bake one interpretation into the file.
package thumbs

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/draw"
)

// Size is the long edge, in pixels, of a cached thumbnail.
type Size int

const (
	SizeGrid  Size = 512  // grid tile
	SizeLoupe Size = 2560 // full-screen loupe
)

const (
	jpegQuality     = 85
	defaultMaxBytes = 1 << 30 // 1GB
	shardLen        = 2       // leading key characters used as a subdirectory
	tmpPrefix       = ".thumb-"
)

// entryKey identifies one cached file.
type entryKey struct {
	key  string
	size Size
}

// entry is the index record for one cached file. used is a rank from the
// store's counter rather than a timestamp, so recency is a strict order even
// when two writes land inside the same clock tick.
type entry struct {
	bytes int64
	used  uint64
}

// Store is a content-addressed disk cache of thumbnails. It is safe for
// concurrent use.
type Store struct {
	dir      string
	maxBytes int64

	mu      sync.Mutex
	entries map[entryKey]entry
	total   int64
	clock   uint64
}

// NewStore opens the cache rooted at dir, creating it if needed, and rebuilds
// its index from whatever is already there. maxBytes caps total disk usage;
// 0 or less selects the 1GB default.
func NewStore(dir string, maxBytes int64) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("thumbs: empty cache directory")
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("thumbs: create cache dir: %w", err)
	}
	s := &Store{
		dir:      dir,
		maxBytes: maxBytes,
		entries:  make(map[entryKey]entry),
	}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns where (key, size) lives in the cache and whether it is present.
// The path is returned either way, so a caller can log a miss by name.
func (s *Store) Path(key string, size Size) (string, bool) {
	ek := entryKey{key: key, size: size}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[ek]
	return s.filePath(ek), ok
}

// Put decodes srcJPEG, shrinks it so its long edge is size (a smaller source
// is left alone rather than upscaled), re-encodes it as JPEG, writes it
// atomically and marks it most recently used. Entries are evicted afterwards
// if the cache is over its cap; the file just written is never one of them.
func (s *Store) Put(key string, size Size, srcJPEG []byte) (string, error) {
	return s.PutOriented(key, size, srcJPEG, 0)
}

// PutOriented is Put for sources whose orientation lives outside the bytes —
// a RAW's embedded preview often carries no EXIF of its own, so the caller
// passes the container's orientation. Zero means read it from the bytes.
func (s *Store) PutOriented(key string, size Size, srcJPEG []byte, orientation int) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	if size <= 0 {
		return "", fmt.Errorf("thumbs: invalid size %d", size)
	}

	src, err := jpeg.Decode(bytes.NewReader(srcJPEG))
	if err != nil {
		return "", fmt.Errorf("thumbs: decode source for %s: %w", key, err)
	}
	// The re-encode below strips the EXIF segment, so the orientation it
	// carried must be baked into the pixels — otherwise every sideways camera
	// shot renders sideways forever from the cache.
	if orientation == 0 {
		orientation = sourceOrientation(srcJPEG)
	}
	src = orient(src, orientation)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, shrink(src, size), &jpeg.Options{Quality: jpegQuality}); err != nil {
		return "", fmt.Errorf("thumbs: encode %s: %w", key, err)
	}

	ek := entryKey{key: key, size: size}
	path := s.filePath(ek)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("thumbs: create cache dir: %w", err)
	}
	if err := writeAtomic(path, buf.Bytes()); err != nil {
		return "", fmt.Errorf("thumbs: write %s: %w", path, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.entries[ek]; ok {
		s.total -= old.bytes
	}
	s.clock++
	s.entries[ek] = entry{bytes: int64(buf.Len()), used: s.clock}
	s.total += int64(buf.Len())
	s.evict(ek)
	return path, nil
}

// Touch marks (key, size) most recently used. Call it on a cache hit, so that
// tiles the user keeps looking at outlive the ones they scrolled past once.
func (s *Store) Touch(key string, size Size) {
	ek := entryKey{key: key, size: size}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[ek]
	if !ok {
		return
	}
	s.clock++
	e.used = s.clock
	s.entries[ek] = e
}

// evict deletes least-recently-used entries until the cache is under its cap.
// protect is the entry that must survive: without it a single thumbnail larger
// than the whole cap would delete itself and every Put would miss. The scan is
// linear in the number of entries, which at cache sizes is cheaper than
// maintaining a second ordered structure. Caller holds s.mu.
func (s *Store) evict(protect entryKey) {
	for s.total > s.maxBytes {
		var victim entryKey
		var found bool
		var oldest uint64
		for k, e := range s.entries {
			if k == protect {
				continue
			}
			if !found || e.used < oldest {
				victim, oldest, found = k, e.used, true
			}
		}
		if !found {
			return
		}
		// A file that has already vanished still leaves the index to fix.
		os.Remove(s.filePath(victim))
		s.total -= s.entries[victim].bytes
		delete(s.entries, victim)
	}
}

// rebuild indexes the files already in the cache directory, seeding recency
// from mtime so a reopened cache evicts in roughly the order it was filled.
// Temporary files left behind by an interrupted write are cleaned up here.
func (s *Store) rebuild() error {
	type scanned struct {
		ek    entryKey
		bytes int64
		mod   int64
	}
	var found []scanned

	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, tmpPrefix) {
			os.Remove(path)
			return nil
		}
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 || filepath.Ext(name) != ".jpg" {
			return nil // not something this package wrote; leave it alone
		}
		size, err := strconv.Atoi(parts[0])
		if err != nil || size <= 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // vanished mid-scan
		}
		found = append(found, scanned{
			ek:    entryKey{key: strings.TrimSuffix(name, ".jpg"), size: Size(size)},
			bytes: info.Size(),
			mod:   info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("thumbs: scan cache: %w", err)
	}

	sort.Slice(found, func(i, j int) bool { return found[i].mod < found[j].mod })
	for _, f := range found {
		s.clock++
		s.entries[f.ek] = entry{bytes: f.bytes, used: s.clock}
		s.total += f.bytes
	}
	return nil
}

// filePath lays the cache out as <dir>/<size>/<first two key chars>/<key>.jpg,
// so no single directory ends up with a hundred thousand entries in it.
func (s *Store) filePath(ek entryKey) string {
	return filepath.Join(s.dir, strconv.Itoa(int(ek.size)), shard(ek.key), ek.key+".jpg")
}

func shard(key string) string {
	if len(key) >= shardLen {
		return key[:shardLen]
	}
	return key + strings.Repeat("_", shardLen-len(key))
}

// validKey rejects anything that would escape the cache directory or collide
// with the layout. Real keys are hex hashes; this only catches misuse.
func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("thumbs: empty key")
	}
	if strings.ContainsAny(key, `/\.`) {
		return fmt.Errorf("thumbs: invalid key %q", key)
	}
	return nil
}

// shrink returns src scaled so its long edge is size. A source already within
// size is returned unchanged: upscaling a thumbnail wastes bytes and adds no
// detail.
func shrink(src image.Image, size Size) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || max(w, h) <= int(size) {
		return src
	}

	scale := float64(size) / float64(max(w, h))
	dw := max(1, int(math.Round(float64(w)*scale)))
	dh := max(1, int(math.Round(float64(h)*scale)))
	// Pin the long edge exactly; rounding the other side keeps the aspect.
	if w >= h {
		dw = int(size)
	} else {
		dh = int(size)
	}

	// Grid tiles are numerous and small on screen, so speed wins there; the
	// loupe is one image the user is staring at, so quality wins.
	scaler := draw.Scaler(draw.CatmullRom)
	if size <= SizeGrid {
		scaler = draw.ApproxBiLinear
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	scaler.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// writeAtomic writes data to a temporary file in the same directory and
// renames it into place, so a reader never sees a half-written thumbnail.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename below has succeeded

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Package scan walks a directory and groups image files into PhotoGroups,
// the unit of display and decision. The UI never operates on individual files.
package scan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kind classifies a PhotoGroup by which halves of the RAW+JPEG pair exist.
type Kind int

const (
	KindPaired   Kind = iota // RAW + JPEG both present
	KindJPEGOnly             // JPEG-class file only
	KindRAWOnly              // RAW file only
)

func (k Kind) String() string {
	switch k {
	case KindPaired:
		return "paired"
	case KindJPEGOnly:
		return "jpeg-only"
	case KindRAWOnly:
		return "raw-only"
	}
	return "unknown"
}

// FileRef points at one file on disk.
type FileRef struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// PhotoGroup is one frame: the RAW and/or JPEG that share a stem, plus any
// sidecars. Grouping key is (Dir, lowercase(Stem)) — case-insensitive because
// exFAT and default macOS volumes are case-insensitive but ext4 is not.
type PhotoGroup struct {
	Dir      string    // absolute directory
	Stem     string    // basename without extension, case-preserved
	Kind     Kind
	Raw      *FileRef  // nil if absent
	Jpeg     *FileRef  // nil if absent
	Sidecars []FileRef // follow the RAW on all operations
	Shot     time.Time // EXIF DateTimeOriginal, falls back to mtime
	Warnings []string  // surfaced as badges in the UI
}

// Config holds the extension classes. Order within each list is the priority
// used to pick a primary when several files of the same class share a stem.
type Config struct {
	RawExts     []string // lowercase, with dot
	JpegExts    []string
	SidecarExts []string
}

// DefaultConfig returns the built-in extension lists. Users can extend these
// via the config file without a release.
func DefaultConfig() Config {
	return Config{
		RawExts: []string{
			".raf", ".arw", ".cr2", ".cr3", ".nef", ".nrw", ".orf", ".rw2",
			".dng", ".pef", ".srw", ".raw", ".rwl", ".3fr", ".iiq", ".x3f",
		},
		JpegExts: []string{
			".jpg", ".jpeg", ".heic", ".heif", ".png", ".tif", ".tiff",
			".webp", ".avif",
		},
		SidecarExts: []string{".xmp", ".aae", ".dop"},
	}
}

func (c Config) class(ext string) (class string, priority int) {
	for i, e := range c.RawExts {
		if e == ext {
			return "raw", i
		}
	}
	for i, e := range c.JpegExts {
		if e == ext {
			return "jpeg", i
		}
	}
	for i, e := range c.SidecarExts {
		if e == ext {
			return "sidecar", i
		}
	}
	return "", 0
}

// stemKey returns the case-insensitive grouping key for a filename and the
// case-preserved stem. For sidecars named after a full filename
// (DSCF1234.RAF.xmp) the inner extension is stripped as well.
func stemKey(name string, cfg Config) (key, stem string) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	// DSCF1234.RAF.xmp → base DSCF1234.RAF → strip known inner image ext
	if inner := strings.ToLower(filepath.Ext(base)); inner != "" {
		if cls, _ := cfg.class(inner); cls == "raw" || cls == "jpeg" {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return strings.ToLower(base), base
}

// ScanDir reads one directory (non-recursive — groups are never merged across
// directories) and returns its PhotoGroups sorted by stem. Unrecognised
// extensions are ignored, never touched.
func ScanDir(dir string, cfg Config) ([]PhotoGroup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type slot struct {
		file     FileRef
		name     string
		priority int
	}
	type bucket struct {
		stem     string
		raws     []slot
		jpegs    []slot
		sidecars []FileRef
	}
	buckets := make(map[string]*bucket)

	get := func(name string) *bucket {
		key, stem := stemKey(name, cfg)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{stem: stem}
			buckets[key] = b
		}
		return b
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		class, prio := cfg.class(ext)
		if class == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished mid-scan; skip rather than fail the whole walk
		}
		ref := FileRef{
			Path:    filepath.Join(dir, name),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		b := get(name)
		switch class {
		case "raw":
			b.raws = append(b.raws, slot{ref, name, prio})
		case "jpeg":
			b.jpegs = append(b.jpegs, slot{ref, name, prio})
		case "sidecar":
			b.sidecars = append(b.sidecars, ref)
		}
	}

	pick := func(slots []slot) (*FileRef, []string) {
		if len(slots) == 0 {
			return nil, nil
		}
		sort.SliceStable(slots, func(i, j int) bool { return slots[i].priority < slots[j].priority })
		var warns []string
		if len(slots) > 1 {
			var names []string
			for _, s := range slots[1:] {
				names = append(names, s.name)
			}
			warns = append(warns, "duplicate files for this frame, using "+slots[0].name+
				" (also present: "+strings.Join(names, ", ")+")")
		}
		f := slots[0].file
		return &f, warns
	}

	var groups []PhotoGroup
	for _, b := range buckets {
		raw, rawWarns := pick(b.raws)
		jpeg, jpegWarns := pick(b.jpegs)
		if raw == nil && jpeg == nil {
			continue // sidecar with no parent image; leave it alone
		}
		g := PhotoGroup{
			Dir:      dir,
			Stem:     b.stem,
			Raw:      raw,
			Jpeg:     jpeg,
			Sidecars: b.sidecars,
			Warnings: append(rawWarns, jpegWarns...),
		}
		switch {
		case raw != nil && jpeg != nil:
			g.Kind = KindPaired
		case raw != nil:
			g.Kind = KindRAWOnly
		default:
			g.Kind = KindJPEGOnly
		}
		// EXIF DateTimeOriginal comes later with the preview pipeline; mtime
		// of the primary file is the fallback either way.
		if jpeg != nil {
			g.Shot = jpeg.ModTime
		} else {
			g.Shot = raw.ModTime
		}
		groups = append(groups, g)
	}

	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Stem) < strings.ToLower(groups[j].Stem)
	})
	return groups, nil
}

// Package scan walks a directory and groups image files into PhotoGroups,
// the unit of display and decision. The UI never operates on individual files.
package scan

import (
	"fmt"
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
	Dir      string // absolute directory
	Stem     string // basename without extension, case-preserved
	Kind     Kind
	Raw      *FileRef  // nil if absent
	Jpeg     *FileRef  // nil if absent
	Sidecars []FileRef // follow the RAW on all operations
	Shot     time.Time // the primary file's mtime; the catalogue replaces it with the EXIF capture time
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

// fileSlot is one candidate for a group's primary file, kept with the name and
// class priority that decide which candidate wins.
type fileSlot struct {
	file     FileRef
	name     string
	priority int
}

// stemBucket collects the files that share a stem before they become a group.
type stemBucket struct {
	stem     string
	raws     []fileSlot
	jpegs    []fileSlot
	sidecars []FileRef
}

// noteStem keeps the lowest case-preserved spelling of the stem, so a bucket
// built from "dscf1234.raf" and "DSCF1234.JPG" is named the same whichever
// order the directory hands the two files over in.
func (b *stemBucket) noteStem(stem string) {
	if b.stem == "" || stem < b.stem {
		b.stem = stem
	}
}

// add files the slot under the class its extension put it in.
func (b *stemBucket) add(class string, s fileSlot) {
	switch class {
	case "raw":
		b.raws = append(b.raws, s)
	case "jpeg":
		b.jpegs = append(b.jpegs, s)
	case "sidecar":
		b.sidecars = append(b.sidecars, s.file)
	}
}

// pickPrimary chooses a group's primary file from the candidates of one class
// and warns about the ones it passed over. Candidates are ordered by the
// configured extension priority, then by name so that the warning reads the
// same however the directory was walked.
func pickPrimary(slots []fileSlot) (*FileRef, []string) {
	if len(slots) == 0 {
		return nil, nil
	}
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].priority != slots[j].priority {
			return slots[i].priority < slots[j].priority
		}
		return slots[i].name < slots[j].name
	})
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

// group turns a bucket into a PhotoGroup. A bucket holding only sidecars has
// no frame to describe and reports false.
func (b *stemBucket) group(dir string) (PhotoGroup, bool) {
	raw, rawWarns := pickPrimary(b.raws)
	jpeg, jpegWarns := pickPrimary(b.jpegs)
	if raw == nil && jpeg == nil {
		return PhotoGroup{}, false // sidecar with no parent image; leave it alone
	}
	sidecars := b.sidecars
	sort.Slice(sidecars, func(i, j int) bool { return sidecars[i].Path < sidecars[j].Path })
	g := PhotoGroup{
		Dir:      dir,
		Stem:     b.stem,
		Raw:      raw,
		Jpeg:     jpeg,
		Sidecars: sidecars,
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
	// The walk reads no file, so all it can offer is the primary file's mtime.
	// That is the day the file was written, not the day the photograph was
	// taken, and on a library assembled by copying folders about the two are
	// nothing alike — the catalogue therefore replaces this with the EXIF
	// capture time while it is already reading the file to hash it, and
	// records which of the two a row ended up holding.
	if jpeg != nil {
		g.Shot = jpeg.ModTime
	} else {
		g.Shot = raw.ModTime
	}
	return g, true
}

// ScanDir reads one directory (non-recursive — groups are never merged across
// directories) and returns its PhotoGroups sorted by stem. Unrecognised
// extensions are ignored, never touched.
//
// It walks the whole directory before returning anything. Callers that paint
// frames as they arrive want ScanDirStream instead.
func ScanDir(dir string, cfg Config) ([]PhotoGroup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	buckets := make(map[string]*stemBucket)
	// The last link whose target exists but could not be reached. Harmless
	// while anything else scans; decisive when nothing does — a folder of
	// frames locked away behind links must not read as an emptied folder.
	var unreachable error

	get := func(name string) *stemBucket {
		key, stem := stemKey(name, cfg)
		b, ok := buckets[key]
		if !ok {
			b = &stemBucket{}
			buckets[key] = b
		}
		b.noteStem(stem)
		return b
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Hidden files are never frames. macOS writes AppleDouble companions
		// (._NAME.RAF) onto SMB and exFAT volumes that carry real image
		// extensions but hold resource-fork metadata.
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		class, prio := cfg.class(ext)
		if class == "" {
			continue
		}
		path := filepath.Join(dir, name)
		var info os.FileInfo
		if e.Type()&os.ModeSymlink != 0 {
			// A DirEntry describes the link itself. The frame is the target:
			// its size and mtime are what move when the file is edited. A
			// link whose target cannot be statted — dangling, looping, or
			// aimed somewhere unreadable — is junk rather than a frame, and
			// junk is skipped: one broken link must not take a folder of
			// readable frames down with it. A link that leads to a directory
			// — which slips past IsDir above, lstat not following links — is
			// no frame either.
			target, err := os.Stat(path)
			if err != nil {
				// A dangling link's target is gone, and gone is gone. Any
				// other failure means the frame exists but cannot be reached,
				// which matters below if nothing else in the folder can be.
				if !os.IsNotExist(err) {
					unreachable = fmt.Errorf("scan %s: %w", path, err)
				}
				continue
			}
			if target.IsDir() {
				continue
			}
			info = target
		} else {
			fi, err := e.Info()
			if err != nil {
				if os.IsNotExist(err) {
					continue // vanished mid-scan; gone is gone
				}
				// A stat failure on a regular entry hides a file that is still
				// there — a directory with list permission but no traverse
				// fails every one this way. Returning zero groups with a nil
				// error would be indistinguishable from an emptied folder,
				// which is grounds for a catalogue to forget frames that are
				// still on disk. Failing is the honest answer: this scan
				// cannot say what the directory holds.
				return nil, fmt.Errorf("scan %s: %w", path, err)
			}
			info = fi
		}
		ref := FileRef{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		b := get(name)
		b.add(class, fileSlot{ref, name, prio})
	}

	var groups []PhotoGroup
	for _, b := range buckets {
		if g, ok := b.group(dir); ok {
			groups = append(groups, g)
		}
	}
	// Every recognised entry was a link to somewhere unreachable: zero groups
	// with a nil error is what tells a catalogue the folder was emptied, and
	// these frames are merely locked away. Failing is the honest answer, the
	// same one an untraversable directory gets.
	if len(groups) == 0 && unreachable != nil {
		return nil, unreachable
	}

	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Stem) < strings.ToLower(groups[j].Stem)
	})
	return groups, nil
}

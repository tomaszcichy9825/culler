package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// Volume is one mounted filesystem, as the import screens list it.
//
// Removable and Network are hints, not gates: the app uses them to decide what
// to offer first and what never to write to, and a wrong answer must degrade
// into a volume the user can still reach by typing its path. Total and Free are
// zero when the operating system would not say, which the UI draws as no
// capacity bar rather than as an empty disk.
type Volume struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Removable bool   `json:"removable"`
	Network   bool   `json:"network"`
	Total     int64  `json:"total"`
	Free      int64  `json:"free"`
}

// VolumeLister enumerates what is mounted. One interface, one implementation
// per operating system, exactly as the trash is done.
type VolumeLister interface {
	Volumes() ([]Volume, error)
}

// Volumes lists the volumes mounted on this machine, the boot volume first.
func Volumes() ([]Volume, error) {
	return listVolumes()
}

// SystemVolumes is the lister for the machine the app is running on.
func SystemVolumes() VolumeLister { return systemLister{} }

type systemLister struct{}

func (systemLister) Volumes() ([]Volume, error) { return Volumes() }

// mountTree is how the Unix-alikes enumerate: a boot volume that is always
// there, and directories whose immediate children are the other mount points.
// Every question only the kernel can answer goes through describe, so the walk
// itself is portable and tested against a temporary directory tree.
type mountTree struct {
	// boot is the volume the app is running from. It leads the list and is
	// never reported removable, whatever the flags on it say.
	boot string

	// parents are the directories mount points appear under: /Volumes on
	// macOS, /media and /mnt on Linux. A parent that does not exist on this
	// machine is skipped, not an error.
	parents []string

	// describe answers what the operating system knows about one mount point.
	// nil leaves every volume with no capacity and not removable, which is what
	// the portable tests exercise.
	describe func(path string) (removable bool, total, free int64)
}

// list walks the tree. Unreadable parents and mount points that vanish under
// the walk are skipped: enumerating volumes runs while cards are being pulled
// out, and a half-answer the user can act on beats an error they cannot.
func (t mountTree) list() []Volume {
	bootPath := filepath.Clean(t.boot)
	bootReal := realPath(bootPath)
	bootName := filepath.Base(bootPath)

	isParent := make(map[string]bool, len(t.parents))
	for _, p := range t.parents {
		isParent[filepath.Clean(p)] = true
	}

	seen := map[string]bool{bootReal: true}
	var mounted []Volume
	for _, parent := range t.parents {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join(parent, e.Name())
			// Stat rather than the entry's own type: the boot volume appears
			// under /Volumes as a symlink, and so do hand-made mounts.
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			real := realPath(path)
			if real == bootReal {
				// macOS links the boot volume into /Volumes under the name the
				// user gave it, which is the name to show — but it is the same
				// volume, not a second one.
				bootName = e.Name()
				continue
			}
			if isParent[path] || seen[real] {
				continue
			}
			seen[real] = true
			mounted = append(mounted, t.volume(path, e.Name(), false))
		}
	}

	out := make([]Volume, 0, len(mounted)+1)
	out = append(out, t.volume(bootPath, bootName, true))
	return append(out, mounted...)
}

// volume fills in one mount point.
func (t mountTree) volume(path, name string, boot bool) Volume {
	v := Volume{Path: path, Name: name, Network: IsNetwork(path)}
	if t.describe != nil {
		v.Removable, v.Total, v.Free = t.describe(path)
	}
	if boot {
		v.Removable = false
	}
	return v
}

// realPath resolves symlinks so that two ways of reaching one mount point are
// recognised as the same volume. A path that will not resolve — it has just
// been unmounted — is its own answer rather than an error.
func realPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// underAny reports whether path is one of roots or sits inside one. It is how
// the Linux lister decides a mount point is removable from where the desktop
// put it.
func underAny(path string, roots ...string) bool {
	clean := filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

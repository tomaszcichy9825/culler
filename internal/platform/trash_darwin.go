//go:build darwin

package platform

import (
	"os"
	"path/filepath"
)

// SystemTrasher returns the platform's user-recoverable trash: ~/.Trash, which
// Finder shows as the Trash for the boot volume.
func SystemTrasher() (Trasher, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return homeTrash(filepath.Join(home, ".Trash")), nil
}

// homeTrasher moves files into a Finder trash directory. Finder's "Put Back"
// needs a com.apple.metadata:_kMDItemUserTagsTrashPutBackPath entry that only
// the private Finder API writes, so put-back is out of scope: recovery outside
// the app is a manual drag out of the Trash, and inside the app it is the
// journal's undo.
type homeTrasher struct {
	dir string
}

// homeTrash returns a Trasher writing into dir.
func homeTrash(dir string) homeTrasher {
	return homeTrasher{dir: dir}
}

func (t homeTrasher) Trash(path string) (string, error) {
	return DirTrasher{Dir: t.dir}.Trash(path)
}

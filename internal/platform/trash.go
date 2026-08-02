// Package platform holds everything that differs per operating system —
// trash, volume listing, cache directories — behind interfaces. No
// runtime.GOOS checks are allowed outside this package.
package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Trasher moves a file to a recoverable location. It returns the path the
// file ended up at, which the journal records so undo can bring it back.
// Deletion in this app is never os.Remove.
type Trasher interface {
	Trash(path string) (recovered string, err error)
}

// DirTrasher trashes by moving files into Dir with a collision-proof name.
// It backs both the user-facing trash (pointed at the OS trash directory)
// and the _Rejected folder mode.
type DirTrasher struct {
	Dir string
}

// Trash moves path into t.Dir, never overwriting anything already there.
func (t DirTrasher) Trash(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return "", err
	}
	dst, err := UniquePath(filepath.Join(t.Dir, filepath.Base(path)))
	if err != nil {
		return "", err
	}
	if err := MoveFile(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// UniquePath returns path if nothing exists there, otherwise the first
// "name-2.ext", "name-3.ext", … that is free.
func UniquePath(path string) (string, error) {
	return uniqueName(path, pathFree)
}

// uniqueName returns the first of path, "name-2.ext", "name-3.ext", … that
// free accepts. Trash backends that have to keep two directories in step pass
// their own predicate so every backend numbers collisions the same way.
func uniqueName(path string, free func(string) bool) (string, error) {
	if free(path) {
		return path, nil
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if free(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free name for %s", path)
}

// pathFree reports whether nothing at all exists at path, symlinks included.
func pathFree(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

// MoveFile renames src to dst, falling back to copy+remove when the rename
// crosses filesystems (card to disk, disk to network share).
func MoveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := CopyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// CopyFile copies src to a new file at dst, refusing to overwrite, and syncs
// before returning. On any failure the partial destination is removed.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

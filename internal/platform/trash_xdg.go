package platform

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// trashInfoDateLayout is the DeletionDate format the freedesktop.org trash
// spec requires: local time, no zone offset.
const trashInfoDateLayout = "2006-01-02T15:04:05"

// xdgTrasher implements the freedesktop.org Trash specification v1.0. Files
// move into root/files and a matching root/info/NAME.trashinfo records where
// each came from, which is what lets a Linux file manager offer "restore".
type xdgTrasher struct {
	root string
}

var _ Trasher = xdgTrasher{}

// xdgTrash returns a Trasher backed by the trash directory at root.
func xdgTrash(root string) xdgTrasher {
	return xdgTrasher{root: root}
}

// xdgTrashRoot resolves $XDG_DATA_HOME/Trash, defaulting to
// ~/.local/share/Trash. The spec requires a relative XDG_DATA_HOME to be
// ignored rather than resolved against the working directory.
func xdgTrashRoot() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

// Trash moves path into root/files under a collision-proof name and writes the
// matching info file. The returned path is the one in files/, so undo is a
// plain MoveFile back; the info file it orphans is metadata only.
//
// If the info file cannot be written the move is rolled back. When even the
// rollback fails the file is in the trash and the returned path is non-empty
// alongside the error, so a caller can still record where it went.
func (t xdgTrasher) Trash(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	filesDir := filepath.Join(t.root, "files")
	infoDir := filepath.Join(t.root, "info")
	for _, dir := range []string{filesDir, infoDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}

	dst, err := uniqueName(filepath.Join(filesDir, filepath.Base(abs)), func(candidate string) bool {
		return pathFree(candidate) && pathFree(trashInfoPath(infoDir, candidate))
	})
	if err != nil {
		return "", err
	}

	// Claim the info name before the move so files/ and info/ cannot drift.
	info := trashInfoPath(infoDir, dst)
	claim, err := os.OpenFile(info, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	claim.Close()

	if err := MoveFile(abs, dst); err != nil {
		os.Remove(info)
		return "", err
	}
	if err := writeTrashInfo(info, abs, time.Now()); err != nil {
		if rollback := MoveFile(dst, abs); rollback != nil {
			return dst, err
		}
		os.Remove(info)
		return "", err
	}
	return dst, nil
}

// trashInfoPath maps a path in files/ to its counterpart in info/.
func trashInfoPath(infoDir, filesPath string) string {
	return filepath.Join(infoDir, filepath.Base(filesPath)+".trashinfo")
}

// writeTrashInfo writes the info file atomically so a reader never sees a
// half-written record pointing at a file that has already moved.
func writeTrashInfo(path, original string, deleted time.Time) error {
	body := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: original}).EscapedPath(), deleted.Format(trashInfoDateLayout))

	tmp, err := os.CreateTemp(filepath.Dir(path), ".trashinfo-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

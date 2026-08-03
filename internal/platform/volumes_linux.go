//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"syscall"
)

// autoMountRoots are the directories a desktop's automounter uses. udisks2
// puts removable media in /media/<user> or /run/media/<user>; /mnt is where
// people mount things by hand and is not treated as removable.
var autoMountRoots = []string{"/media", "/run/media"}

// listVolumes enumerates the boot volume and the mount points under the
// directories a Linux desktop uses.
func listVolumes() ([]Volume, error) {
	return mountTree{
		boot:     "/",
		parents:  mountParents(),
		describe: describeVolume,
	}.list(), nil
}

// mountParents is where to look for mount points. The per-user directories are
// included as parents as well as the ones above them, so that both the
// /media/CARD layout and the /media/<user>/CARD layout are enumerated and the
// per-user directory itself is never mistaken for a volume.
func mountParents() []string {
	parents := []string{"/media", "/run/media", "/mnt"}
	if user := os.Getenv("USER"); user != "" {
		parents = append(parents,
			filepath.Join("/media", user),
			filepath.Join("/run/media", user),
		)
	}
	return parents
}

// describeVolume asks the kernel about one mount point. A statfs that fails —
// the card was pulled between the directory read and this call — answers with
// nothing rather than a guess.
func describeVolume(path string) (removable bool, total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, 0, 0
	}
	total = int64(st.Blocks) * st.Bsize
	// Bavail, not Bfree: the blocks reserved for root are not space an import
	// can land in.
	free = int64(st.Bavail) * st.Bsize
	return removableMount(path), total, free
}

// removableMount reports whether a mount point is removable media, judged by
// where the desktop put it.
//
// The exact answer lives in /sys/block/<device>/removable, and reaching it
// means mapping a mount point back to its device through /proc/self/mountinfo,
// then back again through the device-mapper for anything encrypted. The mount
// point is the cheaper signal and is nearly as good: udisks2 auto-mounts under
// /media or /run/media and mounts nothing else there. A card mounted by hand
// under /mnt is missed and shows as an ordinary volume, which the user can
// still browse and import from — the list is a shortcut, not a gate.
func removableMount(path string) bool {
	return underAny(path, autoMountRoots...)
}

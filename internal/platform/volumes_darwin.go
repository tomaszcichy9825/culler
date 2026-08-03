//go:build darwin

package platform

import (
	"strings"
	"syscall"
)

// Mount flags from <sys/mount.h>.
const (
	mntRemovable = 0x00000200
	mntLocalFlag = 0x00001000
)

// removableTypes are the filesystems a camera card, a phone or a USB stick
// arrives formatted as. They stand in for the removable flag on the readers
// that do not set it — see removableFrom.
var removableTypes = map[string]bool{
	"exfat":  true,
	"msdos":  true, // FAT16 and FAT32, which is most cards under 32 GB
	"ntfs":   true,
	"cd9660": true,
	"udf":    true,
}

// listVolumes enumerates the boot volume and everything mounted under
// /Volumes, which is where macOS puts every other disk.
func listVolumes() ([]Volume, error) {
	return mountTree{
		boot:     "/",
		parents:  []string{"/Volumes"},
		describe: describeVolume,
	}.list(), nil
}

// describeVolume asks the kernel about one mount point. A statfs that fails —
// the card was pulled between the directory read and this call — answers with
// nothing rather than a guess.
func describeVolume(path string) (removable bool, total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, 0, 0
	}
	total = int64(st.Blocks) * int64(st.Bsize)
	// Bavail, not Bfree: the reserve an APFS volume keeps for itself is not
	// space an import can land in.
	free = int64(st.Bavail) * int64(st.Bsize)
	return removableFrom(st.Flags, fsTypeName(st.Fstypename)), total, free
}

// removableFrom decides whether a mount point is removable media.
//
// MNT_REMOVABLE is the honest answer and is taken whenever the kernel sets it,
// which it does for most card readers and USB enclosures. It is not set by all
// of them, so the filesystem type stands in: a locally mounted exFAT or FAT
// volume is a card, a stick or a phone in all but the rarest setup. The known
// wrong answer is an external SSD formatted exFAT for sharing with Windows,
// which this calls removable. That is the safer direction to be wrong in — the
// app offers to import from it and refuses to write a cache to it, and both are
// harmless — where missing a real card means the user cannot see it at all.
//
// A network share is never removable whatever it is formatted as: it is not a
// disk anybody can pull out, and the import screens must not offer to copy a
// share onto itself.
func removableFrom(flags uint32, fsType string) bool {
	if flags&mntLocalFlag == 0 {
		return false
	}
	if flags&mntRemovable != 0 {
		return true
	}
	return removableTypes[fsType]
}

// fsTypeName reads the NUL-terminated filesystem name out of a statfs.
func fsTypeName(raw [16]int8) string {
	b := make([]byte, 0, len(raw))
	for _, c := range raw {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return strings.ToLower(string(b))
}

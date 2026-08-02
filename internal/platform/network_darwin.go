package platform

import "syscall"

// IsNetwork reports whether path lives on a network volume. Darwin sets
// MNT_LOCAL on local filesystems, so its absence means SMB/AFP/NFS/WebDAV.
// Unknown or unreachable paths report false — callers treat network status
// as a hint, never a gate.
func IsNetwork(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	const mntLocal = 0x00001000 // MNT_LOCAL from <sys/mount.h>
	return st.Flags&mntLocal == 0
}

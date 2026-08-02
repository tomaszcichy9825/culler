package platform

import "syscall"

// IsNetwork reports whether path lives on a network filesystem, matched by
// statfs magic number. Unknown or unreachable paths report false — network
// status is a hint, never a gate.
func IsNetwork(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	switch uint32(st.Type) {
	case 0x6969, // NFS
		0x517B,     // SMB
		0xFE534D42, // SMB2
		0xFF534D42, // CIFS
		0x73757245, // Coda
		0x47504653: // GPFS
		return true
	}
	return false
}

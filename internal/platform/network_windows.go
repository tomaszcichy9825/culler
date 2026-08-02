package platform

import (
	"strings"
	"syscall"
	"unsafe"
)

// IsNetwork reports whether path is a UNC path or a mapped network drive.
// Unknown or unreachable paths report false — network status is a hint,
// never a gate.
func IsNetwork(path string) bool {
	if strings.HasPrefix(path, `\\`) {
		return true
	}
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	root, err := syscall.UTF16PtrFromString(path[:2] + `\`)
	if err != nil {
		return false
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDriveTypeW")
	const driveRemote = 4
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(root)))
	return ret == driveRemote
}

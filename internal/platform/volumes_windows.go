//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

// Drive types from GetDriveTypeW.
const (
	driveNoRootDir = 1
	driveRemovable = 2
	driveRemote    = 4
	driveCDROM     = 5
)

// listVolumes enumerates the drive letters that are mounted. Windows has no
// directory of mount points, so the letters are the volumes; a lettered mount
// of a folder is reached the same way, by its letter.
//
// There is no boot volume entry: the system drive is already one of the
// letters, and adding it again would list it twice.
func listVolumes() ([]Volume, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives := kernel32.NewProc("GetLogicalDrives")
	getDriveType := kernel32.NewProc("GetDriveTypeW")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")
	getVolumeInformation := kernel32.NewProc("GetVolumeInformationW")

	mask, _, _ := getLogicalDrives.Call()
	out := []Volume{}
	for i := uint(0); i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		wide, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		kind, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(wide)))
		if kind == driveNoRootDir {
			// A letter the system reports and cannot reach: a card reader with
			// nothing in it. Listing it would offer an import from an empty
			// slot.
			continue
		}
		v := Volume{
			Path: root,
			Name: driveLabel(getVolumeInformation, wide, root),
			// An optical disc is removable media too, and the import screens
			// treat it the same: read from it, never write to it.
			Removable: kind == driveRemovable || kind == driveCDROM,
			Network:   kind == driveRemote,
		}
		v.Total, v.Free = driveSpace(getDiskFreeSpaceEx, wide)
		out = append(out, v)
	}
	return out, nil
}

// driveLabel reads the volume's label — "UNTITLED", "Samsung T7" — falling
// back to the drive letter, which is always something to point at.
func driveLabel(proc *syscall.LazyProc, root *uint16, fallback string) string {
	var name [syscall.MAX_PATH + 1]uint16
	ok, _, _ := proc.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(len(name)),
		0, 0, 0, 0, 0,
	)
	if ok == 0 {
		return fallback
	}
	label := syscall.UTF16ToString(name[:])
	if label == "" {
		return fallback
	}
	return label
}

// driveSpace reports the capacity of one drive, zero when the call fails —
// which is what a card pulled between the two calls looks like.
func driveSpace(proc *syscall.LazyProc, root *uint16) (total, free int64) {
	// Available, not free: the two differ under a disk quota, and the
	// available figure is the one an import can land in.
	var available, capacity, unused uint64
	ok, _, _ := proc.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&capacity)),
		uintptr(unsafe.Pointer(&unused)),
	)
	if ok == 0 {
		return 0, 0
	}
	return int64(capacity), int64(available)
}

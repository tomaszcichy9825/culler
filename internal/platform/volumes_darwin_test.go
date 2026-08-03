//go:build darwin

package platform

import "testing"

func TestRemovableFromReadsTheKernelFlagFirst(t *testing.T) {
	if !removableFrom(mntLocalFlag|mntRemovable, "apfs") {
		t.Error("MNT_REMOVABLE is the kernel saying so and must be believed")
	}
	if removableFrom(mntLocalFlag, "apfs") {
		t.Error("an internal APFS volume is not removable")
	}
}

func TestRemovableFromFallsBackToTheFilesystem(t *testing.T) {
	// Not every reader sets the flag. A locally mounted exFAT or FAT volume is
	// a card in all but the rarest setup.
	for _, fsType := range []string{"exfat", "msdos"} {
		if !removableFrom(mntLocalFlag, fsType) {
			t.Errorf("a local %s volume must read as removable", fsType)
		}
	}
}

func TestRemovableFromNeverCallsAShareRemovable(t *testing.T) {
	// MNT_LOCAL absent means SMB, AFP or NFS. A share formatted exFAT behind
	// the scenes is still not a disk anyone can pull out.
	if removableFrom(0, "exfat") {
		t.Error("a network share reported as removable media")
	}
}

func TestFsTypeNameStopsAtTheTerminator(t *testing.T) {
	var raw [16]int8
	for i, c := range []byte("ExFAT") {
		raw[i] = int8(c)
	}
	if got := fsTypeName(raw); got != "exfat" {
		t.Errorf("fsTypeName = %q, want %q", got, "exfat")
	}
}

func TestDescribeVolumeAnswersForTheBootVolume(t *testing.T) {
	removable, total, free := describeVolume("/")
	if total <= 0 || free < 0 || free > total {
		t.Errorf("boot volume reports %d free of %d", free, total)
	}
	if removable {
		t.Error("the boot volume reported as removable media")
	}
}

func TestDescribeVolumeSaysNothingAboutAPathThatIsNotThere(t *testing.T) {
	removable, total, free := describeVolume("/definitely/not/a/real/mount")
	if removable || total != 0 || free != 0 {
		t.Errorf("an unreachable path answered %v %d %d, want nothing", removable, total, free)
	}
}

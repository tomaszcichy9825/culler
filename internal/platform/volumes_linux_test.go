//go:build linux

package platform

import (
	"os"
	"slices"
	"testing"
)

func TestRemovableMountReadsWhereTheDesktopPutIt(t *testing.T) {
	removable := []string{
		"/media/CARD",
		"/media/tomasz/UNTITLED",
		"/run/media/tomasz/UNTITLED",
	}
	for _, path := range removable {
		if !removableMount(path) {
			t.Errorf("%s is where an automounter puts a card and must read as removable", path)
		}
	}

	fixed := []string{"/", "/mnt/archive", "/home/tomasz/Pictures", "/mediatheque"}
	for _, path := range fixed {
		if removableMount(path) {
			t.Errorf("%s read as removable media", path)
		}
	}
}

func TestMountParentsCoverBothAutomountLayouts(t *testing.T) {
	t.Setenv("USER", "tomasz")
	got := mountParents()

	for _, want := range []string{"/media", "/run/media", "/mnt", "/media/tomasz", "/run/media/tomasz"} {
		if !slices.Contains(got, want) {
			t.Errorf("mountParents() = %v, missing %q", got, want)
		}
	}
}

func TestMountParentsWithoutAUserStillCoverTheSharedDirectories(t *testing.T) {
	if err := os.Unsetenv("USER"); err != nil {
		t.Fatal(err)
	}
	got := mountParents()
	if len(got) != 3 {
		t.Errorf("mountParents() = %v, want the three shared directories only", got)
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

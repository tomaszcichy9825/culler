package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// mount makes one mount point under parent and returns its path.
func mount(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// link makes a symlink, skipping the test when the platform will not allow one
// — unprivileged Windows will not, and the rules being tested here are the
// same everywhere else.
func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// paths is the listed volumes' paths, which is what most of these assert on.
func paths(vols []Volume) []string {
	out := make([]string, 0, len(vols))
	for _, v := range vols {
		out = append(out, v.Path)
	}
	return out
}

func TestMountTreeListsBootThenChildrenOfParents(t *testing.T) {
	root := t.TempDir()
	boot := mount(t, root, "boot")
	volumes := mount(t, root, "Volumes")
	card := mount(t, volumes, "UNTITLED")
	disk := mount(t, volumes, "Archive")

	got := mountTree{boot: boot, parents: []string{volumes}}.list()

	// The boot volume leads, then the mount points in the order the directory
	// read gives them, which os.ReadDir sorts by name.
	want := []string{boot, disk, card}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", paths(got), want)
	}
	for i, path := range want {
		if got[i].Path != path {
			t.Errorf("volume %d = %q, want %q", i, got[i].Path, path)
		}
	}
	if got[2].Name != "UNTITLED" {
		t.Errorf("name = %q, want the mount point's own name", got[2].Name)
	}
}

func TestMountTreeSkipsHiddenEntriesAndFiles(t *testing.T) {
	root := t.TempDir()
	volumes := mount(t, root, "Volumes")
	mount(t, volumes, ".Spotlight-V100")
	card := mount(t, volumes, "CARD")
	if err := os.WriteFile(filepath.Join(volumes, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := mountTree{boot: filepath.Join(root, "boot"), parents: []string{volumes}}.list()

	if len(got) != 2 || got[1].Path != card {
		t.Fatalf("listed %v, want the boot volume and %q only", paths(got), card)
	}
}

func TestMountTreeIgnoresAParentThatIsNotThere(t *testing.T) {
	root := t.TempDir()
	media := mount(t, root, "media")
	card := mount(t, media, "CARD")

	// /run/media exists on some desktops and not others. A missing one is a
	// fact about the machine, not a failure to enumerate it.
	got := mountTree{
		boot:    filepath.Join(root, "boot"),
		parents: []string{filepath.Join(root, "run", "media"), media},
	}.list()

	if len(got) != 2 || got[1].Path != card {
		t.Fatalf("listed %v, want the boot volume and %q only", paths(got), card)
	}
}

func TestMountTreeNamesTheBootVolumeAfterItsLink(t *testing.T) {
	root := t.TempDir()
	boot := mount(t, root, "boot")
	volumes := mount(t, root, "Volumes")
	link(t, boot, filepath.Join(volumes, "Macintosh HD"))

	got := mountTree{boot: boot, parents: []string{volumes}}.list()

	// macOS puts a link to the boot volume in /Volumes under the name the user
	// gave it. That name belongs to the boot volume, not to a second one.
	if len(got) != 1 {
		t.Fatalf("listed %v, want the boot volume alone", paths(got))
	}
	if got[0].Path != boot {
		t.Errorf("path = %q, want the boot volume %q", got[0].Path, boot)
	}
	if got[0].Name != "Macintosh HD" {
		t.Errorf("name = %q, want the name its link carries", got[0].Name)
	}
}

func TestMountTreeSkipsAParentThatIsAlsoAChild(t *testing.T) {
	root := t.TempDir()
	media := mount(t, root, "media")
	perUser := mount(t, media, "tomasz")
	card := mount(t, perUser, "CARD")

	// /media/<user> is where udisks2 mounts, and it is also a child of /media.
	// It is a directory holding mount points, never a volume itself.
	got := mountTree{
		boot:    filepath.Join(root, "boot"),
		parents: []string{media, perUser},
	}.list()

	if len(got) != 2 || got[1].Path != card {
		t.Fatalf("listed %v, want the boot volume and %q only", paths(got), card)
	}
}

func TestMountTreeListsOneMountOnceWhateverReachesIt(t *testing.T) {
	root := t.TempDir()
	media := mount(t, root, "media")
	mnt := mount(t, root, "mnt")
	card := mount(t, media, "CARD")
	link(t, card, filepath.Join(mnt, "card"))

	got := mountTree{boot: filepath.Join(root, "boot"), parents: []string{media, mnt}}.list()

	if len(got) != 2 || got[1].Path != card {
		t.Fatalf("listed %v, want the boot volume and %q once", paths(got), card)
	}
}

func TestMountTreeCarriesWhatTheSystemAnswers(t *testing.T) {
	root := t.TempDir()
	boot := mount(t, root, "boot")
	volumes := mount(t, root, "Volumes")
	card := mount(t, volumes, "CARD")

	got := mountTree{
		boot:    boot,
		parents: []string{volumes},
		describe: func(path string) (bool, int64, int64) {
			if path == card {
				return true, 64 << 30, 12 << 30
			}
			// The boot volume answers removable here on purpose: nothing the
			// system says may make the disk the app is running from a card.
			return true, 500 << 30, 100 << 30
		},
	}.list()

	if len(got) != 2 {
		t.Fatalf("listed %v, want two volumes", paths(got))
	}
	if got[0].Removable {
		t.Error("the boot volume is never removable")
	}
	if got[0].Total != 500<<30 || got[0].Free != 100<<30 {
		t.Errorf("boot capacity = %d/%d, want the described figures", got[0].Free, got[0].Total)
	}
	if !got[1].Removable {
		t.Error("the card is removable and was not reported so")
	}
	if got[1].Total != 64<<30 || got[1].Free != 12<<30 {
		t.Errorf("card capacity = %d/%d, want the described figures", got[1].Free, got[1].Total)
	}
}

func TestMountTreeSurvivesAnUnreadableParent(t *testing.T) {
	root := t.TempDir()
	shut := mount(t, root, "shut")
	if err := os.Chmod(shut, 0o000); err != nil {
		t.Skipf("cannot close a directory here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o755) })
	if _, err := os.ReadDir(shut); err == nil {
		t.Skip("directory is readable anyway, likely running as root")
	}

	got := mountTree{boot: filepath.Join(root, "boot"), parents: []string{shut}}.list()

	if len(got) != 1 {
		t.Fatalf("listed %v, want the boot volume alone", paths(got))
	}
}

func TestVolumesListsTheMachineItRunsOn(t *testing.T) {
	got, err := Volumes()
	if err != nil {
		t.Fatalf("Volumes: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no volumes at all: every machine has at least the one it booted from")
	}
	for _, v := range got {
		if v.Path == "" {
			t.Errorf("volume with no path: %+v", v)
		}
		if v.Name == "" {
			t.Errorf("volume %q has no name", v.Path)
		}
		if v.Free > v.Total {
			t.Errorf("volume %q reports %d free of %d", v.Path, v.Free, v.Total)
		}
	}
}

func TestSystemVolumesIsAVolumeLister(t *testing.T) {
	var lister VolumeLister = SystemVolumes()
	got, err := lister.Volumes()
	if err != nil {
		t.Fatalf("Volumes: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("the system lister found nothing mounted")
	}
}

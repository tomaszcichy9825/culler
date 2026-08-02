package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// trashInfoField returns the value of key in a .trashinfo file.
func trashInfoField(t *testing.T, path, key string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trashinfo: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if lines[0] != "[Trash Info]" {
		t.Fatalf("first line is %q, want [Trash Info]", lines[0])
	}
	for _, line := range lines[1:] {
		if name, value, ok := strings.Cut(line, "="); ok && name == key {
			return value
		}
	}
	t.Fatalf("no %s= line in %s", key, string(body))
	return ""
}

func TestXDGTrashLayout(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	p := filepath.Join(src, "DSCF0001.RAF")
	write(t, p, "rawbytes")

	recovered, err := xdgTrash(root).Trash(p)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "files", "DSCF0001.RAF")
	if recovered != want {
		t.Errorf("recovered = %s, want %s", recovered, want)
	}
	if got, err := os.ReadFile(recovered); err != nil || string(got) != "rawbytes" {
		t.Errorf("recovered file = %q, %v", got, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("source still exists")
	}

	info := filepath.Join(root, "info", "DSCF0001.RAF.trashinfo")
	if got := trashInfoField(t, info, "Path"); got != p {
		t.Errorf("Path = %s, want %s", got, p)
	}
	stamp := trashInfoField(t, info, "DeletionDate")
	when, err := time.ParseInLocation(trashInfoDateLayout, stamp, time.Local)
	if err != nil {
		t.Fatalf("DeletionDate %q unparseable: %v", stamp, err)
	}
	if d := time.Since(when); d < -time.Minute || d > time.Minute {
		t.Errorf("DeletionDate %s is %v away from now", stamp, d)
	}
}

func TestXDGTrashInfoEscapesPath(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "shoot day one")
	p := filepath.Join(src, "DSC 0001 ünïcode #2.RAF")
	write(t, p, "x")

	if _, err := xdgTrash(root).Trash(p); err != nil {
		t.Fatal(err)
	}

	info := filepath.Join(root, "info", "DSC 0001 ünïcode #2.RAF.trashinfo")
	got := trashInfoField(t, info, "Path")
	for _, want := range []string{"shoot%20day%20one", "DSC%200001%20", "%C3%BCn%C3%AF", "%232.RAF"} {
		if !strings.Contains(got, want) {
			t.Errorf("Path = %s, want it to contain %s", got, want)
		}
	}
	if strings.Contains(got, " ") {
		t.Errorf("Path = %s, want no literal spaces", got)
	}
	if !strings.HasPrefix(got, "/") {
		t.Errorf("Path = %s, want an absolute path with unescaped separators", got)
	}
}

func TestXDGTrashCollisionKeepsFilesAndInfoInSync(t *testing.T) {
	root, srcA, srcB := t.TempDir(), t.TempDir(), t.TempDir()
	pa := filepath.Join(srcA, "DSCF0001.RAF")
	pb := filepath.Join(srcB, "DSCF0001.RAF")
	write(t, pa, "a")
	write(t, pb, "b")

	tr := xdgTrash(root)
	ra, err := tr.Trash(pa)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := tr.Trash(pb)
	if err != nil {
		t.Fatal(err)
	}
	if ra == rb {
		t.Fatalf("collision silently overwrote: both at %s", ra)
	}
	if got, _ := os.ReadFile(ra); string(got) != "a" {
		t.Errorf("first file clobbered")
	}
	if got, _ := os.ReadFile(rb); string(got) != "b" {
		t.Errorf("second file clobbered")
	}

	for original, recovered := range map[string]string{pa: ra, pb: rb} {
		info := filepath.Join(root, "info", filepath.Base(recovered)+".trashinfo")
		if got := trashInfoField(t, info, "Path"); got != original {
			t.Errorf("%s: Path = %s, want %s", info, got, original)
		}
	}
}

func TestXDGTrashUndoRoundTrip(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	p := filepath.Join(src, "DSCF0001.RAF")
	write(t, p, "rawbytes")

	recovered, err := xdgTrash(root).Trash(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveFile(recovered, p); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "rawbytes" {
		t.Errorf("restored file = %q, %v", got, err)
	}
}

func TestXDGTrashMissingSource(t *testing.T) {
	if _, err := xdgTrash(t.TempDir()).Trash(filepath.Join(t.TempDir(), "nope.jpg")); err == nil {
		t.Fatal("want error for missing source")
	}
}

func TestXDGTrashRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"env set", "/data/xdg", filepath.Join("/data/xdg", "Trash")},
		{"env unset", "", filepath.Join(home, ".local", "share", "Trash")},
		{"env relative is ignored", "relative/xdg", filepath.Join(home, ".local", "share", "Trash")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", c.env)
			got, err := xdgTrashRoot()
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("xdgTrashRoot() = %s, want %s", got, c.want)
			}
		})
	}
}

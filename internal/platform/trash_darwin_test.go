package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemTrasherPointsAtHomeTrash(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	tr, err := SystemTrasher()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := tr.(homeTrasher)
	if !ok {
		t.Fatalf("SystemTrasher() = %T, want homeTrasher", tr)
	}
	if want := filepath.Join(home, ".Trash"); got.dir != want {
		t.Errorf("dir = %s, want %s", got.dir, want)
	}
}

func TestHomeTrasherMovesIntoOverriddenDir(t *testing.T) {
	trash, src := t.TempDir(), t.TempDir()
	p := filepath.Join(src, "DSCF0001.RAF")
	write(t, p, "rawbytes")

	recovered, err := homeTrash(trash).Trash(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(trash, "DSCF0001.RAF"); recovered != want {
		t.Errorf("recovered = %s, want %s", recovered, want)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("source still exists")
	}
	if err := MoveFile(recovered, p); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "rawbytes" {
		t.Errorf("restored file = %q, %v", got, err)
	}
}

func TestHomeTrasherCollisionKeepsBoth(t *testing.T) {
	trash, srcA, srcB := t.TempDir(), t.TempDir(), t.TempDir()
	pa := filepath.Join(srcA, "DSCF0001.RAF")
	pb := filepath.Join(srcB, "DSCF0001.RAF")
	write(t, pa, "a")
	write(t, pb, "b")

	tr := homeTrash(trash)
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
}

func TestHomeTrasherMissingSource(t *testing.T) {
	if _, err := homeTrash(t.TempDir()).Trash(filepath.Join(t.TempDir(), "nope.jpg")); err == nil {
		t.Fatal("want error for missing source")
	}
}

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirTrasherMovesFile(t *testing.T) {
	src := t.TempDir()
	trash := t.TempDir()
	p := filepath.Join(src, "DSCF0001.RAF")
	write(t, p, "rawbytes")

	tr := DirTrasher{Dir: trash}
	recovered, err := tr.Trash(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("source still exists")
	}
	got, err := os.ReadFile(recovered)
	if err != nil {
		t.Fatalf("recovered path unreadable: %v", err)
	}
	if string(got) != "rawbytes" {
		t.Errorf("content changed in transit")
	}
}

func TestDirTrasherCollisionKeepsBoth(t *testing.T) {
	srcA, srcB, trash := t.TempDir(), t.TempDir(), t.TempDir()
	pa := filepath.Join(srcA, "DSCF0001.RAF")
	pb := filepath.Join(srcB, "DSCF0001.RAF")
	write(t, pa, "a")
	write(t, pb, "b")

	tr := DirTrasher{Dir: trash}
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

func TestDirTrasherMissingSource(t *testing.T) {
	tr := DirTrasher{Dir: t.TempDir()}
	if _, err := tr.Trash(filepath.Join(t.TempDir(), "nope.jpg")); err == nil {
		t.Fatal("want error for missing source")
	}
}

package hash

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// write creates dir/name with the given contents and returns its path.
func write(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustHash(t *testing.T, path string) string {
	t.Helper()
	h, err := Content(path)
	if err != nil {
		t.Fatalf("Content(%s): %v", path, err)
	}
	if h == "" {
		t.Fatal("empty hash")
	}
	return h
}

func TestSameContentSameHash(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.raf", []byte("identical bytes"))
	b := write(t, dir, "b.raf", []byte("identical bytes"))

	if mustHash(t, a) != mustHash(t, b) {
		t.Error("same content must hash the same")
	}
}

func TestDifferentPrefixDifferentHash(t *testing.T) {
	dir := t.TempDir()
	// Same length, different bytes inside the first 64KB.
	a := write(t, dir, "a.raf", []byte("hello"))
	b := write(t, dir, "b.raf", []byte("world"))

	if mustHash(t, a) == mustHash(t, b) {
		t.Error("different content must hash differently")
	}
}

func TestSamePrefixDifferentSizeDifferentHash(t *testing.T) {
	dir := t.TempDir()
	// Both files are identical for the first 64KB and only differ in length,
	// so the size must be folded into the hash for these to separate.
	a := write(t, dir, "a.raf", bytes.Repeat([]byte("a"), prefixBytes+16))
	b := write(t, dir, "b.raf", bytes.Repeat([]byte("a"), prefixBytes+4096))

	if mustHash(t, a) == mustHash(t, b) {
		t.Error("same 64KB prefix but different size must hash differently")
	}
}

func TestSmallFile(t *testing.T) {
	dir := t.TempDir()
	small := write(t, dir, "small.jpg", []byte("tiny"))
	empty := write(t, dir, "empty.jpg", nil)

	if mustHash(t, small) == mustHash(t, empty) {
		t.Error("a short file and an empty file must not collide")
	}
}

func TestStableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.raf", bytes.Repeat([]byte("x"), 200_000))

	if mustHash(t, p) != mustHash(t, p) {
		t.Error("hash must be deterministic")
	}
}

func TestHexEncoded(t *testing.T) {
	dir := t.TempDir()
	got := mustHash(t, write(t, dir, "a.raf", []byte("x")))

	if len(got) != 64 {
		t.Errorf("want a 64-char sha256 hex string, got %d chars: %q", len(got), got)
	}
	for _, r := range got {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("non-hex character %q in hash %q", r, got)
		}
	}
}

func TestMissingFileErrors(t *testing.T) {
	if _, err := Content(filepath.Join(t.TempDir(), "nope.raf")); err == nil {
		t.Error("want an error for a missing file")
	}
}

func TestDirectoryErrors(t *testing.T) {
	// A directory is never a frame; hashing one must fail rather than return
	// a hash that would key a decision.
	if _, err := Content(t.TempDir()); err == nil {
		t.Error("want an error for a directory")
	}
}

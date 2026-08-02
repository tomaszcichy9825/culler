//go:build !windows

package platform

import "testing"

func TestLocalDirIsNotNetwork(t *testing.T) {
	if IsNetwork(t.TempDir()) {
		t.Fatal("temp dir reported as network volume")
	}
}

func TestMissingPathIsNotNetwork(t *testing.T) {
	if IsNetwork("/definitely/not/a/real/path") {
		t.Fatal("unreachable path must report false, not guess")
	}
}

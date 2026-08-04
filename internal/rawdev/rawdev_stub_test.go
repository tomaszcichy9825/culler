//go:build !libraw

package rawdev

import (
	"errors"
	"testing"
)

// The default build must say so plainly rather than half-working: this is the
// contract the preview handler and the loupe's fallback are both written
// against.
func TestUnavailableWithoutTheTag(t *testing.T) {
	if Available() {
		t.Fatal("Available() = true in a build without -tags libraw")
	}
	if _, err := Develop("/nowhere/DSCF0001.RAF"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Develop error = %v, want ErrUnavailable", err)
	}
}

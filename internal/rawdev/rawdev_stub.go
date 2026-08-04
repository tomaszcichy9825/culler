//go:build !libraw

// Package rawdev develops a RAW file into full-resolution RGB pixels — tier 3
// of the preview pipeline, which exists only so that 1:1 zoom on a RAW-only
// frame shows real sensor detail instead of an upscaled embedded preview.
//
// Demosaicing needs LibRaw, and LibRaw needs cgo, a system library, and a
// LGPL/CDDL dual licence that the MIT default build stays clear of. So the
// whole thing sits behind the `libraw` build tag: this file is what an
// ordinary build compiles, and it reports the tier as unavailable rather than
// failing to link. Nothing else in the app may assume Develop works —
// callers must handle ErrUnavailable as an ordinary outcome, not a fault.
package rawdev

import "errors"

// ErrUnavailable reports that this binary was built without the demosaicer.
// It is declared in both build variants so callers can test for it whichever
// way they were compiled.
var ErrUnavailable = errors.New("rawdev: built without LibRaw support (rebuild with -tags libraw)")

// Available reports whether Develop can do anything. False in this build.
func Available() bool { return false }

// Develop always fails here. The caller falls back to the embedded preview.
func Develop(path string) ([]byte, error) { return nil, ErrUnavailable }

package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// fixtureTree writes the awkward cases — a pair, a duplicate jpeg-class file,
// both sidecar spellings, mixed case, hidden companions, files that are not
// images and a subdirectory — into one directory.
func fixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{
		"DSCF1234.RAF", "DSCF1234.JPG", "DSCF1234.RAF.xmp",
		"DSCF1235.RAF", "DSCF1235.xmp",
		"IMG_0001.JPG", "IMG_0001.HEIC",
		"dscf9999.raf", "DSCF9999.JPG",
		"solo.arw",
		"lonely.xmp",
		"._DSCF1234.RAF", ".DS_Store", ".hidden.jpg",
		"notes.txt", "movie.mov",
		filepath.Join("sub", "DSCF1234.JPG"),
	} {
		touch(t, dir, name)
	}
	return dir
}

// collectStream runs a stream and returns the batches and their concatenation.
func collectStream(t *testing.T, dir string, opts StreamOptions) ([][]PhotoGroup, []PhotoGroup) {
	t.Helper()
	var batches [][]PhotoGroup
	var all []PhotoGroup
	err := ScanDirStreamContext(context.Background(), dir, DefaultConfig(), opts, func(batch []PhotoGroup) {
		batches = append(batches, batch)
		all = append(all, batch...)
	})
	if err != nil {
		t.Fatalf("ScanDirStreamContext: %v", err)
	}
	return batches, all
}

// The streamed walk and the batch walk must agree on every field of every
// group, not merely on how many groups there are.
func TestScanDirStreamMatchesScanDir(t *testing.T) {
	dir := fixtureTree(t)

	want, err := ScanDir(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, got := collectStream(t, dir, StreamOptions{BatchSize: 2})

	if len(got) != len(want) {
		t.Fatalf("streamed %d groups, ScanDir found %d", len(got), len(want))
	}
	if !reflect.DeepEqual(got, want) {
		for i := range want {
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Errorf("group %d:\n streamed %+v\n  ScanDir %+v", i, got[i], want[i])
			}
		}
		t.Fatal("streamed groups differ from ScanDir's")
	}
}

// Batching is bounded and never hands over an empty frame.
func TestScanDirStreamBatchesAreBounded(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg", "f.jpg", "g.jpg"} {
		touch(t, dir, name)
	}

	batches, all := collectStream(t, dir, StreamOptions{BatchSize: 3})
	if len(batches) < 3 {
		t.Fatalf("7 frames at a batch size of 3 arrived in %d batches; the walk is not streaming", len(batches))
	}
	for i, b := range batches {
		if len(b) == 0 {
			t.Errorf("batch %d is empty", i)
		}
		if len(b) > 3 {
			t.Errorf("batch %d holds %d frames, over the bound of 3", i, len(b))
		}
	}
	if len(all) != 7 {
		t.Errorf("streamed %d frames, want 7", len(all))
	}
}

// A batch that fills slowly is handed over on the interval rather than held.
func TestScanDirStreamFlushesOnInterval(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "b.jpg")
	touch(t, dir, "c.jpg")

	batches, all := collectStream(t, dir, StreamOptions{BatchSize: 1000, MaxDelay: time.Nanosecond})
	if len(batches) != 3 {
		t.Fatalf("an expired interval must flush each frame on its own; got %d batches", len(batches))
	}
	if len(all) != 3 {
		t.Errorf("streamed %d frames, want 3", len(all))
	}
}

// The emitted slice belongs to the caller; the scanner must not write into it
// again on the next batch.
func TestScanDirStreamBatchesAreNotReused(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "b.jpg")

	var kept [][]PhotoGroup
	err := ScanDirStreamContext(context.Background(), dir, DefaultConfig(), StreamOptions{BatchSize: 1},
		func(batch []PhotoGroup) { kept = append(kept, batch) })
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 {
		t.Fatalf("got %d batches, want 2", len(kept))
	}
	if kept[0][0].Stem != "a" || kept[1][0].Stem != "b" {
		t.Fatalf("a retained batch was overwritten: %q then %q", kept[0][0].Stem, kept[1][0].Stem)
	}
}

// The streamed walk treats symlinks exactly as ScanDir does: target metadata
// for a linked file, nothing at all for a link to a directory.
func TestScanDirStreamResolvesSymlinks(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "master.jpg")
	if err := os.WriteFile(target, make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "LINK0001.JPG")); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "FAKE.JPG")); err != nil {
		t.Fatal(err)
	}

	_, all := collectStream(t, dir, StreamOptions{})
	if len(all) != 1 || all[0].Stem != "LINK0001" || all[0].Jpeg == nil {
		t.Fatalf("streamed %+v, want LINK0001 alone", all)
	}
	if got := all[0].Jpeg.Size; got != 1234 {
		t.Errorf("size = %d, want the target's 1234, not the link's own", got)
	}
}

func TestScanDirStreamHiddenFilesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "DSCF1234.RAF")
	touch(t, dir, "._DSCF1234.RAF")
	touch(t, dir, ".DS_Store")
	touch(t, dir, ".hidden.jpg")

	_, all := collectStream(t, dir, StreamOptions{})
	if len(all) != 1 || all[0].Stem != "DSCF1234" {
		t.Fatalf("hidden files became frames: %+v", all)
	}
}

func TestScanDirStreamEmptyDirEmitsNothing(t *testing.T) {
	calls := 0
	err := ScanDirStream(t.TempDir(), DefaultConfig(), func([]PhotoGroup) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("empty folder produced %d batches, want none", calls)
	}
}

func TestScanDirStreamMissingDirectory(t *testing.T) {
	err := ScanDirStream(filepath.Join(t.TempDir(), "nope"), DefaultConfig(), func([]PhotoGroup) {
		t.Error("a missing folder must not emit frames")
	})
	if err == nil {
		t.Fatal("want an error for a missing directory")
	}
}

// A cancelled walk stops handing over frames; the folder switch that cancelled
// it is about to replace them.
func TestScanDirStreamStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg", "f.jpg"} {
		touch(t, dir, name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batches := 0
	err := ScanDirStreamContext(ctx, dir, DefaultConfig(), StreamOptions{BatchSize: 1}, func([]PhotoGroup) {
		batches++
		cancel()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if batches > 2 {
		t.Errorf("%d batches after the first was cancelled", batches)
	}
}

func TestScanDirStreamCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ScanDirStreamContext(ctx, fixtureTree(t), DefaultConfig(), StreamOptions{}, func([]PhotoGroup) {
		t.Error("a cancelled walk must not emit")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

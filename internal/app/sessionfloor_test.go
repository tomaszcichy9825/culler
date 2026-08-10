package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/config"
)

// floorService is an index service whose library shows shoots of at least
// minFrames, which is the setting the Sessions list is drawn against.
func floorService(t *testing.T, minFrames int) *LibraryIndexService {
	t.Helper()
	cfg := config.Default()
	cfg.Behaviour.MinSessionFrames = minFrames
	a := newAt(filepath.Join(t.TempDir(), "config.json"), t.TempDir(), cfg)
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	s := NewLibraryIndexService(a)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close index service: %v", err)
		}
	})
	return s
}

// floorTree indexes a two-frame shoot, a lone frame the next day, and a
// three-frame shoot the day after.
func floorTree(t *testing.T, s *LibraryIndexService) {
	t.Helper()
	dir := t.TempDir()
	shoot(t, dir, "SHOOT001", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))
	shoot(t, dir, "SHOOT002", time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC))
	shoot(t, dir, "STRAY001", time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC))
	shoot(t, dir, "BIG00001", time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC))
	shoot(t, dir, "BIG00002", time.Date(2026, 5, 3, 9, 30, 0, 0, time.UTC))
	shoot(t, dir, "BIG00003", time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC))
	if _, err := s.RegisterRoot(dir); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	if _, err := s.reindex(dir); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}

func TestSessionsHonourTheConfiguredFloor(t *testing.T) {
	s := floorService(t, 2)
	floorTree(t, s)

	list, err := s.Sessions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("%d sessions shown at a floor of 2, want 2: %+v", len(list.Sessions), list.Sessions)
	}
	// The list says what it is not showing, so a filtered list cannot read as
	// a complete one.
	if list.Hidden != 1 {
		t.Errorf("hidden = %d, want the one stray frame", list.Hidden)
	}
	if list.MinFrames != 2 {
		t.Errorf("the list reports a floor of %d, want 2", list.MinFrames)
	}
}

func TestSessionsShowEverythingAtAFloorOfOne(t *testing.T) {
	s := floorService(t, 1)
	floorTree(t, s)

	list, err := s.Sessions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 3 {
		t.Fatalf("%d sessions at a floor of 1, want all 3", len(list.Sessions))
	}
	if list.Hidden != 0 {
		t.Errorf("hidden = %d with nothing filtered", list.Hidden)
	}
}

// A configuration written before the floor existed, or hand-edited to zero,
// must not hide every shoot. Zero means the built-in default.
func TestSessionsFloorOfZeroTakesTheDefault(t *testing.T) {
	s := floorService(t, 0)
	floorTree(t, s)

	list, err := s.Sessions(0)
	if err != nil {
		t.Fatal(err)
	}
	// The default floor is five, so both shoots and the stray fall under it.
	if len(list.Sessions) != 0 || list.Hidden != 3 {
		t.Errorf("a zero floor gave %d shown / %d hidden, want the default of five to apply",
			len(list.Sessions), list.Hidden)
	}
	if list.MinFrames != config.Default().Behaviour.MinSessionFrames {
		t.Errorf("the list reports a floor of %d, want the default", list.MinFrames)
	}
}

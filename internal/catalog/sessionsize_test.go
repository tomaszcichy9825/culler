package catalog

import (
	"testing"
)

// A catalogue of real photographs is full of ones and twos — a test frame, a
// shot fired by accident, the one picture taken on a Tuesday — and at four
// hours' gap each of those is its own session. On a real library they
// outnumber the shoots badly enough to bury them, so the list can be told a
// floor.

// sessionTree writes one two-frame shoot, one single frame a day later, and a
// three-frame shoot the day after that.
func sessionTree(t *testing.T) *Store {
	t.Helper()
	s := openStore(t)
	root := t.TempDir()
	writeFrame(t, root, "SHOOT001", 100, 0, day(2026, 5, 1, 9, 0))
	writeFrame(t, root, "SHOOT002", 100, 0, day(2026, 5, 1, 9, 30))
	writeFrame(t, root, "STRAY001", 100, 0, day(2026, 5, 2, 9, 0))
	writeFrame(t, root, "BIG00001", 100, 0, day(2026, 5, 3, 9, 0))
	writeFrame(t, root, "BIG00002", 100, 0, day(2026, 5, 3, 9, 30))
	writeFrame(t, root, "BIG00003", 100, 0, day(2026, 5, 3, 10, 0))
	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return s
}

func TestSessionsKeepEveryShootByDefault(t *testing.T) {
	s := sessionTree(t)

	sessions, err := s.Sessions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("%d sessions with no floor, want 3: %+v", len(sessions), sessions)
	}
}

func TestSessionsDropShootsUnderTheFloor(t *testing.T) {
	s := sessionTree(t)

	sessions, err := s.SessionsWith(SessionOptions{MinFrames: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("%d sessions at a floor of 2, want 2: %+v", len(sessions), sessions)
	}
	for _, sess := range sessions {
		if sess.Frames < 2 {
			t.Errorf("a session of %d frames survived a floor of 2", sess.Frames)
		}
	}

	// The floor does not change the clustering, only what is shown: the shoots
	// that survive are the same ones, with the same counts.
	if sessions[0].Frames != 3 || sessions[1].Frames != 2 {
		t.Errorf("surviving sessions %+v", sessions)
	}
}

// A floor above everything leaves nothing rather than falling back to the
// whole list, which would be the opposite of what was asked.
func TestSessionsFloorAboveEverything(t *testing.T) {
	s := sessionTree(t)

	sessions, err := s.SessionsWith(SessionOptions{MinFrames: 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("%d sessions survived a floor of 99", len(sessions))
	}
}

// Zero and one both mean "every shoot", because a shoot cannot hold no frames.
func TestSessionsFloorOfZeroOrOneKeepsEverything(t *testing.T) {
	s := sessionTree(t)

	for _, floor := range []int{0, 1, -3} {
		sessions, err := s.SessionsWith(SessionOptions{MinFrames: floor})
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 3 {
			t.Errorf("floor %d left %d sessions, want all 3", floor, len(sessions))
		}
	}
}

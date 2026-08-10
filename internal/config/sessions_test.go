package config

import "testing"

// The Sessions list is only useful if the shoots in it are shoots. A real
// library is full of one- and two-frame fragments, so the list has a floor,
// and five is where a run of frames starts looking like a session rather than
// a stray press of the shutter.
func TestMinSessionFramesDefault(t *testing.T) {
	c := Default()
	if c.Behaviour.MinSessionFrames != 5 {
		t.Errorf("default min session frames = %d, want 5", c.Behaviour.MinSessionFrames)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("the default configuration must validate: %v", err)
	}
}

// One is the floor that shows everything. Zero and below would too, but they
// are refused so a saved configuration says what it means.
func TestMinSessionFramesMustBeAtLeastOne(t *testing.T) {
	c := Default()
	c.Behaviour.MinSessionFrames = 0
	if err := c.Validate(); err == nil {
		t.Error("a floor of zero was accepted")
	}
	c.Behaviour.MinSessionFrames = 1
	if err := c.Validate(); err != nil {
		t.Errorf("a floor of one must be legal, it means every shoot: %v", err)
	}
}

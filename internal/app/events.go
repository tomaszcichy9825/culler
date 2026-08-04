package app

import "github.com/wailsapp/wails/v3/pkg/application"

// The events a folder open produces. A streamed open emits frames, then their
// identities, then progress, and finally exactly one done — all four carrying
// the token of the open they belong to.
const (
	EventScanProgress = "scan:progress"
	EventScanFrames   = "scan:frames"
	EventScanHashed   = "scan:hashed"
	EventScanDone     = "scan:done"
)

// ScanProgress reports how far a folder open has got. Done counts identity
// hashes completed. Total is the number of frames found so far, which for a
// streamed open still rises while the walk is running and only settles when it
// ends. Token is empty for the unstreamed open.
type ScanProgress struct {
	Token string `json:"token"`
	Dir   string `json:"dir"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// ScanFrames is a batch of frames the walk has just found. They carry no hash
// and no decision: those are not known yet and arrive on ScanHashed. This is
// what the grid paints from, so it must never wait on anything slower than the
// walk itself.
type ScanFrames struct {
	Token  string     `json:"token"`
	Seq    int64      `json:"seq"`
	Dir    string     `json:"dir"`
	Frames []GroupDTO `json:"frames"`
}

// FrameHash is one frame's identity and everything the store remembers under
// it. Frames are addressed by dir and stem rather than by position, so a batch
// can be applied whatever order the grid is holding its frames in. Warnings is
// the frame's complete list, replacing the one it arrived with.
type FrameHash struct {
	Dir         string   `json:"dir"`
	Stem        string   `json:"stem"`
	Hash        string   `json:"hash"` // empty when the primary file could not be read
	Verdict     string   `json:"verdict"`
	Mask        string   `json:"mask"`
	Rating      int      `json:"rating"`
	Destination string   `json:"destination"`
	Decision    string   `json:"decision"`
	Warnings    []string `json:"warnings"`
}

// ScanHashed is a batch of identities for frames already handed over.
type ScanHashed struct {
	Token  string      `json:"token"`
	Seq    int64       `json:"seq"`
	Dir    string      `json:"dir"`
	Frames []FrameHash `json:"frames"`
}

// ScanDone ends a streamed open. Error is empty when the folder was read in
// full; a folder that failed mid-walk still keeps the frames it did produce.
type ScanDone struct {
	Token string `json:"token"`
	Seq   int64  `json:"seq"`
	Dir   string `json:"dir"`
	Total int    `json:"total"`
	Error string `json:"error"`
}

func init() {
	// Registration gives the binding generator a typed JS/TS API for the event.
	application.RegisterEvent[ScanProgress](EventScanProgress)
	application.RegisterEvent[ScanFrames](EventScanFrames)
	application.RegisterEvent[ScanHashed](EventScanHashed)
	application.RegisterEvent[ScanDone](EventScanDone)
}

// emitEvent publishes to the webview when the app is running. Tests exercise
// the services without a Wails application, so a missing instance is fine.
func emitEvent(name string, data any) {
	defer func() { _ = recover() }()
	if a := application.Get(); a != nil {
		a.Event.Emit(name, data)
	}
}

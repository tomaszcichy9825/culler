package app

import "github.com/wailsapp/wails/v3/pkg/application"

// EventScanProgress carries ScanProgress while a folder open is hashing.
const EventScanProgress = "scan:progress"

// ScanProgress reports how far a folder open has got. Total is the number of
// frames found by the scan; Done counts identity hashes completed.
type ScanProgress struct {
	Dir   string `json:"dir"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func init() {
	// Registration gives the binding generator a typed JS/TS API for the event.
	application.RegisterEvent[ScanProgress](EventScanProgress)
}

// emitEvent publishes to the webview when the app is running. Tests exercise
// the services without a Wails application, so a missing instance is fine.
func emitEvent(name string, data any) {
	defer func() { _ = recover() }()
	if a := application.Get(); a != nil {
		a.Event.Emit(name, data)
	}
}

// Package config holds the user-editable settings: extension lists, keymap,
// collision policy, and thresholds. It is the only place defaults live, and it
// is deliberately a plain JSON file so a broken install can be fixed in a text
// editor.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomaszcichy9825/culler/internal/scan"
)

// CollisionPolicy decides what happens when a copy or move lands on an
// existing file. Never silently overwrite: the default renames.
type CollisionPolicy string

const (
	CollisionSkip         CollisionPolicy = "skip"
	CollisionRenameSuffix CollisionPolicy = "rename-suffix"
	CollisionOverwrite    CollisionPolicy = "overwrite"
)

// TrashMode selects where rejected files go. Neither mode deletes anything;
// emptying rejects is a separate explicit command.
type TrashMode string

const (
	TrashSystem         TrashMode = "system"
	TrashRejectedFolder TrashMode = "rejected-folder"
)

// KeepMask says which halves of a RAW+JPEG pair a keep verdict holds on to.
type KeepMask string

const (
	KeepMaskBoth KeepMask = "rj"
	KeepMaskRAW  KeepMask = "r"
	KeepMaskJPEG KeepMask = "j"
)

// CutScope says how much of a frame a cut verdict removes: the whole frame, or
// only the halves the mask leaves out.
type CutScope string

const (
	CutRemovesBoth   CutScope = "both"
	CutRemovesMasked CutScope = "masked"
)

// Behaviour groups the settings that change what an apply does.
type Behaviour struct {
	CollisionPolicy      CollisionPolicy `json:"collisionPolicy"`
	BulkConfirmThreshold int             `json:"bulkConfirmThreshold"`
	TrashMode            TrashMode       `json:"trashMode"`
	RejectedFolderName   string          `json:"rejectedFolderName"`

	// Culling semantics: the mask a fresh keep starts with, and how far a cut
	// reaches.
	DefaultKeepMask KeepMask `json:"defaultKeepMask"`
	CutRemoves      CutScope `json:"cutRemoves"`

	// Import semantics: where a destination that is not an absolute path hangs
	// off, whether routed frames are copied or taken off the card, and whether
	// every copy is read back and compared before it counts as done.
	//
	// MoveOnImport is off because a card is the only copy of a photograph until
	// the import has finished, and VerifyCopies is on because the cost of a
	// second read is minutes and the cost of a silently broken RAW is the
	// photograph.
	LibraryRoot  string `json:"libraryRoot"`
	MoveOnImport bool   `json:"moveOnImport"`
	VerifyCopies bool   `json:"verifyCopies"`

	// XMPExport, when on, writes verdicts and ratings to XMP sidecars automatically
	// the frames, for Lightroom and Bridge. Off, because the sidecar is an
	// export and not the source of truth, and because turning it on puts a
	// file next to every decided photograph — see docs/DESIGN.md §3.3. It
	// gates an action the user still has to invoke; nothing is written to a
	// sidecar merely because this is true.
	XMPExport bool `json:"xmpExport"`

	// MinSessionFrames is the smallest shoot the Sessions list shows. A real
	// library is full of one- and two-frame fragments — a test frame, a shot
	// fired by accident, the one picture taken on a Tuesday — and at four
	// hours' gap each of those is a session of its own, which buries the
	// shoots among them. Five is where a run of frames starts looking like a
	// session; one shows every last fragment.
	//
	// It filters the list and nothing else. No frame is hidden from the grid,
	// the search or the tree by it, and changing it costs a query.
	MinSessionFrames int `json:"minSessionFrames"`

	// Concurrency limits for slow sources. Local disks tolerate parallel
	// reads; network shares stall under them, so those caps stay low.
	LocalReadSlots      int `json:"localReadSlots"`      // concurrent preview reads, local volumes
	NetworkReadSlots    int `json:"networkReadSlots"`    // concurrent preview reads, network volumes
	NetworkHashWorkers  int `json:"networkHashWorkers"`  // identity-hash workers on network volumes
	SlowScanHintSeconds int `json:"slowScanHintSeconds"` // when the "still scanning" line appears
}

// Config is the whole settings file.
type Config struct {
	Behaviour   Behaviour           `json:"behaviour"`
	Keymap      map[string][]string `json:"keymap"` // action -> key chords
	RawExts     []string            `json:"rawExts"`
	JpegExts    []string            `json:"jpegExts"`
	SidecarExts []string            `json:"sidecarExts"`
}

// Default returns the built-in configuration. Extension lists come from scan
// so there is one source of truth for what the app recognises.
func Default() Config {
	sc := scan.DefaultConfig()
	return Config{
		Behaviour: Behaviour{
			CollisionPolicy:      CollisionRenameSuffix,
			BulkConfirmThreshold: 20,
			TrashMode:            TrashSystem,
			RejectedFolderName:   "_Rejected",
			DefaultKeepMask:      KeepMaskBoth,
			CutRemoves:           CutRemovesBoth,
			LibraryRoot:          "~/Pictures",
			MoveOnImport:         false,
			VerifyCopies:         true,
			XMPExport:            false,
			MinSessionFrames:     5,
			LocalReadSlots:       16,
			NetworkReadSlots:     4,
			NetworkHashWorkers:   4,
			SlowScanHintSeconds:  10,
		},
		Keymap:      DefaultKeymap(),
		RawExts:     sc.RawExts,
		JpegExts:    sc.JpegExts,
		SidecarExts: sc.SidecarExts,
	}
}

// DefaultKeymap returns the stock bindings. "mod" stands for Cmd on macOS and
// Ctrl elsewhere; resolving it is the frontend's job, not this package's.
//
// A verdict is a letter and a rating is a digit: k keeps, x cuts, r and j
// toggle which half of a pair a keep holds on to, and 1-5 rate. That claims
// j, k and r, so the focus actions are on the arrow keys alone.
func DefaultKeymap() map[string][]string {
	return map[string][]string{
		"focus-left":       {"ArrowLeft"},
		"focus-right":      {"ArrowRight"},
		"focus-up":         {"ArrowUp"},
		"focus-down":       {"ArrowDown"},
		"cycle-layout":     {"Tab"},
		"toggle-loupe":     {"space"},
		"toggle-select":    {"s"},
		"select-all":       {"mod+a"},
		"escape":           {"Escape"},
		"verdict-keep":     {"k"},
		"verdict-cut":      {"x"},
		"mask-toggle-raw":  {"r"},
		"mask-toggle-jpeg": {"j"},
		"rate-1":           {"1"},
		"rate-2":           {"2"},
		"rate-3":           {"3"},
		"rate-4":           {"4"},
		"rate-5":           {"5"},
		"rate-clear":       {"0"},
		"copy-palette":     {"c"},
		"move-palette":     {"m"},
		"filter-palette":   {"f"},
		"zoom":             {"z"},
		"apply":            {"Enter"},
		"undo":             {"mod+z"},
		"redo":             {"shift+mod+z"},
		"command-palette":  {"mod+k"},
		"search":           {"/"},
		"keymap-overlay":   {"?"},
		"open-settings":    {"mod+,"},
		"enter-compare":    {"shift+c"},
		"write-metadata":   {"mod+s"},
	}
}

// DefaultPath is where the settings file lives: the OS config dir plus
// culler/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, "culler", "config.json"), nil
}

// Load reads path and merges it onto the defaults, so a file that sets one
// field keeps stock values for the rest and new settings appear without the
// user editing anything. A missing file is not an error; a corrupt one is,
// because returning defaults would silently discard the user's settings on the
// next Save. Load does not validate — call Validate on the result.
func Load(path string) (Config, error) {
	c := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return c, nil
}

// Save writes c to path, creating parent directories. The write is atomic:
// a temp file in the same directory is renamed over the target, so an
// interrupted save can never leave a half-written config behind.
func Save(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}

// Validate reports the first problem that would make the config unsafe or
// ambiguous to act on.
func (c Config) Validate() error {
	switch c.Behaviour.CollisionPolicy {
	case CollisionSkip, CollisionRenameSuffix, CollisionOverwrite:
	default:
		return fmt.Errorf("unknown collision policy %q: want %q, %q or %q",
			c.Behaviour.CollisionPolicy, CollisionSkip, CollisionRenameSuffix, CollisionOverwrite)
	}

	switch c.Behaviour.TrashMode {
	case TrashSystem:
	case TrashRejectedFolder:
		if c.Behaviour.RejectedFolderName == "" {
			return fmt.Errorf("rejected folder name must be set when trash mode is %q", TrashRejectedFolder)
		}
	default:
		return fmt.Errorf("unknown trash mode %q: want %q or %q",
			c.Behaviour.TrashMode, TrashSystem, TrashRejectedFolder)
	}

	switch c.Behaviour.DefaultKeepMask {
	case KeepMaskBoth, KeepMaskRAW, KeepMaskJPEG:
	default:
		return fmt.Errorf("unknown default keep mask %q: want %q, %q or %q",
			c.Behaviour.DefaultKeepMask, KeepMaskBoth, KeepMaskRAW, KeepMaskJPEG)
	}

	switch c.Behaviour.CutRemoves {
	case CutRemovesBoth, CutRemovesMasked:
	default:
		return fmt.Errorf("unknown cut removes scope %q: want %q or %q",
			c.Behaviour.CutRemoves, CutRemovesBoth, CutRemovesMasked)
	}

	// A destination the user typed as a bare name has to hang off something,
	// and silently choosing the working directory would scatter imports
	// wherever the app happened to be launched from.
	if strings.TrimSpace(c.Behaviour.LibraryRoot) == "" {
		return fmt.Errorf("library root must be set: it is what a destination that is not an absolute path is relative to")
	}

	if c.Behaviour.BulkConfirmThreshold < 0 {
		return fmt.Errorf("bulk confirm threshold must not be negative, got %d", c.Behaviour.BulkConfirmThreshold)
	}

	for name, v := range map[string]int{
		"localReadSlots":      c.Behaviour.LocalReadSlots,
		"networkReadSlots":    c.Behaviour.NetworkReadSlots,
		"networkHashWorkers":  c.Behaviour.NetworkHashWorkers,
		"slowScanHintSeconds": c.Behaviour.SlowScanHintSeconds,
		"minSessionFrames":    c.Behaviour.MinSessionFrames,
	} {
		if v < 1 {
			return fmt.Errorf("%s must be at least 1, got %d", name, v)
		}
	}

	return c.validateKeymap()
}

// validateKeymap rejects a chord bound to two actions, which would make the
// winner depend on map iteration order. Chords are compared literally, so
// "mod+z" and "z" are different bindings.
func (c Config) validateKeymap() error {
	actions := make([]string, 0, len(c.Keymap))
	for action := range c.Keymap {
		actions = append(actions, action)
	}
	sort.Strings(actions) // deterministic error message

	owner := make(map[string]string)
	for _, action := range actions {
		for _, chord := range c.Keymap[action] {
			if prev, taken := owner[chord]; taken && prev != action {
				return fmt.Errorf("key %q is bound to both %q and %q", chord, prev, action)
			}
			owner[chord] = action
		}
	}
	return nil
}

// ScanConfig is the bridge into the scanner: it only needs the extension
// classes.
func (c Config) ScanConfig() scan.Config {
	return scan.Config{
		RawExts:     c.RawExts,
		JpegExts:    c.JpegExts,
		SidecarExts: c.SidecarExts,
	}
}

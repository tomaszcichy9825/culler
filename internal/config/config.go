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

// Behaviour groups the settings that change what an apply does.
type Behaviour struct {
	CollisionPolicy      CollisionPolicy `json:"collisionPolicy"`
	BulkConfirmThreshold int             `json:"bulkConfirmThreshold"`
	TrashMode            TrashMode       `json:"trashMode"`
	RejectedFolderName   string          `json:"rejectedFolderName"`
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
		},
		Keymap:      DefaultKeymap(),
		RawExts:     sc.RawExts,
		JpegExts:    sc.JpegExts,
		SidecarExts: sc.SidecarExts,
	}
}

// DefaultKeymap returns the stock bindings. "mod" stands for Cmd on macOS and
// Ctrl elsewhere; resolving it is the frontend's job, not this package's.
func DefaultKeymap() map[string][]string {
	return map[string][]string{
		"focus-left":      {"ArrowLeft", "h"},
		"focus-right":     {"ArrowRight", "l"},
		"focus-up":        {"ArrowUp", "k"},
		"focus-down":      {"ArrowDown", "j"},
		"toggle-loupe":    {"Tab"},
		"toggle-select":   {"space"},
		"select-all":      {"mod+a"},
		"escape":          {"Escape"},
		"keep-all":        {"1"},
		"drop-raw":        {"2"},
		"drop-jpeg":       {"3"},
		"drop-both":       {"4"},
		"clear-decision":  {"0"},
		"copy-palette":    {"c"},
		"move-palette":    {"m"},
		"filter-palette":  {"f"},
		"zoom":            {"z"},
		"apply":           {"Enter"},
		"undo":            {"mod+z"},
		"redo":            {"shift+mod+z"},
		"command-palette": {"mod+k"},
		"keymap-overlay":  {"?"},
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

	if c.Behaviour.BulkConfirmThreshold < 0 {
		return fmt.Errorf("bulk confirm threshold must not be negative, got %d", c.Behaviour.BulkConfirmThreshold)
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

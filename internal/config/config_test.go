package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/scan"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefault(t *testing.T) {
	c := Default()

	if c.Behaviour.CollisionPolicy != CollisionRenameSuffix {
		t.Errorf("collision policy: want %q, got %q", CollisionRenameSuffix, c.Behaviour.CollisionPolicy)
	}
	if c.Behaviour.BulkConfirmThreshold != 20 {
		t.Errorf("bulk confirm threshold: want 20, got %d", c.Behaviour.BulkConfirmThreshold)
	}
	if c.Behaviour.TrashMode != TrashSystem {
		t.Errorf("trash mode: want %q, got %q", TrashSystem, c.Behaviour.TrashMode)
	}
	if c.Behaviour.RejectedFolderName != "_Rejected" {
		t.Errorf("rejected folder: want %q, got %q", "_Rejected", c.Behaviour.RejectedFolderName)
	}

	def := scan.DefaultConfig()
	if !slices.Equal(c.RawExts, def.RawExts) {
		t.Errorf("raw exts must match scan defaults, got %v", c.RawExts)
	}
	if !slices.Equal(c.JpegExts, def.JpegExts) {
		t.Errorf("jpeg exts must match scan defaults, got %v", c.JpegExts)
	}
	if !slices.Equal(c.SidecarExts, def.SidecarExts) {
		t.Errorf("sidecar exts must match scan defaults, got %v", c.SidecarExts)
	}

	if err := c.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestDefaultKeymapCoversEveryAction(t *testing.T) {
	km := Default().Keymap

	want := map[string]string{
		"focus-left":      "ArrowLeft",
		"focus-right":     "ArrowRight",
		"focus-up":        "ArrowUp",
		"focus-down":      "ArrowDown",
		"toggle-loupe":    "Tab",
		"toggle-select":   "space",
		"select-all":      "mod+a",
		"escape":          "Escape",
		"keep-all":        "1",
		"drop-raw":        "2",
		"drop-jpeg":       "3",
		"drop-both":       "4",
		"clear-decision":  "0",
		"copy-palette":    "c",
		"move-palette":    "m",
		"filter-palette":  "f",
		"zoom":            "z",
		"apply":           "Enter",
		"undo":            "mod+z",
		"redo":            "shift+mod+z",
		"command-palette": "mod+k",
		"keymap-overlay":  "?",
	}

	for action, chord := range want {
		chords, ok := km[action]
		if !ok {
			t.Errorf("action %q missing from default keymap", action)
			continue
		}
		if !slices.Contains(chords, chord) {
			t.Errorf("action %q: want chord %q in %v", action, chord, chords)
		}
	}
	if len(km) != len(want) {
		t.Errorf("keymap has %d actions, want %d: %v", len(km), len(want), km)
	}

	// The vim keys are alternates on the same actions as the arrows.
	for action, chord := range map[string]string{
		"focus-left": "h", "focus-down": "j", "focus-up": "k", "focus-right": "l",
	} {
		if !slices.Contains(km[action], chord) {
			t.Errorf("action %q: want vim alternate %q in %v", action, chord, km[action])
		}
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "config.json")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("missing file must not be an error: %v", err)
	}
	if c.Behaviour.BulkConfirmThreshold != Default().Behaviour.BulkConfirmThreshold {
		t.Errorf("want defaults, got %+v", c.Behaviour)
	}
	if len(c.Keymap) != len(Default().Keymap) {
		t.Errorf("want default keymap, got %v", c.Keymap)
	}
}

func TestLoadMergesOntoDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"behaviour":{"bulkConfirmThreshold":5}}`)

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if c.Behaviour.BulkConfirmThreshold != 5 {
		t.Errorf("threshold: want 5, got %d", c.Behaviour.BulkConfirmThreshold)
	}
	if c.Behaviour.CollisionPolicy != CollisionRenameSuffix {
		t.Errorf("unset field must keep its default, got %q", c.Behaviour.CollisionPolicy)
	}
	if c.Behaviour.TrashMode != TrashSystem {
		t.Errorf("unset field must keep its default, got %q", c.Behaviour.TrashMode)
	}
	if c.Behaviour.RejectedFolderName != "_Rejected" {
		t.Errorf("unset field must keep its default, got %q", c.Behaviour.RejectedFolderName)
	}
	if !slices.Equal(c.RawExts, scan.DefaultConfig().RawExts) {
		t.Errorf("unset ext list must keep its default, got %v", c.RawExts)
	}
	if len(c.Keymap) != len(Default().Keymap) {
		t.Errorf("unset keymap must keep its default, got %v", c.Keymap)
	}
}

func TestLoadMergesKeymapPerAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"keymap":{"zoom":["v"]}}`)

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := c.Keymap["zoom"]; !slices.Equal(got, []string{"v"}) {
		t.Errorf("rebound action: want [v], got %v", got)
	}
	if got := c.Keymap["apply"]; !slices.Contains(got, "Enter") {
		t.Errorf("untouched action must keep its default, got %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	c := Default()
	c.Behaviour.CollisionPolicy = CollisionSkip
	c.Behaviour.BulkConfirmThreshold = 7
	c.Behaviour.TrashMode = TrashRejectedFolder
	c.Behaviour.RejectedFolderName = "_Bin"
	c.RawExts = append(slices.Clone(c.RawExts), ".gpr")
	c.Keymap["zoom"] = []string{"v"}

	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Behaviour != c.Behaviour {
		t.Errorf("behaviour did not round-trip:\n got %+v\nwant %+v", got.Behaviour, c.Behaviour)
	}
	if !slices.Equal(got.RawExts, c.RawExts) {
		t.Errorf("raw exts did not round-trip: got %v, want %v", got.RawExts, c.RawExts)
	}
	if !slices.Equal(got.JpegExts, c.JpegExts) {
		t.Errorf("jpeg exts did not round-trip: got %v", got.JpegExts)
	}
	if !slices.Equal(got.SidecarExts, c.SidecarExts) {
		t.Errorf("sidecar exts did not round-trip: got %v", got.SidecarExts)
	}
	for action, chords := range c.Keymap {
		if !slices.Equal(got.Keymap[action], chords) {
			t.Errorf("action %q did not round-trip: got %v, want %v", action, got.Keymap[action], chords)
		}
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.json")

	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("atomic write left debris: %v", names)
	}
}

func TestSaveOverwritesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	c := Default()
	c.Behaviour.BulkConfirmThreshold = 3
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c.Behaviour.BulkConfirmThreshold = 9
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Behaviour.BulkConfirmThreshold != 9 {
		t.Errorf("want 9, got %d", got.Behaviour.BulkConfirmThreshold)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"behaviour": {`)

	// A corrupt file must never be papered over with defaults: the user would
	// silently lose their settings the next time we Save.
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt JSON must return an error")
	}
}

func TestLoadWrongTypeIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"behaviour":{"bulkConfirmThreshold":"twenty"}}`)

	if _, err := Load(path); err == nil {
		t.Fatal("type mismatch must return an error")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "defaults",
			mutate: func(*Config) {},
		},
		{
			name:    "unknown collision policy",
			mutate:  func(c *Config) { c.Behaviour.CollisionPolicy = "clobber" },
			wantErr: "collision policy",
		},
		{
			name:    "empty collision policy",
			mutate:  func(c *Config) { c.Behaviour.CollisionPolicy = "" },
			wantErr: "collision policy",
		},
		{
			name:    "unknown trash mode",
			mutate:  func(c *Config) { c.Behaviour.TrashMode = "shred" },
			wantErr: "trash mode",
		},
		{
			name: "empty rejected folder name in rejected-folder mode",
			mutate: func(c *Config) {
				c.Behaviour.TrashMode = TrashRejectedFolder
				c.Behaviour.RejectedFolderName = ""
			},
			wantErr: "rejected folder",
		},
		{
			name: "empty rejected folder name is fine in system trash mode",
			mutate: func(c *Config) {
				c.Behaviour.TrashMode = TrashSystem
				c.Behaviour.RejectedFolderName = ""
			},
		},
		{
			name:    "chord bound to two actions",
			mutate:  func(c *Config) { c.Keymap["zoom"] = []string{"Enter"} },
			wantErr: "Enter",
		},
		{
			name: "mod+z and z are distinct chords",
			mutate: func(c *Config) {
				c.Keymap["zoom"] = []string{"z"}
				c.Keymap["undo"] = []string{"mod+z"}
				c.Keymap["redo"] = []string{"shift+mod+z"}
			},
		},
		{
			name:    "negative bulk confirm threshold",
			mutate:  func(c *Config) { c.Behaviour.BulkConfirmThreshold = -1 },
			wantErr: "threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error mentioning %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNamesBothConflictingActions(t *testing.T) {
	c := Default()
	c.Keymap["zoom"] = []string{"Enter"}

	err := c.Validate()
	if err == nil {
		t.Fatal("want duplicate binding error")
	}
	for _, action := range []string{"zoom", "apply"} {
		if !strings.Contains(err.Error(), action) {
			t.Errorf("error %q should name the conflicting action %q", err, action)
		}
	}
}

func TestScanConfig(t *testing.T) {
	c := Default()
	c.RawExts = []string{".raf"}
	c.JpegExts = []string{".jpg"}
	c.SidecarExts = []string{".xmp"}

	sc := c.ScanConfig()

	if !slices.Equal(sc.RawExts, c.RawExts) {
		t.Errorf("raw exts: got %v, want %v", sc.RawExts, c.RawExts)
	}
	if !slices.Equal(sc.JpegExts, c.JpegExts) {
		t.Errorf("jpeg exts: got %v, want %v", sc.JpegExts, c.JpegExts)
	}
	if !slices.Equal(sc.SidecarExts, c.SidecarExts) {
		t.Errorf("sidecar exts: got %v, want %v", sc.SidecarExts, c.SidecarExts)
	}
}

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("culler", "config.json"); !strings.HasSuffix(p, want) {
		t.Errorf("path %q should end with %q", p, want)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("path %q should be absolute", p)
	}
}

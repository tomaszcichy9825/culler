// Package app wires the culling engine to the Wails frontend. It owns the
// state the services share — the loaded configuration, the decision store and
// the journal — and converts between the internal types and the JSON-friendly
// shapes the frontend binds against.
//
// Nothing here writes to the folder being culled. The store and the journal
// live in the app data directory; the only exception in the whole package is
// the _Rejected folder, which the user asks for explicitly.
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/platform"
)

// Names of the two files the app keeps in its data directory.
const (
	decisionsFile = "decisions.db"
	journalFile   = "journal.jsonl"
)

// App is the state every service shares.
//
// The decision store and the journal open on first use rather than at
// startup, so launching the app costs nothing and a folder that is only
// browsed never creates either file.
type App struct {
	mu         sync.Mutex
	cfg        config.Config
	configPath string
	dataDir    string
	store      *decide.Store
	jrnl       *journal.Journal
}

// New loads the configuration from its default location and prepares the
// shared state. A configuration file that cannot be read or that fails
// validation is an error: carrying on with defaults would silently discard
// the user's settings on the next save.
func New() (*App, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate app data dir: %w", err)
	}
	return newAt(path, filepath.Join(dir, "culler"), cfg), nil
}

// newAt builds an App rooted at an explicit config path and data directory,
// which is how the tests run without touching the real one.
func newAt(configPath, dataDir string, cfg config.Config) *App {
	return &App{cfg: cfg, configPath: configPath, dataDir: dataDir}
}

// Config returns the current configuration.
func (a *App) Config() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// setConfig validates c, writes it to disk and adopts it. The write happens
// first: if it fails, the running app keeps the settings it already had.
func (a *App) setConfig(c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := config.Save(a.configPath, c); err != nil {
		return err
	}
	a.cfg = c
	return nil
}

// decisions returns the decision store, opening it on first use.
func (a *App) decisions() (*decide.Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		return a.store, nil
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create app data dir %s: %w", a.dataDir, err)
	}
	store, err := decide.Open(filepath.Join(a.dataDir, decisionsFile))
	if err != nil {
		return nil, err
	}
	a.store = store
	return store, nil
}

// openJournal returns the journal, opening it on first use.
func (a *App) openJournal() (*journal.Journal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jrnl != nil {
		return a.jrnl, nil
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create app data dir %s: %w", a.dataDir, err)
	}
	j, err := journal.Open(filepath.Join(a.dataDir, journalFile))
	if err != nil {
		return nil, err
	}
	a.jrnl = j
	return j, nil
}

// trasher picks where the rejects from a batch over dir go. Rejected-folder
// mode puts them in a subfolder of the culled directory, the one thing the
// app is ever allowed to write there; otherwise they go to the OS trash, so
// recovery is possible from the file manager without the app.
func (a *App) trasher(dir string) (platform.Trasher, error) {
	cfg := a.Config()
	if cfg.Behaviour.TrashMode == config.TrashRejectedFolder {
		name := cfg.Behaviour.RejectedFolderName
		if name == "" {
			name = config.Default().Behaviour.RejectedFolderName
		}
		return platform.DirTrasher{Dir: filepath.Join(dir, name)}, nil
	}
	return platform.SystemTrasher()
}

// scopeTrasher is the trasher for an apply that may span several folders. In
// rejected-folder mode a single fixed bin will not do — each folder keeps its
// own _Rejected — so it routes by the file's parent. The machine trash already
// serves every folder, so that mode is unchanged.
func (a *App) scopeTrasher() (platform.Trasher, error) {
	cfg := a.Config()
	if cfg.Behaviour.TrashMode == config.TrashRejectedFolder {
		name := cfg.Behaviour.RejectedFolderName
		if name == "" {
			name = config.Default().Behaviour.RejectedFolderName
		}
		return platform.PerFolderTrasher{Name: name}, nil
	}
	return platform.SystemTrasher()
}

// Close releases the store and the journal. It is safe to call on an App
// whose lazy state was never opened.
func (a *App) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var errs []error
	if a.store != nil {
		errs = append(errs, a.store.Close())
		a.store = nil
	}
	if a.jrnl != nil {
		errs = append(errs, a.jrnl.Close())
		a.jrnl = nil
	}
	return errors.Join(errs...)
}

// expandPath turns what the user typed into an absolute path: a leading ~ is
// their home directory, everything else is resolved against the working
// directory.
func expandPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("no folder given")
	}
	if p == "~" || len(p) >= 2 && p[0] == '~' && (p[1] == '/' || p[1] == filepath.Separator) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		p = filepath.Join(home, p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	return abs, nil
}

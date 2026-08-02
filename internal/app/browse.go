package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// DirEntryDTO is one subdirectory in the sidebar tree.
type DirEntryDTO struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	HasDirs bool   `json:"hasDirs"` // whether it has subdirectories, for the expand chevron
}

// PickFolder opens the native directory chooser and returns the selected
// absolute path, or "" when the user cancels.
func (s *LibraryService) PickFolder() (string, error) {
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Choose a folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true)
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		// every platform reports cancel differently; treat it as no choice
		return "", nil
	}
	return path, nil
}

// ListDirs returns dir's visible subdirectories, sorted by name, for lazy
// expansion of the sidebar tree.
func (s *LibraryService) ListDirs(dir string) ([]DirEntryDTO, error) {
	resolved, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", resolved, err)
	}
	out := make([]DirEntryDTO, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(resolved, e.Name())
		out = append(out, DirEntryDTO{Name: e.Name(), Path: p, HasDirs: hasVisibleSubdir(p)})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// hasVisibleSubdir reports whether dir contains at least one non-hidden
// directory. Best-effort: unreadable directories simply show no chevron.
func hasVisibleSubdir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
)

type DirKind int

const (
	DirEmpty DirKind = iota
	DirBackup
	DirForeign
)

var ignoredDirEntries = map[string]bool{
	".DS_Store": true,
	".git":      true,
}

func InspectDir(dir string) (DirKind, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return DirEmpty, nil
	}
	if err != nil {
		return DirForeign, err
	}

	meaningful := 0
	for _, e := range entries {
		if !ignoredDirEntries[e.Name()] {
			meaningful++
		}
	}
	if meaningful == 0 {
		return DirEmpty, nil
	}

	backedUp, err := containsBackedUpShortcut(dir)
	if err != nil {
		return DirForeign, err
	}
	if backedUp {
		return DirBackup, nil
	}
	return DirForeign, nil
}

func containsBackedUpShortcut(dir string) (bool, error) {
	root := filepath.Join(dir, ShortcutsDir)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name(), MetadataFileName))
		if err != nil {
			continue
		}
		var meta archive.Metadata
		if json.Unmarshal(raw, &meta) != nil {
			continue
		}
		if meta.WorkflowID != "" && meta.Name != "" {
			return true, nil
		}
	}
	return false, nil
}

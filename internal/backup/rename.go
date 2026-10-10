package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

type Rename struct {
	WorkflowID string
	FromName   string
	ToName     string
	FromSlug   string
	ToSlug     string
}

func (r Rename) String() string {
	if r.FromName == r.ToName {
		return fmt.Sprintf("%s (%s → %s)", r.ToName, r.FromSlug, r.ToSlug)
	}
	return r.FromName + " → " + r.ToName
}

func (s *Service) Renames() []Rename {
	out := append([]Rename(nil), s.renames...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ToSlug != out[j].ToSlug {
			return out[i].ToSlug < out[j].ToSlug
		}
		return out[i].FromSlug < out[j].FromSlug
	})
	return out
}

func previousDirs(repo map[string]archive.Metadata, workflowID, slug string) []string {
	var out []string
	for other, meta := range repo {
		if other != slug && meta.WorkflowID == workflowID {
			out = append(out, other)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) adoptPreviousDirs(sc shortcuts.Shortcut, dir string) error {
	repo, err := s.repoSnapshot()
	if err != nil {
		return err
	}
	slug := filepath.Base(dir)
	previous := previousDirs(repo, sc.WorkflowID, slug)
	if len(previous) == 0 {
		return nil
	}

	_, statErr := os.Stat(dir)
	targetExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	root := filepath.Join(s.cfg.RepoPath, ShortcutsDir)
	for _, old := range previous {
		oldDir := filepath.Join(root, old)
		meta, ok, err := readMetadata(oldDir)
		if err != nil {
			return err
		}
		if !ok || meta.WorkflowID != sc.WorkflowID {
			continue
		}
		if targetExists {
			if err := os.RemoveAll(oldDir); err != nil {
				return err
			}
		} else {
			if err := os.Rename(oldDir, dir); err != nil {
				return err
			}
			targetExists = true
		}
		s.removed[oldDir] = true
		s.renames = append(s.renames, Rename{
			WorkflowID: sc.WorkflowID, FromName: meta.Name, ToName: sc.Name, FromSlug: old, ToSlug: slug,
		})
	}
	return nil
}

func carryOverSignedFile(dir, signedPath string, jsonChanged bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".shortcut") || path == signedPath {
			continue
		}
		stale = append(stale, path)
	}
	if len(stale) == 0 {
		return nil, nil
	}

	if _, err := os.Stat(signedPath); errors.Is(err, os.ErrNotExist) && !jsonChanged {
		if err := os.Rename(stale[0], signedPath); err != nil {
			return nil, err
		}
		return stale, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return stale, nil
}

func (s *Service) removeStaleDuplicates(live map[string]shortcuts.Shortcut) error {
	if err := s.ensureSlugs(); err != nil {
		return err
	}
	current, err := readRepoMetadata(s.cfg.RepoPath)
	if err != nil {
		return err
	}
	root := filepath.Join(s.cfg.RepoPath, ShortcutsDir)
	for id, sc := range live {
		slug, ok := s.slugs[id]
		if !ok || current[slug].WorkflowID != id {
			continue
		}
		for _, old := range previousDirs(current, id, slug) {
			oldDir := filepath.Join(root, old)
			if err := os.RemoveAll(oldDir); err != nil {
				return err
			}
			s.removed[oldDir] = true
			s.renames = append(s.renames, Rename{
				WorkflowID: id, FromName: current[old].Name, ToName: sc.Name, FromSlug: old, ToSlug: slug,
			})
		}
	}
	return nil
}

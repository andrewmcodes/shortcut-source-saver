package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

type StatusChange struct {
	Name       string
	WorkflowID string
	Slug       string
	From       shortcuts.Status
	To         shortcuts.Status
}

func (c StatusChange) Restored() bool {
	return c.From == shortcuts.StatusDeleted && c.To != shortcuts.StatusDeleted
}

func readRepoMetadata(repoPath string) (map[string]archive.Metadata, error) {
	root := filepath.Join(repoPath, ShortcutsDir)
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]archive.Metadata{}, nil
	}
	if err != nil {
		return nil, err
	}

	out := map[string]archive.Metadata{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		meta, ok, err := readMetadata(filepath.Join(root, d.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", d.Name(), err)
		}
		if ok {
			out[d.Name()] = meta
		}
	}
	return out, nil
}

func readMetadata(dir string) (archive.Metadata, bool, error) {
	var meta archive.Metadata
	raw, err := os.ReadFile(filepath.Join(dir, MetadataFileName))
	if errors.Is(err, os.ErrNotExist) {
		return meta, false, nil
	}
	if err != nil {
		return meta, false, err
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return meta, false, err
	}
	return meta, true, nil
}

func directoryOwners(repo map[string]archive.Metadata) map[string]string {
	owners := make(map[string]string, len(repo))
	for slug, meta := range repo {
		if meta.WorkflowID != "" {
			owners[slug] = meta.WorkflowID
		}
	}
	return owners
}

func ResolveSlugsAgainst(all []shortcuts.Shortcut, owners map[string]string) map[string]string {
	resolved := ResolveSlugs(all)
	if len(owners) == 0 {
		return resolved
	}
	for _, sc := range all {
		id := sc.WorkflowID
		base := archive.Slug(sc.Name)
		suffixed := base + "-" + shortID(id)
		switch {
		case owners[base] == id:
			resolved[id] = base
		case owners[suffixed] == id:
			resolved[id] = suffixed
		default:
			if owner, taken := owners[resolved[id]]; taken && owner != id {
				resolved[id] = suffixed
				if owner, taken := owners[suffixed]; taken && owner != id {
					resolved[id] = base + "-" + fullID(id)
				}
			}
		}
	}
	return resolved
}

func fullID(workflowID string) string {
	cleaned := strings.ToLower(strings.ReplaceAll(workflowID, "-", ""))
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func (s *Service) previousSlug(slug string) string {
	if _, ok := s.repo[slug]; ok {
		return slug
	}
	for _, r := range s.renames {
		if r.ToSlug == slug {
			return r.FromSlug
		}
	}
	return slug
}

func (s *Service) ReconcileStatus() ([]StatusChange, error) {
	if s.lib == nil {
		return nil, nil
	}
	all, err := s.library()
	if err != nil {
		return nil, err
	}
	before, err := s.repoSnapshot()
	if err != nil {
		return nil, err
	}
	live := make(map[string]shortcuts.Shortcut, len(all))
	for _, sc := range all {
		live[sc.WorkflowID] = sc
	}
	if err := s.removeStaleDuplicates(live); err != nil {
		return nil, err
	}
	current, err := readRepoMetadata(s.cfg.RepoPath)
	if err != nil {
		return nil, err
	}

	var changes []StatusChange
	for slug, meta := range current {
		if meta.WorkflowID == "" {
			continue
		}
		status, origin := shortcuts.StatusDeleted, meta.Original()
		if sc, ok := live[meta.WorkflowID]; ok {
			status, origin = sc.Status, sc.ConflictOf
		}

		if meta.ShortcutStatus() != status || meta.Original() != origin {
			updated := meta
			updated.Status = string(status)
			updated.ConflictOf = nil
			if origin != (shortcuts.Original{}) {
				updated.ConflictOf = &archive.Origin{WorkflowID: origin.WorkflowID, Name: origin.Name}
			}
			path := filepath.Join(s.cfg.RepoPath, ShortcutsDir, slug, MetadataFileName)
			if err := archive.WriteMetadata(path, updated); err != nil {
				return nil, err
			}
			s.touched[path] = true
		}

		previous := meta.ShortcutStatus()
		if old, ok := before[s.previousSlug(slug)]; ok {
			previous = old.ShortcutStatus()
		}
		if previous != status {
			changes = append(changes, StatusChange{
				Name: meta.Name, WorkflowID: meta.WorkflowID, Slug: slug, From: previous, To: status,
			})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Slug < changes[j].Slug })
	return changes, nil
}

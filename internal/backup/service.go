package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/gitrepo"
	"github.com/andrewmcodes/shortcut-source-saver/internal/render"
	"github.com/andrewmcodes/shortcut-source-saver/internal/scan"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/andrewmcodes/shortcut-source-saver/internal/tui"
)

const (
	ShortcutsDir     = "shortcuts"
	SourceFileName   = "shortcut.json"
	MetadataFileName = "metadata.json"
	ReadmeFileName   = "README.md"
)

var ErrSecretsFound = errors.New("possible credentials found in shortcut actions")

type Options struct {
	AllowSecrets  bool
	IncludeHidden bool
	ExpandAll     bool
}

type Service struct {
	cfg  *config.Config
	lib  *shortcuts.Library
	opts Options

	touched map[string]bool
	names   map[string]bool

	slugs    map[string]string
	slugsErr error
}

func New(cfg *config.Config, lib *shortcuts.Library, opts Options) *Service {
	return &Service{
		cfg:     cfg,
		lib:     lib,
		opts:    opts,
		touched: map[string]bool{},
		names:   map[string]bool{},
	}
}

func (s *Service) List() ([]shortcuts.Shortcut, error) {
	all, err := s.lib.List()
	if err != nil {
		return nil, err
	}
	if s.opts.IncludeHidden {
		return all, nil
	}
	visible := make([]shortcuts.Shortcut, 0, len(all))
	for _, sc := range all {
		if !sc.Hidden {
			visible = append(visible, sc)
		}
	}
	return visible, nil
}

const disambiguationLength = 8

func ResolveSlugs(all []shortcuts.Shortcut) map[string]string {
	groups := map[string][]shortcuts.Shortcut{}
	for _, sc := range all {
		base := archive.Slug(sc.Name)
		groups[base] = append(groups[base], sc)
	}

	resolved := make(map[string]string, len(all))
	for base, members := range groups {
		if len(members) == 1 {
			resolved[members[0].WorkflowID] = base
			continue
		}
		for _, sc := range members {
			resolved[sc.WorkflowID] = base + "-" + shortID(sc.WorkflowID)
		}
	}
	return resolved
}

func shortID(workflowID string) string {
	cleaned := strings.ToLower(strings.ReplaceAll(workflowID, "-", ""))
	if cleaned == "" {
		return "unknown"
	}
	if len(cleaned) > disambiguationLength {
		return cleaned[:disambiguationLength]
	}
	return cleaned
}

func (s *Service) slugFor(sc shortcuts.Shortcut) (string, error) {
	if s.slugs == nil && s.slugsErr == nil {
		all, err := s.lib.List()
		if err != nil {
			s.slugsErr = err
		} else {
			s.slugs = ResolveSlugs(all)
		}
	}
	if s.slugsErr != nil {
		return "", s.slugsErr
	}
	if slug, ok := s.slugs[sc.WorkflowID]; ok {
		return slug, nil
	}
	return archive.Slug(sc.Name), nil
}

func (s *Service) ShortcutDir(sc shortcuts.Shortcut) (string, error) {
	slug, err := s.slugFor(sc)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.cfg.RepoPath, ShortcutsDir, slug), nil
}

func (s *Service) Backup(ctx context.Context, sc shortcuts.Shortcut) tui.Result {
	result := tui.Result{Shortcut: sc}

	if err := ctx.Err(); err != nil {
		result.Outcome = tui.OutcomeSkipped
		result.Detail = "cancelled"
		return result
	}

	w, err := s.lib.Load(sc)
	if err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}

	if findings := FindSecrets(w); len(findings) > 0 && !s.opts.AllowSecrets {
		result.Outcome = tui.OutcomeFailed
		result.Detail = fmt.Sprintf("%d possible credential(s); re-run with --allow-secrets to override", len(findings))
		result.Err = fmt.Errorf("%w in %q:\n%s", ErrSecretsFound, sc.Name, scan.Summary(findings))
		return result
	}

	dir, err := s.ShortcutDir(sc)
	if err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}

	workflow := archive.BuildWorkflow(w)

	sourcePath := filepath.Join(dir, SourceFileName)
	changed, err := archive.WriteJSON(sourcePath, workflow)
	if err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}
	s.track(sourcePath, sc.Name)

	metadataPath := filepath.Join(dir, MetadataFileName)
	if err := archive.WriteMetadata(metadataPath, archive.NewMetadata(w)); err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}
	s.track(metadataPath, sc.Name)

	readme, err := render.ShortcutREADMEWithOptions(w, render.Options{
		ExpandAll:    s.opts.ExpandAll,
		SiteRoot:     relativeSlashPath(dir, s.cfg.SitePath()),
		Slug:         filepath.Base(dir),
		ShortcutFile: signedFileName(sc.Name),
	})
	if err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}
	readmePath := filepath.Join(dir, ReadmeFileName)
	if err := writeFileIfChanged(readmePath, readme); err != nil {
		result.Outcome = tui.OutcomeFailed
		result.Err = err
		return result
	}
	s.track(readmePath, sc.Name)

	if s.cfg.IncludeSigned {
		signedPath := filepath.Join(dir, signedFileName(sc.Name))
		if err := archive.Sign(changed, workflow, signedPath, s.cfg.SignMode); err != nil {
			result.Outcome = tui.OutcomeFailed
			result.Err = err
			return result
		}
		s.track(signedPath, sc.Name)
	}

	if changed {
		result.Outcome = tui.OutcomeWritten
		result.Detail = fmt.Sprintf("%d actions", len(w.Actions))
	} else {
		result.Outcome = tui.OutcomeUnchanged
		result.Detail = "no changes"
	}
	return result
}

func FindSecrets(w *shortcuts.Workflow) []scan.Finding {
	findings := scan.Scan(w.Actions)
	for _, raw := range w.ImportQuestions {
		index := -1
		if q, ok := raw.(map[string]any); ok {
			if n, ok := intValue(q["ActionIndex"]); ok {
				index = n
			}
		}
		findings = append(findings, scan.ScanValue(index, "import question", raw)...)
	}
	return findings
}

func (s *Service) track(path, name string) {
	s.touched[path] = true
	s.names[name] = true
}

func (s *Service) TouchedPaths() []string {
	out := make([]string, 0, len(s.touched))
	for p := range s.touched {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (s *Service) BackedUpNames() []string {
	out := make([]string, 0, len(s.names))
	for n := range s.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (s *Service) BuildSite() ([]string, error) {
	entries, err := s.repoEntries()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	files, err := render.Site(entries, render.SiteOptions{
		Title:     s.cfg.SiteTitle,
		ExpandAll: s.opts.ExpandAll,
	})
	if err != nil {
		return nil, err
	}

	siteRoot := s.cfg.SitePath()
	written := make([]string, 0, len(files))
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		target := filepath.Join(siteRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := writeFileIfChanged(target, files[rel]); err != nil {
			return nil, err
		}
		written = append(written, target)
	}
	return written, nil
}

func (s *Service) repoEntries() ([]render.Entry, error) {
	root := filepath.Join(s.cfg.RepoPath, ShortcutsDir)
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []render.Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		w, err := loadFromRepo(filepath.Join(root, d.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", d.Name(), err)
		}
		if w == nil {
			continue
		}
		entries = append(entries, render.Entry{Workflow: w, Slug: d.Name()})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Workflow.Name) < strings.ToLower(entries[j].Workflow.Name)
	})
	return entries, nil
}

func loadFromRepo(dir string) (*shortcuts.Workflow, error) {
	sourceRaw, err := os.ReadFile(filepath.Join(dir, SourceFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var source map[string]any
	if err := json.Unmarshal(sourceRaw, &source); err != nil {
		return nil, err
	}

	var meta archive.Metadata
	metaRaw, err := os.ReadFile(filepath.Join(dir, MetadataFileName))
	if err == nil {
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	w := &shortcuts.Workflow{}
	w.Name = meta.Name
	if w.Name == "" {
		w.Name = filepath.Base(dir)
	}
	w.WorkflowID = meta.WorkflowID
	w.Icon = shortcuts.Icon{Glyph: meta.Glyph, Color: parseHexColor(meta.Color)}
	w.Created = parseTime(meta.Created)
	w.Modified = parseTime(meta.Modified)
	w.ClientVersion = stringValue(source["WFWorkflowClientVersion"], meta.ClientVersion)
	w.MinimumClientVersion = stringValue(source["WFWorkflowMinimumClientVersionString"], meta.MinimumClientVersion)
	w.Actions = arrayValue(source["WFWorkflowActions"])
	w.InputContentItemClasses = arrayValue(source["WFWorkflowInputContentItemClasses"])
	w.OutputContentItemClasses = arrayValue(source["WFWorkflowOutputContentItemClasses"])
	w.ImportQuestions = arrayValue(source["WFWorkflowImportQuestions"])
	w.HasOutputFallback = boolValue(source["WFWorkflowHasOutputFallback"])
	w.HasShortcutInputVariables = boolValue(source["WFWorkflowHasShortcutInputVariables"])
	w.ActionCount = len(w.Actions)
	if w.ActionCount == 0 {
		w.ActionCount = meta.ActionCount
	}

	w.HiddenFromWidget = true
	for _, t := range arrayValue(source["WFWorkflowTypes"]) {
		if s, ok := t.(string); ok {
			switch s {
			case "NCWidget":
				w.HiddenFromWidget = false
			case "WFWorkflowTypeShowInSearch":
				w.ShowInSearch = true
			}
		}
	}
	return w, nil
}

func (s *Service) Stage(ctx context.Context, repo *gitrepo.Repo, extra []string) error {
	paths := append(s.TouchedPaths(), extra...)
	if len(paths) == 0 {
		return nil
	}
	return repo.Add(ctx, paths...)
}

func writeFileIfChanged(path string, contents []byte) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(contents) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sss-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func relativeSlashPath(from, to string) string {
	rel, err := filepath.Rel(from, to)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func signedFileName(name string) string {
	return sanitizeFileName(name) + ".shortcut"
}

func sanitizeFileName(name string) string {
	replaced := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	replaced = strings.TrimSpace(replaced)
	if replaced == "" {
		return "shortcut"
	}
	return replaced
}

func parseHexColor(s string) uint32 {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0
	}
	var v uint32
	for _, c := range s {
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return 0
		}
		v = v<<4 | d
	}
	return v<<8 | 0xFF
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func stringValue(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func arrayValue(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func boolValue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

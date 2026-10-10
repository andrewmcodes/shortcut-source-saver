package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/backup"
	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/gitrepo"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/andrewmcodes/shortcut-source-saver/internal/tui"
)

type session struct {
	cfg     *config.Config
	lib     *shortcuts.Library
	service *backup.Service
	flags   *globalFlags
}

func loadSessionConfig(flags *globalFlags) (*config.Config, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return nil, err
	}
	if flags.repoPath != "" {
		expanded, err := config.ExpandTilde(flags.repoPath)
		if err != nil {
			return nil, err
		}
		cfg.RepoPath = expanded
	}
	if flags.includeHidden {
		cfg.IncludeHidden = true
	}
	if flags.outputDir != "" {
		if err := useOutputDir(cfg, flags.outputDir); err != nil {
			return nil, err
		}
		if !flags.force {
			if err := guardOutputDir(cfg); err != nil {
				return nil, err
			}
		}
		return cfg, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w\n\nRun `sss init` to create a config file, or pass --output DIR to write without one", err)
	}
	return cfg, nil
}

func useOutputDir(cfg *config.Config, raw string) error {
	dir, err := resolveOutputDir(raw)
	if err != nil {
		return err
	}
	cfg.RepoPath = dir
	cfg.AutoCommit = false
	cfg.AutoPush = false
	if filepath.IsAbs(cfg.SiteDir) {
		cfg.SiteDir = config.Default().SiteDir
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	return nil
}

func guardOutputDir(cfg *config.Config) error {
	kind, err := backup.InspectDir(cfg.RepoPath)
	if err != nil {
		return err
	}
	if kind != backup.DirForeign {
		return nil
	}

	siteRel, err := filepath.Rel(cfg.RepoPath, cfg.SitePath())
	if err != nil {
		siteRel = cfg.SiteDir
	}
	targets := []string{backup.ShortcutsDir + "/", filepath.ToSlash(siteRel) + "/"}
	var existing []string
	for _, rel := range targets {
		if _, err := os.Stat(filepath.Join(cfg.RepoPath, rel)); err == nil {
			existing = append(existing, rel)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s is not empty and is not an sss backup, so nothing was written.\n", cfg.RepoPath)
	fmt.Fprintf(&b, "sss would write %s into it, replacing any file already at the same path", strings.Join(targets, " and "))
	switch len(existing) {
	case 0:
	case 1:
		fmt.Fprintf(&b, " (%s already exists here)", existing[0])
	default:
		fmt.Fprintf(&b, " (%s already exist here)", strings.Join(existing, " and "))
	}
	b.WriteString(".\nRe-run with --force to write there anyway, or choose an empty or new directory.")
	return errors.New(b.String())
}

func resolveOutputDir(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("--output needs a directory")
	}
	expanded, err := config.ExpandTilde(raw)
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return dir, nil
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", fmt.Errorf("--output %s is not a directory", dir)
	}
	return dir, nil
}

func (s *session) writesToOutputDir() bool {
	return s.flags.outputDir != ""
}

func openSession(flags *globalFlags) (*session, error) {
	cfg, err := loadSessionConfig(flags)
	if err != nil {
		return nil, err
	}

	lib, err := shortcuts.Open(flags.dbPath)
	if err != nil {
		return nil, err
	}

	service := backup.New(cfg, lib, backup.Options{
		AllowSecrets:  flags.allowSecrets,
		IncludeHidden: cfg.IncludeHidden,
		ExpandAll:     flags.expandAll,
	})

	return &session{cfg: cfg, lib: lib, service: service, flags: flags}, nil
}

func (s *session) Close() error {
	return s.lib.Close()
}

func (s *session) finish(ctx context.Context, results []tui.Result) error {
	if countBy(results, tui.OutcomeWritten) == 0 && countBy(results, tui.OutcomeUnchanged) == 0 {
		return reportFailures(results)
	}

	changes, err := s.service.ReconcileStatus()
	if err != nil {
		return fmt.Errorf("recording shortcut status: %w", err)
	}
	statusSummary := summarizeStatusChanges(changes)
	printStatusChanges(statusSummary)

	sitePaths, err := s.service.BuildSite()
	if err != nil {
		return fmt.Errorf("building the site: %w", err)
	}

	if s.writesToOutputDir() {
		fmt.Printf("\nBacked up to %s (%d written, %d unchanged)\n",
			s.cfg.RepoPath, countBy(results, tui.OutcomeWritten), countBy(results, tui.OutcomeUnchanged))
		return reportFailures(results)
	}

	if s.flags.noCommit || !s.cfg.AutoCommit {
		fmt.Printf("\nWrote %d file(s) under %s (not committed)\n",
			len(s.service.TouchedPaths())+len(sitePaths), s.cfg.RepoPath)
		return reportFailures(results)
	}

	repo, err := gitrepo.Open(ctx, s.cfg.RepoPath)
	if err != nil {
		return err
	}
	if err := s.service.Stage(ctx, repo, sitePaths); err != nil {
		return err
	}

	names := s.service.BackedUpNames()
	message := gitrepo.BackupMessage(names, statusSummary)
	committed, err := repo.Commit(ctx, message.String())
	if err != nil {
		return err
	}
	if !committed {
		fmt.Println("\nNothing changed; no commit created.")
		return reportFailures(results)
	}

	head, err := repo.Head(ctx)
	if err == nil {
		fmt.Printf("\nCommitted %s: %s\n", head, message.Subject)
	} else {
		fmt.Printf("\nCommitted: %s\n", message.Subject)
	}

	if s.flags.push || s.cfg.AutoPush {
		branch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return err
		}
		if err := repo.Push(ctx, gitrepo.DefaultRemote, branch); err != nil {
			return err
		}
		fmt.Printf("Pushed %s to %s\n", branch, gitrepo.DefaultRemote)
	}

	return reportFailures(results)
}

func summarizeStatusChanges(changes []backup.StatusChange) gitrepo.StatusChanges {
	var summary gitrepo.StatusChanges
	for _, c := range changes {
		switch {
		case c.To == shortcuts.StatusDeleted:
			summary.Deleted = append(summary.Deleted, c.Name)
		case c.Restored():
			summary.Restored = append(summary.Restored, c.Name)
		case c.To == shortcuts.StatusConflict:
			summary.Conflicts = append(summary.Conflicts, c.Name)
		}
	}
	return summary
}

func printStatusChanges(summary gitrepo.StatusChanges) {
	for _, group := range []struct {
		label string
		names []string
	}{
		{"marked deleted", summary.Deleted},
		{"restored", summary.Restored},
		{"marked conflict", summary.Conflicts},
	} {
		for _, name := range group.names {
			fmt.Printf("%-16s %s\n", group.label, name)
		}
	}
}

func reportFailures(results []tui.Result) error {
	var failures []tui.Result
	for _, r := range results {
		if r.Outcome == tui.OutcomeFailed {
			failures = append(failures, r)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d shortcut(s) failed:", len(failures))
	for _, f := range failures {
		fmt.Fprintf(&b, "\n  %s: %v", f.Shortcut.Name, f.Err)
	}
	return fmt.Errorf("%s", b.String())
}

func countBy(results []tui.Result, outcome tui.Outcome) int {
	n := 0
	for _, r := range results {
		if r.Outcome == outcome {
			n++
		}
	}
	return n
}

func matchShortcuts(all []shortcuts.Shortcut, names []string) ([]shortcuts.Shortcut, error) {
	byName := map[string][]shortcuts.Shortcut{}
	for _, sc := range all {
		byName[strings.ToLower(sc.Name)] = append(byName[strings.ToLower(sc.Name)], sc)
	}

	var selected []shortcuts.Shortcut
	var missing []string
	for _, want := range names {
		matches, ok := byName[strings.ToLower(want)]
		if !ok {
			missing = append(missing, want)
			continue
		}
		selected = append(selected, matches...)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("no shortcut named %s (run `sss list` to see what is available)",
			strings.Join(quoteAll(missing), ", "))
	}
	return selected, nil
}

func quoteAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

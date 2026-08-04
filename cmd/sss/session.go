package main

import (
	"context"
	"fmt"
	"os"
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

func openSession(flags *globalFlags) (*session, error) {
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
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w\n\nRun `sss init` to create a config file", err)
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

	sitePaths, err := s.service.BuildSite()
	if err != nil {
		return fmt.Errorf("building the site: %w", err)
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
	message := gitrepo.ShortcutBackupMessage(names...)
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

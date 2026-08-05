package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/gitrepo"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/andrewmcodes/shortcut-source-saver/internal/tui"
)

func newListCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every shortcut in your library",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lib, err := shortcuts.Open(flags.dbPath)
			if err != nil {
				return err
			}
			defer lib.Close()

			all, err := lib.List()
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tACTIONS\tMODIFIED\tID")
			shown := 0
			for _, sc := range all {
				if sc.Hidden && !flags.includeHidden {
					continue
				}
				modified := "-"
				if !sc.Modified.IsZero() {
					modified = sc.Modified.Format("2006-01-02")
				}
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", sc.Name, sc.ActionCount, modified, sc.WorkflowID)
				shown++
			}
			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "\n%d shortcut(s)\n", shown)
			return nil
		},
	}
}

func newBackupCommand(flags *globalFlags) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "backup [name...]",
		Short: "Back up shortcuts by name, without the picker",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("name at least one shortcut, or pass --all")
			}
			if all && len(args) > 0 {
				return fmt.Errorf("--all takes no names")
			}

			s, err := openSession(flags)
			if err != nil {
				return err
			}
			defer s.Close()

			available, err := s.service.List()
			if err != nil {
				return err
			}

			selected := available
			if !all {
				if selected, err = matchShortcuts(available, args); err != nil {
					return err
				}
			}
			if len(selected) == 0 {
				return fmt.Errorf("nothing to back up")
			}

			ctx := cmd.Context()
			results := make([]tui.Result, 0, len(selected))
			for _, sc := range selected {
				result := s.service.Backup(ctx, sc)
				results = append(results, result)
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s %s", result.Outcome, sc.Name)
				if result.Detail != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " (%s)", result.Detail)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return s.finish(ctx, results)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "back up every shortcut in your library")
	return cmd
}

func newBuildCommand(flags *globalFlags) *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Regenerate the web reviews and site index from the repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return err
			}
			if flags.repoPath != "" {
				expanded, err := config.ExpandTilde(flags.repoPath)
				if err != nil {
					return err
				}
				cfg.RepoPath = expanded
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			s := &session{cfg: cfg, flags: flags}
			s.service = newSiteOnlyService(cfg, flags)

			written, err := s.service.BuildSite()
			if err != nil {
				return err
			}
			if len(written) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No shortcuts found in the repository; nothing to build.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d file(s) to %s\n", len(written), cfg.SitePath())

			if !check {
				return nil
			}
			repo, err := gitrepo.Open(cmd.Context(), cfg.RepoPath)
			if err != nil {
				return err
			}
			stale, err := siteChanges(cmd.Context(), repo, cfg.SitePath())
			if err != nil {
				return err
			}
			if len(stale) > 0 {
				return fmt.Errorf("the generated site is out of date (%s); run `sss build` and commit the result",
					strings.Join(stale, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "fail if regenerating the site changes anything")
	return cmd
}

func newInitCommand(flags *globalFlags) *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write an example config and set up the backup repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			target := flags.configPath
			if target == "" {
				paths := config.SearchPaths()
				if len(paths) == 0 {
					return fmt.Errorf("cannot determine a config location")
				}
				target = paths[0]
			}
			if _, err := os.Stat(target); err == nil {
				fmt.Fprintf(out, "Config already exists at %s\n", target)
			} else {
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(target, []byte(config.Example()), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "Wrote %s\n", target)
			}

			if repo == "" {
				fmt.Fprintf(out, "\nNext: set repo_path in %s, then run `sss`.\n", target)
				return nil
			}

			expanded, err := config.ExpandTilde(repo)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(expanded) {
				return fmt.Errorf("--repo must be an absolute path after expansion, got %q", expanded)
			}

			r, err := gitrepo.Init(cmd.Context(), expanded)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Initialised %s\n", r.Root())

			if err := writeBackupRepoScaffold(expanded); err != nil {
				return err
			}
			fmt.Fprintf(out, "Added a GitHub Pages workflow and .gitignore to %s\n", expanded)
			fmt.Fprintf(out, "\nNext: set repo_path = %q in %s, then run `sss`.\n", expanded, target)
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "create and scaffold the backup repository at this path")
	return cmd
}

func runInteractive(cmd *cobra.Command, flags *globalFlags) error {
	if !stdoutIsTerminal() {
		return fmt.Errorf("not a terminal; use `sss backup --all` or `sss backup <name>` instead")
	}

	s, err := openSession(flags)
	if err != nil {
		return err
	}
	defer s.Close()

	ctx := cmd.Context()
	results, err := tui.Run(s.service, tui.Options{
		Title:       s.cfg.SiteTitle,
		Destination: s.cfg.RepoPath,
		Context:     ctx,
	})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	return s.finish(ctx, results)
}

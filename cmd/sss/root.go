package main

import (
	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath    string
	repoPath      string
	allowSecrets  bool
	includeHidden bool
	expandAll     bool
	noCommit      bool
	push          bool
	dbPath        string
}

func newRootCommand() *cobra.Command {
	flags := &globalFlags{}

	root := &cobra.Command{
		Use:   "sss",
		Short: "Back up Apple Shortcuts into a git repository",
		Long: "sss reads your Apple Shortcuts library and writes each shortcut into a git repository " +
			"as diffable JSON, an importable .shortcut file, and generated documentation, then rebuilds " +
			"a browsable web review of every shortcut.\n\n" +
			"Running sss with no arguments opens a picker.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(cmd, flags)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flags.configPath, "config", "", "path to a config file")
	pf.StringVar(&flags.repoPath, "repo", "", "override the configured backup repository path")
	pf.StringVar(&flags.dbPath, "database", "", "override the Shortcuts database path")
	pf.BoolVar(&flags.allowSecrets, "allow-secrets", false, "back up even when possible credentials are found")
	pf.BoolVar(&flags.includeHidden, "include-hidden", false, "include shortcuts hidden from your library")
	pf.BoolVar(&flags.expandAll, "expand-all", false, "do not collapse large content in web reviews")
	pf.BoolVar(&flags.noCommit, "no-commit", false, "write files but do not commit")
	pf.BoolVar(&flags.push, "push", false, "push after committing")

	root.AddCommand(
		newListCommand(flags),
		newBackupCommand(flags),
		newBuildCommand(flags),
		newInitCommand(flags),
	)
	return root
}

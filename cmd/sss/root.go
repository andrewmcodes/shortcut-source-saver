package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath    string
	repoPath      string
	outputDir     string
	force         bool
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
			"Running sss with no arguments opens a picker. Pass -o DIR to write a backup into any " +
			"directory instead, with no config file, git repository, or commit.",
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

	addOutputFlags(root, flags)

	backup := newBackupCommand(flags)
	root.AddCommand(
		newListCommand(flags),
		backup,
		newBuildCommand(flags),
		newInitCommand(flags),
		newExportCommand(flags),
	)
	addOutputFlags(backup, flags)
	markOutputExclusive(root)
	markOutputExclusive(backup)
	return root
}

func addOutputFlags(cmd *cobra.Command, flags *globalFlags) {
	cmd.Flags().StringVarP(&flags.outputDir, "output", "o", "",
		"write into `DIR` instead of the backup repository (no config or git needed; never commits)")
	_ = cmd.MarkFlagDirname("output")
	cmd.Flags().BoolVar(&flags.force, "force", false,
		"with --output, write into a non-empty directory that is not an sss backup")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		return validateOutputFlags(cmd, flags)
	}
}

func validateOutputFlags(cmd *cobra.Command, flags *globalFlags) error {
	if cmd.Flags().Changed("output") && strings.TrimSpace(flags.outputDir) == "" {
		return fmt.Errorf("--output needs a directory")
	}
	if flags.force && flags.outputDir == "" {
		return fmt.Errorf("--force only applies with --output: it allows writing into a non-empty directory that is not an sss backup")
	}
	return nil
}

func markOutputExclusive(cmd *cobra.Command) {
	cmd.MarkFlagsMutuallyExclusive("output", "repo")
	cmd.MarkFlagsMutuallyExclusive("output", "push")
}

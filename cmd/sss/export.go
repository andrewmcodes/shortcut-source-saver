package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/export"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

func newExportCommand(flags *globalFlags) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write every shortcut's JSON into a single .zip, without git or signing",
		Long: "export reads your whole Shortcuts library and writes each shortcut's canonical JSON and " +
			"metadata into one .zip, laid out like a backup repository. It needs no config file, never " +
			"signs, and never touches git.\n\n" +
			"Every shortcut is read and encoded before anything is written, so a failure leaves no file " +
			"behind. An existing output file is never overwritten.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := exportTarget(output, time.Now())
			if err != nil {
				return err
			}

			lib, err := shortcuts.Open(flags.dbPath)
			if err != nil {
				return err
			}
			defer lib.Close()

			files, count, err := export.Collect(lib, export.Options{
				IncludeHidden: flags.includeHidden,
				AllowSecrets:  flags.allowSecrets,
			})
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("no shortcuts to export")
			}

			if err := export.CreateExclusive(target, func(w io.Writer) error {
				return export.WriteZip(w, files)
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exported %d shortcut(s) to %s\n", count, target)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"zip file to create, or a directory to create it in (default: ./shortcuts-<timestamp>.zip)")
	return cmd
}

func exportTarget(output string, now time.Time) (string, error) {
	name := "shortcuts-" + now.Format("20060102-150405") + ".zip"
	if output == "" {
		return name, nil
	}
	expanded, err := config.ExpandTilde(output)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(expanded); err == nil && info.IsDir() {
		return filepath.Join(expanded, name), nil
	}
	return expanded, nil
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andrewmcodes/shortcut-source-saver/internal/backup"
	"github.com/andrewmcodes/shortcut-source-saver/internal/config"
	"github.com/andrewmcodes/shortcut-source-saver/internal/gitrepo"
)

func siteChanges(ctx context.Context, repo *gitrepo.Repo, sitePath string) ([]string, error) {
	statuses, err := repo.Status(ctx)
	if err != nil {
		return nil, err
	}

	prefix, err := filepath.Rel(repo.Root(), sitePath)
	if err != nil || strings.HasPrefix(prefix, "..") {
		return nil, fmt.Errorf("site_dir %s is outside the repository at %s", sitePath, repo.Root())
	}
	prefix = filepath.ToSlash(prefix)

	var changed []string
	for _, status := range statuses {
		path := filepath.ToSlash(status.Path)
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	if len(changed) > maxReportedSiteChanges {
		changed = append(changed[:maxReportedSiteChanges],
			fmt.Sprintf("and %d more", len(changed)-maxReportedSiteChanges))
	}
	return changed, nil
}

const maxReportedSiteChanges = 5

func newSiteOnlyService(cfg *config.Config, flags *globalFlags) *backup.Service {
	return backup.New(cfg, nil, backup.Options{
		AllowSecrets:  flags.allowSecrets,
		IncludeHidden: cfg.IncludeHidden,
		ExpandAll:     flags.expandAll,
	})
}

const pagesWorkflow = `name: Deploy web reviews

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  deploy:
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/configure-pages@v5
      - uses: actions/upload-pages-artifact@v3
        with:
          path: docs
      - id: deployment
        uses: actions/deploy-pages@v4
`

const backupRepoGitignore = `.DS_Store
`

const backupRepoReadme = `# My Shortcuts

Apple Shortcuts backed up by [shortcut-source-saver](https://github.com/andrewmcodes/shortcut-source-saver).

Each shortcut lives under ` + "`shortcuts/<slug>/`" + ` with:

- ` + "`shortcut.json`" + ` — the shortcut's actions in canonical JSON, which is what git diffs
- ` + "`metadata.json`" + ` — name, identifier, icon, action count, dates
- ` + "`<Name>.shortcut`" + ` — signed and importable; double-click to add it to Shortcuts
- ` + "`README.md`" + ` — generated documentation

Browsable web reviews of every shortcut are published to GitHub Pages from ` + "`docs/`" + `.

## Restoring a shortcut

Double-click the ` + "`.shortcut`" + ` file, or run:

` + "```sh\nopen \"shortcuts/<slug>/<Name>.shortcut\"\n```" + `
`

func writeBackupRepoScaffold(root string) error {
	files := map[string]string{
		filepath.Join(".github", "workflows", "pages.yml"): pagesWorkflow,
		".gitignore": backupRepoGitignore,
		"README.md":  backupRepoReadme,
	}
	for rel, contents := range files {
		target := filepath.Join(root, rel)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
			return err
		}
	}
	return nil
}

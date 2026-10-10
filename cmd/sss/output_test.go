package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/backup"
	"github.com/andrewmcodes/shortcut-source-saver/internal/tui"
)

func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func samePath(t *testing.T, got, want string) {
	t.Helper()
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	if resolve(got) != resolve(want) {
		t.Errorf("path = %s, want %s", got, want)
	}
}

func TestOutputFlagConflicts(t *testing.T) {
	const exclusive = "[output "
	cases := map[string]struct {
		args []string
		want string
	}{
		"backup with push":       {[]string{"backup", "--all", "-o", "out", "--push"}, exclusive},
		"backup with repo":       {[]string{"backup", "--all", "--output", "out", "--repo", "/tmp/elsewhere"}, exclusive},
		"picker with push":       {[]string{"-o", "out", "--push"}, exclusive},
		"push before backup":     {[]string{"--push", "backup", "--all", "-o", "out"}, exclusive},
		"backup force alone":     {[]string{"backup", "--all", "--force"}, "--force only applies with --output"},
		"picker force alone":     {[]string{"--force"}, "--force only applies with --output"},
		"empty output directory": {[]string{"backup", "--all", "--output="}, "--output needs a directory"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := newRootCommand()
			root.SetArgs(append(tc.args, "--database", filepath.Join(t.TempDir(), "missing.sqlite")))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			err := root.Execute()
			if err == nil {
				t.Fatal("Execute succeeded, want a flag error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func writeBackedUpShortcut(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "shortcuts/encode-url/shortcut.json", `{"WFWorkflowActions":[]}`)
	writeFile(t, dir, "shortcuts/encode-url/metadata.json", `{"name":"Encode URL","workflow_id":"44434252-BEA5-4DA6-BC72-949BD054B2E3"}`)
}

func TestOutputDirGuard(t *testing.T) {
	isolateConfig(t)

	foreign := t.TempDir()
	writeFile(t, foreign, "docs/index.html", "<p>my project docs</p>")
	writeFile(t, foreign, "notes.txt", "mine")

	ours := t.TempDir()
	writeBackedUpShortcut(t, ours)
	writeFile(t, ours, ".gitignore", "bin/")

	empty := t.TempDir()
	writeFile(t, empty, ".DS_Store", "")

	cases := map[string]struct {
		flags   globalFlags
		refused bool
	}{
		"foreign non-empty dir is refused":   {globalFlags{outputDir: foreign}, true},
		"--force allows a foreign dir":       {globalFlags{outputDir: foreign, force: true}, false},
		"re-running into sss output is fine": {globalFlags{outputDir: ours}, false},
		"empty dir is fine":                  {globalFlags{outputDir: empty}, false},
		"missing dir is fine":                {globalFlags{outputDir: filepath.Join(empty, "new")}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadSessionConfig(&tc.flags)
			if !tc.refused {
				if err != nil {
					t.Fatalf("loadSessionConfig: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("loadSessionConfig accepted a non-empty directory that is not an sss backup")
			}
			for _, want := range []string{"--force", "docs/ already exists"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to mention %q", err, want)
				}
			}
		})
	}

	if got, _ := os.ReadFile(filepath.Join(foreign, "docs", "index.html")); string(got) != "<p>my project docs</p>" {
		t.Error("the guard let something overwrite the foreign directory")
	}
}

func TestOutputDirNeedsNoConfigAndResolvesAgainstTheWorkingDirectory(t *testing.T) {
	isolateConfig(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	cfg, err := loadSessionConfig(&globalFlags{outputDir: "a/folder"})
	if err != nil {
		t.Fatalf("loadSessionConfig: %v", err)
	}
	samePath(t, cfg.RepoPath, filepath.Join(cwd, "a", "folder"))
	samePath(t, cfg.SitePath(), filepath.Join(cwd, "a", "folder", "docs"))
	if cfg.AutoCommit || cfg.AutoPush {
		t.Errorf("AutoCommit = %v, AutoPush = %v; --output must never commit or push", cfg.AutoCommit, cfg.AutoPush)
	}
	if _, err := os.Stat(filepath.Join(cwd, "a")); !os.IsNotExist(err) {
		t.Error("loadSessionConfig created the output directory before anything was backed up")
	}
}

func TestOutputDirKeepsEverythingInsideItEvenWithAConfig(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	writeFile(t, dir, "config.toml", `repo_path = "/Users/nobody/git/shortcuts"
site_dir = "/Users/nobody/site"
auto_commit = true
auto_push = true
sign_mode = "people-who-know-me"
`)
	out := filepath.Join(dir, "out")

	cfg, err := loadSessionConfig(&globalFlags{configPath: configPath, outputDir: out})
	if err != nil {
		t.Fatalf("loadSessionConfig: %v", err)
	}
	if cfg.RepoPath != out {
		t.Errorf("RepoPath = %s, want %s", cfg.RepoPath, out)
	}
	if !strings.HasPrefix(cfg.SitePath(), out+string(filepath.Separator)) {
		t.Errorf("SitePath = %s, want it inside %s", cfg.SitePath(), out)
	}
	if cfg.AutoCommit || cfg.AutoPush {
		t.Errorf("AutoCommit = %v, AutoPush = %v; --output must override the config", cfg.AutoCommit, cfg.AutoPush)
	}
	if cfg.SignMode != "people-who-know-me" {
		t.Errorf("SignMode = %q, want the configured value kept", cfg.SignMode)
	}
}

func TestOutputDirMustNotBeAFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "taken", "x")
	if _, err := resolveOutputDir(filepath.Join(dir, "taken")); err == nil {
		t.Error("resolveOutputDir accepted a regular file")
	}
}

func TestFinishIntoAnOutputDirWritesTheSiteWithoutGit(t *testing.T) {
	isolateConfig(t)
	dir := filepath.Join(t.TempDir(), "plain")
	writeBackedUpShortcut(t, dir)

	flags := &globalFlags{outputDir: dir}
	cfg, err := loadSessionConfig(flags)
	if err != nil {
		t.Fatalf("loadSessionConfig: %v", err)
	}
	s := &session{cfg: cfg, flags: flags, service: backup.New(cfg, nil, backup.Options{})}

	if err := s.finish(context.Background(), []tui.Result{{Outcome: tui.OutcomeUnchanged}}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Error("finish created a git repository in the output directory")
	}
	index, err := os.ReadFile(filepath.Join(dir, "docs", "index.html"))
	if err != nil {
		t.Fatalf("site index was not written: %v", err)
	}
	if strings.Contains(string(index), `"/assets/`) || strings.Contains(string(index), `"/shortcuts/`) {
		t.Error("site index uses root-absolute links, which break when opened from disk")
	}
}

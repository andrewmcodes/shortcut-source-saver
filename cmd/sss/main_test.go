package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmcodes/shortcut-source-saver/internal/gitrepo"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

func named(name, id string) shortcuts.Shortcut {
	return shortcuts.Shortcut{Name: name, WorkflowID: id}
}

var library = []shortcuts.Shortcut{
	named("Encode URL", "44434252-BEA5-4DA6-BC72-949BD054B2E3"),
	named("Move to Archive", "FB173261-F28B-4159-9B86-1C5A54500043"),
	named("Summarize PDF", "AAAAAAAA-0000-0000-0000-000000000001"),
	named("Summarize PDF", "BBBBBBBB-0000-0000-0000-000000000002"),
	named("💥💥Add book", "CCCCCCCC-0000-0000-0000-000000000003"),
}

func TestMatchShortcutsFindsByExactName(t *testing.T) {
	got, err := matchShortcuts(library, []string{"Encode URL"})
	if err != nil {
		t.Fatalf("matchShortcuts: %v", err)
	}
	if len(got) != 1 || got[0].WorkflowID != library[0].WorkflowID {
		t.Errorf("matchShortcuts = %v, want just Encode URL", got)
	}
}

func TestMatchShortcutsIsCaseInsensitive(t *testing.T) {
	got, err := matchShortcuts(library, []string{"encode url"})
	if err != nil {
		t.Fatalf("matchShortcuts: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("matchShortcuts = %v, want one match", got)
	}
}

func TestMatchShortcutsReturnsEveryShortcutSharingAName(t *testing.T) {
	got, err := matchShortcuts(library, []string{"Summarize PDF"})
	if err != nil {
		t.Fatalf("matchShortcuts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("matchShortcuts returned %d shortcuts, want both shortcuts named Summarize PDF", len(got))
	}
	if got[0].WorkflowID == got[1].WorkflowID {
		t.Error("matchShortcuts returned the same shortcut twice")
	}
}

func TestMatchShortcutsHandlesEmojiNames(t *testing.T) {
	if _, err := matchShortcuts(library, []string{"💥💥Add book"}); err != nil {
		t.Errorf("matchShortcuts: %v", err)
	}
}

func TestMatchShortcutsReportsEveryUnknownName(t *testing.T) {
	_, err := matchShortcuts(library, []string{"Nope", "Encode URL", "Also Missing"})
	if err == nil {
		t.Fatal("matchShortcuts succeeded, want an error naming the unknown shortcuts")
	}
	for _, want := range []string{"Nope", "Also Missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Encode URL") {
		t.Errorf("error %q mentions a name that does exist", err)
	}
}

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func scratchRepo(t *testing.T) *gitrepo.Repo {
	t.Helper()
	gitAvailable(t)

	dir := t.TempDir()
	ctx := context.Background()
	repo, err := gitrepo.Init(ctx, dir)
	if err != nil {
		t.Fatalf("gitrepo.Init: %v", err)
	}
	for _, args := range [][]string{
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return repo
}

func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestSiteChangesIgnoresFilesOutsideTheSiteDirectory(t *testing.T) {
	repo := scratchRepo(t)
	root := repo.Root()
	ctx := context.Background()

	writeFile(t, root, "docs/index.html", "<html></html>")
	writeFile(t, root, "shortcuts/a/shortcut.json", "{}")
	writeFile(t, root, "UNRELATED.md", "scratch")

	changed, err := siteChanges(ctx, repo, filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("siteChanges: %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("siteChanges found nothing, want the uncommitted site reported")
	}
	for _, path := range changed {
		if !strings.HasPrefix(path, "docs") {
			t.Errorf("siteChanges = %v, want only paths under docs/", changed)
		}
	}
}

func TestSiteChangesReportsNothingWhenTheSiteIsCommitted(t *testing.T) {
	repo := scratchRepo(t)
	root := repo.Root()
	ctx := context.Background()

	writeFile(t, root, "docs/index.html", "<html></html>")
	if err := repo.Add(ctx, "."); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := repo.Commit(ctx, "seed"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeFile(t, root, "UNRELATED.md", "scratch")

	changed, err := siteChanges(ctx, repo, filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("siteChanges: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("siteChanges = %v, want nothing; an unrelated dirty file must not read as a stale site", changed)
	}
}

func TestSiteChangesRejectsASiteDirectoryOutsideTheRepository(t *testing.T) {
	repo := scratchRepo(t)
	outside := t.TempDir()

	if _, err := siteChanges(context.Background(), repo, outside); err == nil {
		t.Error("siteChanges succeeded for a site directory outside the repository, want an error")
	}
}

func TestSiteChangesDoesNotConfuseSiblingDirectories(t *testing.T) {
	repo := scratchRepo(t)
	root := repo.Root()

	writeFile(t, root, "docs/index.html", "<html></html>")
	writeFile(t, root, "docs-draft/index.html", "<html></html>")

	changed, err := siteChanges(context.Background(), repo, filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("siteChanges: %v", err)
	}
	for _, path := range changed {
		if strings.HasPrefix(path, "docs-draft/") {
			t.Errorf("siteChanges = %v, want docs-draft/ excluded", changed)
		}
	}
}

func TestRootCommandHasEverySubcommand(t *testing.T) {
	root := newRootCommand()
	want := map[string]bool{"list": false, "backup": false, "build": false, "init": false}
	for _, sub := range root.Commands() {
		if _, tracked := want[sub.Name()]; tracked {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q is missing", name)
		}
	}
}

func TestSanitizeAndSlugAgreeOnDirectoryAndFileNaming(t *testing.T) {
	root := newRootCommand()
	if root.Use != "sss" {
		t.Errorf("root command Use = %q, want sss", root.Use)
	}
	if !root.SilenceUsage {
		t.Error("root command should silence usage so errors are not buried in help output")
	}
}

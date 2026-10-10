package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		os.Stderr.WriteString("skipping gitrepo tests: git is not on PATH\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_AUTHOR_NAME", "Test Runner")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test Runner")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func newRepo(t *testing.T) *Repo {
	t.Helper()
	isolateGit(t)

	dir := t.TempDir()
	gitExec(t, dir, "init")
	gitExec(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	gitExec(t, dir, "config", "user.name", "Test Runner")
	gitExec(t, dir, "config", "user.email", "test@example.com")
	gitExec(t, dir, "config", "commit.gpgsign", "false")
	gitExec(t, dir, "config", "tag.gpgsign", "false")
	gitExec(t, dir, "config", "core.hooksPath", filepath.Join(t.TempDir(), "no-hooks"))

	repo, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return repo
}

func writeFile(t *testing.T, repo *Repo, name, contents string) string {
	t.Helper()
	path := filepath.Join(repo.Root(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenNonRepository(t *testing.T) {
	isolateGit(t)

	_, err := Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("want ErrNotRepository, got %v", err)
	}
	if !strings.Contains(err.Error(), "git init") {
		t.Errorf("error should suggest `git init`, got %q", err)
	}
}

func TestOpenMissingDirectory(t *testing.T) {
	isolateGit(t)

	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("want an error for a missing directory")
	}
	if errors.Is(err, ErrNotRepository) {
		t.Errorf("a missing directory is not the same as a non-repository: %v", err)
	}
}

func TestOpenFromSubdirectoryReturnsToplevel(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "shortcuts/example/shortcut.json", "{}")

	nested, err := Open(context.Background(), filepath.Join(repo.Root(), "shortcuts", "example"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !sameDir(nested.Root(), repo.Root()) {
		t.Errorf("root = %q, want %q", nested.Root(), repo.Root())
	}
}

func TestInitIsIdempotent(t *testing.T) {
	isolateGit(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "backups", "shortcuts")

	first, err := Init(ctx, path)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.Root(), ".git")); err != nil {
		t.Fatalf("expected a .git directory: %v", err)
	}

	second, err := Init(ctx, path)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if first.Root() != second.Root() {
		t.Errorf("roots differ: %q vs %q", first.Root(), second.Root())
	}
	if !sameDir(second.Root(), path) {
		t.Errorf("root = %q, want %q", second.Root(), path)
	}
}

func TestAddAndCommit(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	clean, err := repo.IsClean(ctx)
	if err != nil {
		t.Fatalf("IsClean: %v", err)
	}
	if !clean {
		t.Fatal("a fresh repository should be clean")
	}

	absolute := writeFile(t, repo, "shortcuts/daily-note/shortcut.json", "{\"name\":\"Daily Note\"}\n")
	writeFile(t, repo, "shortcuts/daily-note/README.md", "# Daily Note\n")

	if clean, err = repo.IsClean(ctx); err != nil || clean {
		t.Fatalf("IsClean after writing files = %v, %v; want false, nil", clean, err)
	}

	if err := repo.Add(ctx, absolute, "shortcuts/daily-note/README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	staged, err := repo.HasStagedChanges(ctx)
	if err != nil || !staged {
		t.Fatalf("HasStagedChanges = %v, %v; want true, nil", staged, err)
	}

	committed, err := repo.Commit(ctx, ShortcutBackupMessage("Daily Note").String())
	if err != nil || !committed {
		t.Fatalf("Commit = %v, %v; want true, nil", committed, err)
	}

	head, err := repo.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(head) < 4 {
		t.Errorf("Head = %q, want a short hash", head)
	}

	if clean, err = repo.IsClean(ctx); err != nil || !clean {
		t.Fatalf("IsClean after commit = %v, %v; want true, nil", clean, err)
	}

	subject := strings.TrimSpace(gitExec(t, repo.Root(), "log", "-1", "--pretty=%s"))
	if subject != "chore(shortcuts): back up Daily Note" {
		t.Errorf("subject = %q", subject)
	}
}

func TestRemoveStagesARenamedDirectory(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	writeFile(t, repo, "shortcuts/foo/shortcut.json", "{\"a\":1}\n")
	if err := repo.Add(ctx, "shortcuts/foo/shortcut.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, "seed"); err != nil {
		t.Fatal(err)
	}

	root := repo.Root()
	if err := os.Rename(filepath.Join(root, "shortcuts", "foo"), filepath.Join(root, "shortcuts", "bar")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(ctx, filepath.Join(root, "shortcuts", "foo"), filepath.Join(root, "shortcuts", "never-tracked")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := repo.Add(ctx, "shortcuts/bar/shortcut.json"); err != nil {
		t.Fatal(err)
	}

	status := gitExec(t, root, "status", "--porcelain")
	if !strings.Contains(status, "R  shortcuts/foo/shortcut.json -> shortcuts/bar/shortcut.json") {
		t.Errorf("status = %q, want a staged rename", status)
	}
}

func TestCommitWithNothingStagedIsANoOp(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	writeFile(t, repo, "shortcuts/one/shortcut.json", "{}")
	if err := repo.Add(ctx, "shortcuts/one/shortcut.json"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := repo.Commit(ctx, "chore(shortcuts): back up One"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	committed, err := repo.Commit(ctx, "chore(shortcuts): back up One")
	if err != nil {
		t.Fatalf("second Commit returned an error: %v", err)
	}
	if committed {
		t.Error("committed = true, want false when nothing is staged")
	}

	count := strings.TrimSpace(gitExec(t, repo.Root(), "rev-list", "--count", "HEAD"))
	if count != "1" {
		t.Errorf("commit count = %s, want 1", count)
	}
}

func TestCommitRejectsEmptyMessage(t *testing.T) {
	repo := newRepo(t)
	if _, err := repo.Commit(context.Background(), "  \n"); err == nil {
		t.Fatal("want an error for an empty commit message")
	}
}

func TestCurrentBranch(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	branch, err := repo.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch on an unborn branch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	writeFile(t, repo, "note.txt", "hello")
	if err := repo.Add(ctx, "note.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, "chore(site): rebuild web reviews"); err != nil {
		t.Fatal(err)
	}
	gitExec(t, repo.Root(), "checkout", "-q", "-b", "backup/2026-08-04")

	if branch, err = repo.CurrentBranch(ctx); err != nil || branch != "backup/2026-08-04" {
		t.Fatalf("CurrentBranch = %q, %v", branch, err)
	}
}

func TestCurrentBranchDetachedHead(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	writeFile(t, repo, "note.txt", "hello")
	if err := repo.Add(ctx, "note.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, "chore(site): rebuild web reviews"); err != nil {
		t.Fatal(err)
	}
	gitExec(t, repo.Root(), "checkout", "-q", "--detach")

	if _, err := repo.CurrentBranch(ctx); !errors.Is(err, ErrDetachedHead) {
		t.Fatalf("want ErrDetachedHead, got %v", err)
	}
}

func TestHeadWithoutCommits(t *testing.T) {
	repo := newRepo(t)
	if _, err := repo.Head(context.Background()); !errors.Is(err, ErrNoCommits) {
		t.Fatalf("want ErrNoCommits, got %v", err)
	}
}

func TestAddPathOutsideRepository(t *testing.T) {
	repo := newRepo(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Add(context.Background(), outside); !errors.Is(err, ErrOutsideRepo) {
		t.Fatalf("want ErrOutsideRepo, got %v", err)
	}
}

func TestAddPathThatLooksLikeAFlag(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	writeFile(t, repo, "--all", "not a flag\n")

	if err := repo.Add(ctx, "--all"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	statuses, err := repo.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Path != "--all" || statuses[0].Index != 'A' {
		t.Fatalf("status = %+v", statuses)
	}
}

func TestAddWithNoPathsDoesNothing(t *testing.T) {
	repo := newRepo(t)
	if err := repo.Add(context.Background()); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestStatusReportsUntrackedFiles(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "docs/index.html", "<!doctype html>")

	statuses, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("status = %+v, want one entry", statuses)
	}
	if !statuses[0].Untracked() || statuses[0].Path != "docs/" {
		t.Errorf("status = %+v", statuses[0])
	}
}

func TestPushWithoutRemote(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	present, err := repo.HasRemote(ctx, DefaultRemote)
	if err != nil {
		t.Fatalf("HasRemote: %v", err)
	}
	if present {
		t.Fatal("a fresh repository should have no remotes")
	}

	err = repo.Push(ctx, "", "")
	if !errors.Is(err, ErrNoRemote) {
		t.Fatalf("want ErrNoRemote, got %v", err)
	}
	if !strings.Contains(err.Error(), "git remote add origin") {
		t.Errorf("error should suggest adding a remote, got %q", err)
	}
}

func TestCancelledContext(t *testing.T) {
	repo := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.Status(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

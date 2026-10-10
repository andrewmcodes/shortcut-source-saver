package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	ErrGitNotInstalled = errors.New("git is not installed or not on PATH")
	ErrNotRepository   = errors.New("not a git repository")
	ErrOutsideRepo     = errors.New("outside the repository")
	ErrNoRemote        = errors.New("no git remote configured")
	ErrDetachedHead    = errors.New("repository has a detached HEAD")
	ErrNoCommits       = errors.New("repository has no commits yet")
)

const DefaultRemote = "origin"

type Repo struct {
	root string
}

func (r *Repo) Root() string {
	return r.root
}

var (
	lookGitOnce sync.Once
	gitBinary   string
	lookGitErr  error
)

func gitExecutable() (string, error) {
	lookGitOnce.Do(func() {
		path, err := exec.LookPath("git")
		if err != nil {
			lookGitErr = fmt.Errorf("%w: install it with `xcode-select --install`", ErrGitNotInstalled)
			return
		}
		gitBinary = path
	})
	return gitBinary, lookGitErr
}

type CommandError struct {
	Args     []string
	Stderr   string
	ExitCode int

	cause error
}

func (e *CommandError) Error() string {
	detail := strings.TrimSpace(e.Stderr)
	if detail == "" {
		detail = e.cause.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), firstLines(detail, 3))
}

func (e *CommandError) Unwrap() error {
	return e.cause
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return strings.Join(lines, "; ")
	}
	return strings.Join(lines[:n], "; ") + " …"
}

type command struct {
	dir      string
	args     []string
	readOnly bool
}

func run(ctx context.Context, c command) (string, error) {
	binary, err := gitExecutable()
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, binary, c.args...)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if c.readOnly {
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(c.args, " "), ctxErr)
		}
		failure := &CommandError{Args: c.args, Stderr: stderr.String(), ExitCode: -1, cause: err}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			failure.ExitCode = exitErr.ExitCode()
		}
		return stdout.String(), failure
	}
	return stdout.String(), nil
}

func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	return run(ctx, command{dir: r.root, args: args})
}

func (r *Repo) query(ctx context.Context, args ...string) (string, error) {
	return run(ctx, command{dir: r.root, args: args, readOnly: true})
}

func exitCode(err error) int {
	var failure *CommandError
	if errors.As(err, &failure) {
		return failure.ExitCode
	}
	return -1
}

func stderrOf(err error) string {
	var failure *CommandError
	if errors.As(err, &failure) {
		return failure.Stderr
	}
	return ""
}

func Open(ctx context.Context, path string) (*Repo, error) {
	dir, err := absolutePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("no directory at %s: create it, or let `sss` set it up for you", dir)
	case err != nil:
		return nil, err
	case !info.IsDir():
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	out, err := run(ctx, command{dir: dir, args: []string{"rev-parse", "--show-toplevel"}, readOnly: true})
	if err != nil {
		if strings.Contains(strings.ToLower(stderrOf(err)), "not a git repository") {
			return nil, fmt.Errorf("%s is %w: run `git init` there, or point repo_path at one", dir, ErrNotRepository)
		}
		return nil, err
	}

	root := strings.TrimSpace(out)
	if root == "" {
		return nil, fmt.Errorf("%s is %w", dir, ErrNotRepository)
	}
	return &Repo{root: root}, nil
}

func Init(ctx context.Context, path string) (*Repo, error) {
	dir, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	existing, err := Open(ctx, dir)
	switch {
	case err == nil && sameDir(existing.root, dir):
		return existing, nil
	case err != nil && !errors.Is(err, ErrNotRepository):
		return nil, err
	}

	if _, err := run(ctx, command{dir: dir, args: []string{"init"}}); err != nil {
		return nil, err
	}
	return Open(ctx, dir)
}

func (r *Repo) Add(ctx context.Context, paths ...string) error {
	relative := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := r.relativePath(path)
		if err != nil {
			return err
		}
		relative = append(relative, rel)
	}
	if len(relative) == 0 {
		return nil
	}
	_, err := r.git(ctx, append([]string{"add", "--"}, relative...)...)
	return err
}

func (r *Repo) Remove(ctx context.Context, paths ...string) error {
	relative := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := r.relativePath(path)
		if err != nil {
			return err
		}
		relative = append(relative, rel)
	}
	if len(relative) == 0 {
		return nil
	}
	_, err := r.git(ctx, append([]string{"rm", "-r", "--cached", "--ignore-unmatch", "--quiet", "--"}, relative...)...)
	return err
}

func (r *Repo) relativePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("cannot stage an empty path")
	}

	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(r.root, target)
	}

	rel, err := filepath.Rel(resolveSymlinks(r.root), resolveSymlinks(target))
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is %w at %s", path, ErrOutsideRepo, r.root)
	}
	return rel, nil
}

func absolutePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("no repository path given")
	}
	return filepath.Abs(path)
}

func sameDir(a, b string) bool {
	return resolveSymlinks(a) == resolveSymlinks(b)
}

func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent, base := filepath.Split(path)
	parent = filepath.Clean(parent)
	if base == "" || parent == path {
		return path
	}
	return filepath.Join(resolveSymlinks(parent), base)
}

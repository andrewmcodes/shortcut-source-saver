package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (r *Repo) HasStagedChanges(ctx context.Context) (bool, error) {
	_, err := r.query(ctx, "diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	if exitCode(err) == 1 {
		return true, nil
	}
	return false, err
}

func (r *Repo) Commit(ctx context.Context, message string) (bool, error) {
	if strings.TrimSpace(message) == "" {
		return false, errors.New("cannot commit with an empty message")
	}

	staged, err := r.HasStagedChanges(ctx)
	if err != nil {
		return false, err
	}
	if !staged {
		return false, nil
	}

	if _, err := r.git(ctx, "commit", "--message", message); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repo) Head(ctx context.Context) (string, error) {
	out, err := r.query(ctx, "rev-parse", "--verify", "--quiet", "--short", "HEAD")
	if err != nil {
		if exitCode(err) == 1 {
			return "", ErrNoCommits
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.query(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if exitCode(err) == 1 {
			return "", fmt.Errorf("%w: check out a branch before pushing", ErrDetachedHead)
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := r.query(ctx, "remote")
	if err != nil {
		return nil, err
	}
	var remotes []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			remotes = append(remotes, name)
		}
	}
	return remotes, nil
}

func (r *Repo) HasRemote(ctx context.Context, name string) (bool, error) {
	if name == "" {
		name = DefaultRemote
	}
	remotes, err := r.Remotes(ctx)
	if err != nil {
		return false, err
	}
	for _, remote := range remotes {
		if remote == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repo) Push(ctx context.Context, remote, branch string) error {
	if remote == "" {
		remote = DefaultRemote
	}

	present, err := r.HasRemote(ctx, remote)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("%w named %q: add one with `git remote add %s <url>`", ErrNoRemote, remote, remote)
	}

	if branch == "" {
		if branch, err = r.CurrentBranch(ctx); err != nil {
			return err
		}
	}

	_, err = r.git(ctx, "push", remote, branch)
	return err
}

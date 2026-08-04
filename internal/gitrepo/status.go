package gitrepo

import (
	"context"
	"strings"
)

type FileStatus struct {
	Index    byte
	Worktree byte
	Path     string
	OrigPath string
}

func (f FileStatus) Untracked() bool {
	return f.Index == '?' && f.Worktree == '?'
}

func (r *Repo) Status(ctx context.Context) ([]FileStatus, error) {
	out, err := r.query(ctx, "status", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	records := strings.Split(out, "\x00")
	var statuses []FileStatus
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		status := FileStatus{Index: record[0], Worktree: record[1], Path: record[3:]}
		if renamed(status.Index) || renamed(status.Worktree) {
			if i+1 < len(records) {
				i++
				status.OrigPath = records[i]
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func renamed(code byte) bool {
	return code == 'R' || code == 'C'
}

func (r *Repo) IsClean(ctx context.Context) (bool, error) {
	statuses, err := r.Status(ctx)
	if err != nil {
		return false, err
	}
	return len(statuses) == 0, nil
}

package tui

import (
	"context"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

type Backend interface {
	List() ([]shortcuts.Shortcut, error)
	Backup(ctx context.Context, s shortcuts.Shortcut) Result
}

type Outcome int

const (
	OutcomeWritten Outcome = iota
	OutcomeUnchanged
	OutcomeFailed
	OutcomeSkipped
)

func (o Outcome) String() string {
	switch o {
	case OutcomeWritten:
		return "written"
	case OutcomeUnchanged:
		return "unchanged"
	case OutcomeFailed:
		return "failed"
	case OutcomeSkipped:
		return "skipped"
	}
	return "unknown"
}

type Result struct {
	Shortcut shortcuts.Shortcut
	Outcome  Outcome
	Detail   string
	Err      error
}

func HasFailures(results []Result) bool {
	for _, r := range results {
		if r.Outcome == OutcomeFailed || r.Err != nil {
			return true
		}
	}
	return false
}

func countByOutcome(results []Result) map[Outcome]int {
	counts := make(map[Outcome]int, 4)
	for _, r := range results {
		counts[r.Outcome]++
	}
	return counts
}

func failures(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Outcome == OutcomeFailed || r.Err != nil {
			out = append(out, r)
		}
	}
	return out
}

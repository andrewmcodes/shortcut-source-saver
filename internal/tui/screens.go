package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(keyMsg, m.keys.Cancel) || key.Matches(keyMsg, m.keys.Close) {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) loadingView() string {
	if m.loadErr != nil {
		return m.styles.body.Render(strings.Join([]string{
			m.styles.errorText.Render("Could not read your Shortcuts library"),
			clampWidth(m.width-2, m.loadErr.Error()),
			"",
			m.help.ShortHelpView(m.keys.summaryShortHelp()),
		}, "\n"))
	}
	return m.styles.body.Render("Reading your Shortcuts library…")
}

func (m Model) updateSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	if m.list.SettingFilter() {
		if key.Matches(keyMsg, m.keys.Cancel) {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	m.notice = ""

	switch {
	case key.Matches(keyMsg, m.keys.Cancel), key.Matches(keyMsg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(keyMsg, m.keys.Toggle):
		if item, ok := m.list.SelectedItem().(shortcutItem); ok {
			m.selection.toggle(item.shortcut.WorkflowID)
		}
		return m, nil

	case key.Matches(keyMsg, m.keys.ToggleAll):
		m.toggleVisible()
		return m, nil

	case key.Matches(keyMsg, m.keys.Clear):
		m.selection.clear()
		return m, nil

	case key.Matches(keyMsg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.resizeList()
		return m, nil

	case key.Matches(keyMsg, m.keys.Confirm):
		if m.selection.count() == 0 {
			m.notice = "Nothing selected — press space to pick a shortcut, or a to take everything shown."
			return m, nil
		}
		return m.startBackup()
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) toggleVisible() {
	visible := m.list.VisibleItems()
	if len(visible) == 0 {
		return
	}

	allSelected := true
	for _, raw := range visible {
		item, ok := raw.(shortcutItem)
		if ok && !m.selection.has(item.shortcut.WorkflowID) {
			allSelected = false
			break
		}
	}

	for _, raw := range visible {
		item, ok := raw.(shortcutItem)
		if !ok {
			continue
		}
		if allSelected {
			m.selection.remove(item.shortcut.WorkflowID)
		} else {
			m.selection.add(item.shortcut.WorkflowID)
		}
	}
}

func (m Model) selectHelpView() string {
	if m.help.ShowAll {
		return m.help.FullHelpView(m.keys.selectFullHelp())
	}
	return m.help.ShortHelpView(m.keys.selectShortHelp())
}

func (m Model) selectView() string {
	m.list.Title = fmt.Sprintf("%s · %d selected", m.opts.Title, m.selection.count())

	sections := []string{m.list.View()}
	if m.notice != "" {
		sections = append(sections, m.styles.notice.Render(clampWidth(m.width, m.notice)))
	}
	sections = append(sections, m.styles.help.Render(m.selectHelpView()))
	return strings.Join(sections, "\n")
}

func (m Model) updateProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && key.Matches(keyMsg, m.keys.Cancel) {
		return m.cancelBackup(), nil
	}
	return m, nil
}

func (m Model) progressView() string {
	total := len(m.queue)
	done := m.cursor
	fraction := 0.0
	if total > 0 {
		fraction = float64(done) / float64(total)
	}

	current := "finishing up…"
	if done < total {
		current = m.queue[done].Name
	}

	lines := []string{
		m.styles.heading.Render(fmt.Sprintf("Backing up %d of %d", min(done+1, total), total)),
		m.bar.ViewAs(fraction),
		clampWidth(m.width, m.styles.cursor.Render("❯ ")+m.styles.name.Render(current)),
		"",
	}
	lines = append(lines, m.resultLog(m.logHeight())...)
	lines = append(lines, "", m.styles.help.Render(m.help.ShortHelpView(m.keys.progressShortHelp())))

	return m.styles.body.Render(strings.Join(lines, "\n"))
}

func (m Model) logHeight() int {
	height := m.height - 8
	if height < 1 {
		height = 1
	}
	return height
}

func (m Model) resultLog(limit int) []string {
	results := m.results
	if len(results) > limit {
		results = results[len(results)-limit:]
	}
	lines := make([]string, 0, len(results)+1)
	if hidden := len(m.results) - len(results); hidden > 0 {
		lines = append(lines, m.styles.meta.Render(fmt.Sprintf("… %d earlier", hidden)))
	}
	for _, r := range results {
		lines = append(lines, clampWidth(m.width-2, m.resultLine(r)))
	}
	return lines
}

func (m Model) resultLine(r Result) string {
	style := m.styles.outcome(r.Outcome)
	line := style.Render(outcomeGlyph(r.Outcome)) + " " + r.Shortcut.Name
	detail := r.Detail
	if r.Err != nil {
		detail = r.Err.Error()
	}
	if detail == "" {
		detail = r.Outcome.String()
	}
	return line + " " + m.styles.meta.Render("— "+detail)
}

func (m Model) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(keyMsg, m.keys.Close) || key.Matches(keyMsg, m.keys.Cancel) {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) summaryView() string {
	counts := countByOutcome(m.results)

	headline := "Backup complete"
	if m.cancelled {
		headline = "Backup cancelled"
	}
	if len(m.results) == 0 {
		headline = "Nothing to back up"
	}

	tally := []string{
		m.styles.written.Render(fmt.Sprintf("%d written", counts[OutcomeWritten])),
		m.styles.unchanged.Render(fmt.Sprintf("%d unchanged", counts[OutcomeUnchanged])),
		m.styles.failed.Render(fmt.Sprintf("%d failed", counts[OutcomeFailed])),
	}
	if counts[OutcomeSkipped] > 0 {
		tally = append(tally, m.styles.skipped.Render(fmt.Sprintf("%d skipped", counts[OutcomeSkipped])))
	}

	lines := []string{
		m.styles.heading.Render(headline),
		clampWidth(m.width-2, strings.Join(tally, "  ")),
	}

	if bad := failures(m.results); len(bad) > 0 {
		lines = append(lines, "", m.styles.failed.Render("Failures"))
		for _, r := range bad {
			reason := r.Detail
			if r.Err != nil {
				reason = r.Err.Error()
			}
			lines = append(lines, clampWidth(m.width-2, fmt.Sprintf("  %s — %s", r.Shortcut.Name, reason)))
		}
	}

	if counts[OutcomeWritten] > 0 {
		hint := "Wrote " + plural(counts[OutcomeWritten], "shortcut")
		if m.opts.Destination != "" {
			hint += " to " + m.opts.Destination
		}
		lines = append(lines, "", m.styles.meta.Render(clampWidth(m.width-2, hint)))
	}

	lines = append(lines, "", m.styles.help.Render(m.help.ShortHelpView(m.keys.summaryShortHelp())))
	return m.styles.body.Render(strings.Join(lines, "\n"))
}

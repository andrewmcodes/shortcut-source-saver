package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/archive"
	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type shortcutItem struct {
	shortcut shortcuts.Shortcut
}

func (i shortcutItem) FilterValue() string { return i.shortcut.Name }

func itemsOf(all []shortcuts.Shortcut) []list.Item {
	out := make([]list.Item, 0, len(all))
	for _, s := range all {
		out = append(out, shortcutItem{shortcut: s})
	}
	return out
}

type itemDelegate struct {
	selection *selection
	styles    styles
	clock     func() time.Time
}

func (d itemDelegate) Height() int                         { return 1 }
func (d itemDelegate) Spacing() int                        { return 0 }
func (d itemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

const (
	cursorWidth   = 2
	checkboxWidth = 4
	swatchWidth   = 3
	minNameWidth  = 6
)

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(shortcutItem)
	if !ok {
		return
	}
	s := entry.shortcut
	width := m.Width()
	if width <= 0 {
		width = 80
	}

	cursor := "  "
	nameStyle := d.styles.name
	if index == m.Index() {
		cursor = d.styles.cursor.Render("❯ ")
		nameStyle = d.styles.nameActive
	}

	box := d.styles.unchecked.Render("[ ]")
	if d.selection.has(s.WorkflowID) {
		box = d.styles.checked.Render("[x]")
	}

	swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(archive.HexColor(s.Icon.Color))).Render("██")

	meta := shortcutMeta(s, d.clock())
	if s.Hidden {
		meta = "hidden · " + meta
	}

	remaining := width - cursorWidth - checkboxWidth - swatchWidth
	nameWidth := remaining - lipgloss.Width(meta) - 2
	if nameWidth < minNameWidth {
		nameWidth = remaining
		meta = ""
	}
	if nameWidth < 1 {
		fmt.Fprint(w, clampWidth(width, s.Name))
		return
	}

	row := cursor + box + " " + swatch + " " + cell(nameWidth, nameStyle.Render(s.Name))
	if meta != "" {
		row += "  " + d.styles.meta.Render(meta)
	}
	fmt.Fprint(w, clampWidth(width, row))
}

func shortcutMeta(s shortcuts.Shortcut, now time.Time) string {
	parts := []string{plural(s.ActionCount, "action"), relativeTime(s.Modified, now)}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func relativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

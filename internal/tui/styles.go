package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary = lipgloss.AdaptiveColor{Light: "#5A31F4", Dark: "#B39DFF"}
	colorText    = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#EEEEEE"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#9A9A9A"}
	colorFaint   = lipgloss.AdaptiveColor{Light: "#9A9A9A", Dark: "#6C6C6C"}
	colorGood    = lipgloss.AdaptiveColor{Light: "#127C4E", Dark: "#5DD39E"}
	colorWarn    = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E3B341"}
	colorBad     = lipgloss.AdaptiveColor{Light: "#B3261E", Dark: "#FF8A80"}
)

type styles struct {
	title      lipgloss.Style
	heading    lipgloss.Style
	notice     lipgloss.Style
	help       lipgloss.Style
	cursor     lipgloss.Style
	name       lipgloss.Style
	nameActive lipgloss.Style
	meta       lipgloss.Style
	marker     lipgloss.Style
	checked    lipgloss.Style
	unchecked  lipgloss.Style
	hidden     lipgloss.Style
	written    lipgloss.Style
	unchanged  lipgloss.Style
	failed     lipgloss.Style
	skipped    lipgloss.Style
	errorText  lipgloss.Style
	body       lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		title:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorPrimary).Padding(0, 1),
		heading:    lipgloss.NewStyle().Bold(true).Foreground(colorPrimary),
		notice:     lipgloss.NewStyle().Foreground(colorWarn),
		help:       lipgloss.NewStyle().Foreground(colorFaint),
		cursor:     lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
		name:       lipgloss.NewStyle().Foreground(colorText),
		nameActive: lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
		meta:       lipgloss.NewStyle().Foreground(colorMuted),
		marker:     lipgloss.NewStyle().Foreground(colorMuted),
		checked:    lipgloss.NewStyle().Foreground(colorGood).Bold(true),
		unchecked:  lipgloss.NewStyle().Foreground(colorFaint),
		hidden:     lipgloss.NewStyle().Foreground(colorFaint).Italic(true),
		written:    lipgloss.NewStyle().Foreground(colorGood),
		unchanged:  lipgloss.NewStyle().Foreground(colorMuted),
		failed:     lipgloss.NewStyle().Foreground(colorBad).Bold(true),
		skipped:    lipgloss.NewStyle().Foreground(colorFaint),
		errorText:  lipgloss.NewStyle().Foreground(colorBad),
		body:       lipgloss.NewStyle().Padding(0, 1),
	}
}

func (s styles) outcome(o Outcome) lipgloss.Style {
	switch o {
	case OutcomeWritten:
		return s.written
	case OutcomeUnchanged:
		return s.unchanged
	case OutcomeFailed:
		return s.failed
	}
	return s.skipped
}

func outcomeGlyph(o Outcome) string {
	switch o {
	case OutcomeWritten:
		return "+"
	case OutcomeUnchanged:
		return "="
	case OutcomeFailed:
		return "x"
	}
	return "-"
}

func cell(width int, content string) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}

func clampWidth(width int, content string) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(content)
}

package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Navigate  key.Binding
	Filter    key.Binding
	ClearFilt key.Binding
	Toggle    key.Binding
	ToggleAll key.Binding
	Clear     key.Binding
	Confirm   key.Binding
	Help      key.Binding
	Cancel    key.Binding
	Quit      key.Binding
	Close     key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑/↓", "navigate"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		ClearFilt: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear filter"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "select"),
		),
		ToggleAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "all/none shown"),
		),
		Clear: key.NewBinding(
			key.WithKeys("x", "n"),
			key.WithHelp("x", "clear selection"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "back up"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "more"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "cancel"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Close: key.NewBinding(
			key.WithKeys("q", "enter", "esc"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (k keyMap) selectShortHelp() []key.Binding {
	return []key.Binding{k.Navigate, k.Filter, k.Toggle, k.ToggleAll, k.Clear, k.Confirm, k.Help, k.Quit}
}

func (k keyMap) selectFullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Navigate, k.Filter, k.ClearFilt},
		{k.Toggle, k.ToggleAll, k.Clear},
		{k.Confirm, k.Help, k.Quit},
	}
}

func (k keyMap) progressShortHelp() []key.Binding {
	return []key.Binding{k.Cancel}
}

func (k keyMap) summaryShortHelp() []key.Binding {
	return []key.Binding{k.Close}
}

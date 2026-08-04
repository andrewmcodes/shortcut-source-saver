package tui

import (
	"context"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateLoading state = iota
	stateSelect
	stateProgress
	stateSummary
)

const (
	defaultWidth  = 80
	defaultHeight = 24
)

type Options struct {
	Title       string
	Destination string
	Preselect   []string
	SkipSelect  bool
	Context     context.Context
}

type Model struct {
	backend Backend
	opts    Options
	keys    keyMap
	styles  styles
	clock   func() time.Time

	state         state
	width, height int

	list      list.Model
	selection *selection
	help      help.Model
	bar       progress.Model

	library []shortcuts.Shortcut
	loadErr error
	notice  string

	queue     []shortcuts.Shortcut
	cursor    int
	results   []Result
	cancel    context.CancelFunc
	ctx       context.Context
	cancelled bool
}

type shortcutsLoadedMsg struct {
	shortcuts []shortcuts.Shortcut
	err       error
}

type backupResultMsg struct {
	index  int
	result Result
}

func New(b Backend, opts Options) *Model {
	if opts.Title == "" {
		opts.Title = "Shortcuts"
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}

	sel := newSelection()
	st := defaultStyles()
	clock := time.Now

	items := list.New(nil, itemDelegate{selection: sel, styles: st, clock: clock}, defaultWidth, defaultHeight-4)
	items.Title = opts.Title
	items.SetShowHelp(false)
	items.SetShowStatusBar(true)
	items.SetStatusBarItemName("shortcut", "shortcuts")
	items.SetFilteringEnabled(true)
	items.DisableQuitKeybindings()
	items.Styles.Title = st.title

	h := help.New()
	h.Styles.ShortKey = h.Styles.ShortKey.Foreground(colorPrimary)
	h.Styles.FullKey = h.Styles.FullKey.Foreground(colorPrimary)

	return &Model{
		backend:   b,
		opts:      opts,
		keys:      defaultKeyMap(),
		styles:    st,
		clock:     clock,
		state:     stateLoading,
		width:     defaultWidth,
		height:    defaultHeight,
		list:      items,
		selection: sel,
		help:      h,
		bar:       progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
	}
}

func Run(b Backend, opts Options) ([]Result, error) {
	final, err := tea.NewProgram(New(b, opts), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	m, ok := finalModel(final)
	if !ok {
		return nil, nil
	}
	return m.Results(), m.Err()
}

func finalModel(m tea.Model) (Model, bool) {
	switch v := m.(type) {
	case Model:
		return v, true
	case *Model:
		if v != nil {
			return *v, true
		}
	}
	return Model{}, false
}

func (m Model) Results() []Result {
	out := make([]Result, len(m.results))
	copy(out, m.results)
	return out
}

func (m Model) Err() error { return m.loadErr }

func (m Model) Init() tea.Cmd {
	return loadShortcuts(m.backend)
}

func loadShortcuts(b Backend) tea.Cmd {
	return func() tea.Msg {
		all, err := b.List()
		return shortcutsLoadedMsg{shortcuts: all, err: err}
	}
}

func backupOne(ctx context.Context, b Backend, s shortcuts.Shortcut, index int) tea.Cmd {
	return func() tea.Msg {
		if err := ctx.Err(); err != nil {
			return backupResultMsg{index: index, result: Result{Shortcut: s, Outcome: OutcomeSkipped, Detail: "cancelled"}}
		}
		return backupResultMsg{index: index, result: b.Backup(ctx, s)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.resizeList()
		m.bar.Width = barWidth(msg.Width)
		return m, nil

	case shortcutsLoadedMsg:
		return m.applyLibrary(msg)

	case backupResultMsg:
		return m.applyResult(msg)
	}

	switch m.state {
	case stateLoading:
		return m.updateLoading(msg)
	case stateSelect:
		return m.updateSelect(msg)
	case stateProgress:
		return m.updateProgress(msg)
	default:
		return m.updateSummary(msg)
	}
}

func (m Model) View() string {
	switch m.state {
	case stateLoading:
		return m.loadingView()
	case stateSelect:
		return m.selectView()
	case stateProgress:
		return m.progressView()
	default:
		return m.summaryView()
	}
}

func (m *Model) resizeList() {
	height := m.height - 1 - lipgloss.Height(m.selectHelpView())
	if height < 3 {
		height = 3
	}
	width := m.width
	if width < 1 {
		width = defaultWidth
	}
	m.list.SetSize(width, height)
}

func barWidth(width int) int {
	w := width - 4
	if w < 10 {
		w = 10
	}
	if w > 60 {
		w = 60
	}
	return w
}

func (m Model) applyLibrary(msg shortcutsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.loadErr = msg.err
		return m, nil
	}

	m.library = msg.shortcuts
	m.list.SetItems(itemsOf(msg.shortcuts))
	m.resizeList()

	preselected := preselectIDs(msg.shortcuts, m.opts.Preselect)
	for _, id := range preselected {
		m.selection.add(id)
	}

	if m.opts.SkipSelect {
		if len(preselected) == 0 {
			for _, s := range msg.shortcuts {
				m.selection.add(s.WorkflowID)
			}
		}
		return m.startBackup()
	}

	m.state = stateSelect
	return m, nil
}

func preselectIDs(all []shortcuts.Shortcut, wanted []string) []string {
	if len(wanted) == 0 {
		return nil
	}
	want := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		want[w] = true
	}
	var out []string
	for _, s := range all {
		if want[s.WorkflowID] || want[s.Name] {
			out = append(out, s.WorkflowID)
		}
	}
	return out
}

func (m Model) startBackup() (tea.Model, tea.Cmd) {
	m.queue = nil
	for _, s := range m.library {
		if m.selection.has(s.WorkflowID) {
			m.queue = append(m.queue, s)
		}
	}
	if len(m.queue) == 0 {
		m.state = stateSummary
		return m, nil
	}

	ctx, cancel := context.WithCancel(m.opts.Context)
	m.ctx, m.cancel = ctx, cancel
	m.cursor = 0
	m.results = nil
	m.cancelled = false
	m.notice = ""
	m.state = stateProgress
	m.bar.Width = barWidth(m.width)

	return m, backupOne(ctx, m.backend, m.queue[0], 0)
}

func (m Model) applyResult(msg backupResultMsg) (tea.Model, tea.Cmd) {
	if m.state != stateProgress || msg.index != m.cursor {
		return m, nil
	}

	m.results = append(m.results, msg.result)
	m.cursor++

	if m.cursor >= len(m.queue) {
		return m.finish(), nil
	}
	return m, backupOne(m.ctx, m.backend, m.queue[m.cursor], m.cursor)
}

func (m Model) finish() Model {
	if m.cancel != nil {
		m.cancel()
	}
	m.state = stateSummary
	return m
}

func (m Model) cancelBackup() Model {
	if m.cancel != nil {
		m.cancel()
	}
	m.cancelled = true
	for i := m.cursor; i < len(m.queue); i++ {
		m.results = append(m.results, Result{Shortcut: m.queue[i], Outcome: OutcomeSkipped, Detail: "cancelled"})
	}
	m.cursor = len(m.queue)
	m.state = stateSummary
	return m
}

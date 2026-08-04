package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var fixedNow = time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)

func fixtures() []shortcuts.Shortcut {
	return []shortcuts.Shortcut{
		{Name: "Alpha Notes", WorkflowID: "id-alpha", ActionCount: 3, Modified: fixedNow.Add(-2 * time.Hour), Icon: shortcuts.Icon{Color: 0xFF000000}},
		{Name: "Beta 🚀 Deploy", WorkflowID: "id-beta", ActionCount: 12, Modified: fixedNow.Add(-48 * time.Hour)},
		{Name: "Duplicate", WorkflowID: "id-dup-1", ActionCount: 1, Modified: fixedNow.Add(-10 * time.Minute)},
		{Name: "Duplicate", WorkflowID: "id-dup-2", ActionCount: 7, Modified: fixedNow.Add(-90 * 24 * time.Hour)},
		{Name: "Zeta Secret", WorkflowID: "id-zeta", ActionCount: 4, Modified: fixedNow, Hidden: true},
	}
}

type fakeBackend struct {
	items    []shortcuts.Shortcut
	listErr  error
	outcomes map[string]Result
	calls    []string
	ctxSeen  []error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{items: fixtures(), outcomes: map[string]Result{}}
}

func (f *fakeBackend) List() ([]shortcuts.Shortcut, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

func (f *fakeBackend) Backup(ctx context.Context, s shortcuts.Shortcut) Result {
	f.calls = append(f.calls, s.WorkflowID)
	f.ctxSeen = append(f.ctxSeen, ctx.Err())
	if r, ok := f.outcomes[s.WorkflowID]; ok {
		r.Shortcut = s
		return r
	}
	return Result{Shortcut: s, Outcome: OutcomeWritten, Detail: "shortcuts/" + s.WorkflowID + ".json"}
}

type harness struct {
	t     *testing.T
	model tea.Model
	quit  bool
}

func newHarness(t *testing.T, b Backend, opts Options) *harness {
	t.Helper()
	m := New(b, opts)
	m.clock = func() time.Time { return fixedNow }
	m.list.SetDelegate(itemDelegate{selection: m.selection, styles: m.styles, clock: m.clock})
	h := &harness{t: t, model: m}
	h.run(m.Init(), 0)
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.model.Update(msg)
	h.model = next
	h.run(cmd, 0)
}

func (h *harness) run(cmd tea.Cmd, depth int) {
	h.t.Helper()
	if cmd == nil || depth > 200 {
		return
	}
	msg, ok := runWithin(cmd, 100*time.Millisecond)
	if !ok {
		return
	}
	switch typed := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range typed {
			h.run(c, depth+1)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		if strings.Contains(fmt.Sprintf("%T", msg), "linkMsg") {
			return
		}
		next, follow := h.model.Update(msg)
		h.model = next
		h.run(follow, depth+1)
	}
}

func runWithin(cmd tea.Cmd, limit time.Duration) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(limit):
		return nil, false
	}
}

func (h *harness) keys(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyMsg(k))
	}
}

func (h *harness) state() Model {
	h.t.Helper()
	m, ok := finalModel(h.model)
	if !ok {
		h.t.Fatalf("model is not a tui.Model: %T", h.model)
	}
	return m
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func (h *harness) typeFilter(term string) {
	h.t.Helper()
	h.keys("/")
	for _, r := range term {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	h.keys("enter")
}

func selectedIDs(m Model) []string {
	out := make([]string, 0, m.selection.count())
	for id := range m.selection.ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLoadPopulatesSelectScreen(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})
	m := h.state()

	if m.state != stateSelect {
		t.Fatalf("state = %v, want stateSelect", m.state)
	}
	if got := len(m.list.Items()); got != len(fixtures()) {
		t.Fatalf("list has %d items, want %d", got, len(fixtures()))
	}

	view := m.View()
	for _, want := range []string{"Alpha Notes", "Beta 🚀 Deploy", "hidden", "0 selected"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestLoadErrorIsSurfaced(t *testing.T) {
	b := newFakeBackend()
	b.listErr = errors.New("no full disk access")

	h := newHarness(t, b, Options{})
	m := h.state()

	if m.Err() == nil {
		t.Fatal("Err() = nil, want the list error")
	}
	if !strings.Contains(m.View(), "no full disk access") {
		t.Errorf("view missing the error:\n%s", m.View())
	}

	h.keys("q")
	if !h.quit {
		t.Error("q did not quit after a load error")
	}
}

func TestSelectAllOnlySelectsVisibleMatches(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})

	h.typeFilter("dup")
	if got := len(h.state().list.VisibleItems()); got != 2 {
		t.Fatalf("filter matched %d items, want 2", got)
	}

	h.keys("a")
	if got, want := selectedIDs(h.state()), []string{"id-dup-1", "id-dup-2"}; !equalIDs(got, want) {
		t.Fatalf("selection = %v, want %v", got, want)
	}

	h.keys("esc")
	if got := len(h.state().list.VisibleItems()); got != len(fixtures()) {
		t.Fatalf("clearing the filter left %d visible items", got)
	}
	if got, want := selectedIDs(h.state()), []string{"id-dup-1", "id-dup-2"}; !equalIDs(got, want) {
		t.Fatalf("selection changed after clearing the filter: %v, want %v", got, want)
	}
}

func TestSelectAllTwiceClearsVisibleMatches(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})

	h.keys("a")
	if got := h.state().selection.count(); got != len(fixtures()) {
		t.Fatalf("selected %d, want %d", got, len(fixtures()))
	}
	h.keys("a")
	if got := h.state().selection.count(); got != 0 {
		t.Fatalf("selected %d after toggling all off, want 0", got)
	}
}

func TestToggleIsKeyedOnWorkflowIDAndSurvivesFilterChange(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})

	h.typeFilter("dup")
	h.keys("space")
	if got, want := selectedIDs(h.state()), []string{"id-dup-1"}; !equalIDs(got, want) {
		t.Fatalf("selection = %v, want %v", got, want)
	}

	h.keys("esc")
	h.keys("space")
	if got, want := selectedIDs(h.state()), []string{"id-alpha", "id-dup-1"}; !equalIDs(got, want) {
		t.Fatalf("selection = %v, want %v", got, want)
	}

	if !strings.Contains(h.state().View(), "2 selected") {
		t.Errorf("view does not report 2 selected:\n%s", h.state().View())
	}
}

func TestClearSelectionKeys(t *testing.T) {
	for _, k := range []string{"x", "n"} {
		h := newHarness(t, newFakeBackend(), Options{})
		h.keys("a")
		h.keys(k)
		if got := h.state().selection.count(); got != 0 {
			t.Errorf("%q left %d selected, want 0", k, got)
		}
	}
}

func TestFilterKeysDoNotToggleSelection(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})

	h.keys("/")
	h.keys("a", "space", "x", "n")

	m := h.state()
	if got := m.selection.count(); got != 0 {
		t.Fatalf("typing a filter selected %d items", got)
	}
	if got := m.list.FilterValue(); got != "a xn" {
		t.Fatalf("filter value = %q, want %q", got, "a xn")
	}
}

func TestEnterWithEmptySelectionDoesNotAdvance(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})

	h.keys("enter")
	m := h.state()

	if m.state != stateSelect {
		t.Fatalf("state = %v, want stateSelect", m.state)
	}
	if m.notice == "" {
		t.Error("expected a notice explaining that nothing is selected")
	}
	if !strings.Contains(m.View(), "Nothing selected") {
		t.Errorf("view missing the notice:\n%s", m.View())
	}
}

func TestBackupRunsSequentiallyThenSummarises(t *testing.T) {
	b := newFakeBackend()
	h := newHarness(t, b, Options{Destination: "/tmp/repo"})

	h.keys("a", "enter")
	m := h.state()

	if m.state != stateSummary {
		t.Fatalf("state = %v, want stateSummary", m.state)
	}
	if got, want := len(b.calls), len(fixtures()); got != want {
		t.Fatalf("backend saw %d calls, want %d", got, want)
	}
	for i, s := range fixtures() {
		if b.calls[i] != s.WorkflowID {
			t.Fatalf("call %d = %q, want %q (library order)", i, b.calls[i], s.WorkflowID)
		}
	}
	if got, want := len(m.Results()), len(fixtures()); got != want {
		t.Fatalf("results = %d, want %d", got, want)
	}
	if HasFailures(m.Results()) {
		t.Error("HasFailures = true, want false")
	}

	view := m.View()
	for _, want := range []string{"Backup complete", "5 written", "/tmp/repo"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q:\n%s", want, view)
		}
	}

	h.keys("enter")
	if !h.quit {
		t.Error("enter on the summary did not quit")
	}
}

func TestProgressScreenIsShownBetweenResults(t *testing.T) {
	b := newFakeBackend()
	h := newHarness(t, b, Options{})
	h.keys("a")

	m := h.state()
	m.selection.clear()
	m.selection.add("id-alpha")
	m.selection.add("id-beta")

	next, _ := m.startBackup()
	mid, _ := finalModel(next)
	if mid.state != stateProgress {
		t.Fatalf("state = %v, want stateProgress", mid.state)
	}

	view := mid.progressView()
	for _, want := range []string{"Backing up 1 of 2", "Alpha Notes"} {
		if !strings.Contains(view, want) {
			t.Errorf("progress view missing %q:\n%s", want, view)
		}
	}

	after, _ := mid.Update(backupResultMsg{index: 0, result: Result{Shortcut: fixtures()[0], Outcome: OutcomeUnchanged}})
	m2, _ := finalModel(after)
	if m2.state != stateProgress {
		t.Fatalf("state = %v, want stateProgress after the first result", m2.state)
	}
	if !strings.Contains(m2.progressView(), "Backing up 2 of 2") {
		t.Errorf("progress view did not advance:\n%s", m2.progressView())
	}
	if !strings.Contains(m2.progressView(), "Alpha Notes") {
		t.Errorf("progress log missing the completed item:\n%s", m2.progressView())
	}

	after2, _ := m2.Update(backupResultMsg{index: 1, result: Result{Shortcut: fixtures()[1], Outcome: OutcomeWritten}})
	m3, _ := finalModel(after2)
	if m3.state != stateSummary {
		t.Fatalf("state = %v, want stateSummary after the last result", m3.state)
	}
	if got := len(m3.Results()); got != 2 {
		t.Fatalf("results = %d, want 2", got)
	}
}

func TestStaleResultsAreIgnored(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})
	h.keys("a")
	next, _ := h.state().startBackup()
	m, _ := finalModel(next)

	updated, _ := m.Update(backupResultMsg{index: 3, result: Result{Shortcut: fixtures()[3], Outcome: OutcomeWritten}})
	m2, _ := finalModel(updated)
	if got := len(m2.Results()); got != 0 {
		t.Fatalf("out-of-order result was accepted: %d results", got)
	}
}

func TestFailureAppearsInSummary(t *testing.T) {
	b := newFakeBackend()
	b.outcomes["id-beta"] = Result{Outcome: OutcomeFailed, Err: errors.New("git commit failed")}
	b.outcomes["id-dup-2"] = Result{Outcome: OutcomeUnchanged}

	h := newHarness(t, b, Options{})
	h.keys("a", "enter")
	m := h.state()

	if m.state != stateSummary {
		t.Fatalf("state = %v, want stateSummary", m.state)
	}
	if !HasFailures(m.Results()) {
		t.Fatal("HasFailures = false, want true")
	}

	view := m.View()
	for _, want := range []string{"1 failed", "1 unchanged", "Failures", "Beta 🚀 Deploy", "git commit failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q:\n%s", want, view)
		}
	}
}

func TestCancelDuringProgressGoesToSummary(t *testing.T) {
	b := newFakeBackend()
	h := newHarness(t, b, Options{})
	h.keys("a")

	next, pending := h.state().startBackup()
	running, _ := finalModel(next)

	cancelled, _ := running.Update(keyMsg("ctrl+c"))
	m, _ := finalModel(cancelled)

	if m.state != stateSummary {
		t.Fatalf("state = %v, want stateSummary", m.state)
	}
	if !m.cancelled {
		t.Error("cancelled = false, want true")
	}
	if got, want := len(m.Results()), len(fixtures()); got != want {
		t.Fatalf("results = %d, want %d skipped entries", got, want)
	}
	for _, r := range m.Results() {
		if r.Outcome != OutcomeSkipped {
			t.Errorf("%s outcome = %v, want skipped", r.Shortcut.Name, r.Outcome)
		}
	}
	if !strings.Contains(m.View(), "Backup cancelled") {
		t.Errorf("summary does not mention cancellation:\n%s", m.View())
	}

	msg := pending()
	if len(b.calls) != 0 {
		t.Fatalf("backend was called after cancellation: %v", b.calls)
	}
	result, ok := msg.(backupResultMsg)
	if !ok {
		t.Fatalf("pending command returned %T, want backupResultMsg", msg)
	}
	if result.result.Outcome != OutcomeSkipped {
		t.Errorf("in-flight item outcome = %v, want skipped", result.result.Outcome)
	}

	late, _ := m.Update(msg)
	m2, _ := finalModel(late)
	if got, want := len(m2.Results()), len(fixtures()); got != want {
		t.Fatalf("a late result was appended: %d results, want %d", got, want)
	}
}

func TestSkipSelectStartsBackupImmediately(t *testing.T) {
	b := newFakeBackend()
	h := newHarness(t, b, Options{SkipSelect: true})

	m := h.state()
	if m.state != stateSummary {
		t.Fatalf("state = %v, want stateSummary once every item is done", m.state)
	}
	if got, want := len(b.calls), len(fixtures()); got != want {
		t.Fatalf("backend saw %d calls, want %d", got, want)
	}
}

func TestPreselectionByNameAndID(t *testing.T) {
	b := newFakeBackend()
	h := newHarness(t, b, Options{Preselect: []string{"Duplicate", "id-zeta"}})

	if got, want := selectedIDs(h.state()), []string{"id-dup-1", "id-dup-2", "id-zeta"}; !equalIDs(got, want) {
		t.Fatalf("selection = %v, want %v", got, want)
	}
}

func TestSkipSelectWithPreselectionOnlyBacksUpThose(t *testing.T) {
	b := newFakeBackend()
	newHarness(t, b, Options{SkipSelect: true, Preselect: []string{"id-zeta"}})

	if got, want := b.calls, []string{"id-zeta"}; !equalIDs(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestNarrowTerminalDoesNotOverflow(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})
	h.send(tea.WindowSizeMsg{Width: 28, Height: 12})
	h.keys("a")

	m := h.state()
	for _, name := range []string{"select", "progress", "summary"} {
		var view string
		switch name {
		case "select":
			view = m.selectView()
		case "progress":
			next, _ := m.startBackup()
			p, _ := finalModel(next)
			view = p.progressView()
		default:
			next, _ := m.startBackup()
			p, _ := finalModel(next)
			view = p.cancelBackup().summaryView()
		}
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > 28 {
				t.Errorf("%s view line %d is %d cells wide, want <= 28: %q", name, i, w, line)
			}
		}
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		modified time.Time
		want     string
	}{
		{time.Time{}, "never"},
		{fixedNow, "just now"},
		{fixedNow.Add(-30 * time.Second), "just now"},
		{fixedNow.Add(-5 * time.Minute), "5m ago"},
		{fixedNow.Add(-3 * time.Hour), "3h ago"},
		{fixedNow.Add(-72 * time.Hour), "3d ago"},
		{fixedNow.Add(-60 * 24 * time.Hour), "2mo ago"},
		{fixedNow.Add(-800 * 24 * time.Hour), "2y ago"},
		{fixedNow.Add(time.Hour), "just now"},
	}
	for _, tt := range tests {
		if got := relativeTime(tt.modified, fixedNow); got != tt.want {
			t.Errorf("relativeTime(%v) = %q, want %q", tt.modified, got, tt.want)
		}
	}
}

func TestOutcomeString(t *testing.T) {
	for outcome, want := range map[Outcome]string{
		OutcomeWritten:   "written",
		OutcomeUnchanged: "unchanged",
		OutcomeFailed:    "failed",
		OutcomeSkipped:   "skipped",
	} {
		if got := outcome.String(); got != want {
			t.Errorf("Outcome(%d).String() = %q, want %q", outcome, got, want)
		}
	}
}

func TestHelpLineListsKeyBindings(t *testing.T) {
	h := newHarness(t, newFakeBackend(), Options{})
	view := h.state().selectView()

	for _, want := range []string{"space", "filter", "clear selection", "back up"} {
		if !strings.Contains(view, want) {
			t.Errorf("help line missing %q:\n%s", want, view)
		}
	}
}

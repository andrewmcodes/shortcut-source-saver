# Terminal UI

This document is the reference for `internal/tui`: the three screens, the keymap, the `Backend` seam the backup work arrives through, and the reasoning behind the decisions that are not obvious from the code.

`sss` with no arguments lands here. The TUI shows the Shortcuts library, lets you pick what to back up, then shows progress while each shortcut is written.

## API

```go
type Backend interface {
	List() ([]shortcuts.Shortcut, error)
	Backup(ctx context.Context, s shortcuts.Shortcut) Result
}

type Result struct {
	Shortcut shortcuts.Shortcut
	Outcome  Outcome // OutcomeWritten | OutcomeUnchanged | OutcomeFailed | OutcomeSkipped
	Detail   string
	Err      error
}

type Options struct {
	Title       string   // header text; defaults to "Shortcuts"
	Destination string   // shown in the summary hint, e.g. the repo path
	Preselect   []string // workflow IDs or exact names to start selected
	SkipSelect  bool     // jump straight to Progress
	Context     context.Context
}

func New(b Backend, opts Options) *Model            // Model implements tea.Model
func Run(b Backend, opts Options) ([]Result, error) // runs the program, returns the final results
func HasFailures(results []Result) bool             // for the caller's exit code
```

`Model.Results()` and `Model.Err()` expose the same information when the caller drives `tea.Program` itself. `Run` returns the results even when the run was cancelled, so `sss` can exit non-zero on `HasFailures` without re-deriving anything.

`SkipSelect` with an empty `Preselect` means "everything", which is what `sss backup --all` wants: the same progress and summary UI with no picker in front of it. `SkipSelect` with a `Preselect` backs up exactly those shortcuts.

## Screens

One `Model` holds all three screens and a state enum (`stateLoading`, `stateSelect`, `stateProgress`, `stateSummary`). A single model keeps the selection, the results, and the terminal size in one place; splitting them into three `tea.Model`s would mean handing that state back and forth on every transition.

### Loading

`Init` calls `Backend.List` in a `tea.Cmd`. Reading the Shortcuts database means copying a SQLite file and its WAL sidecars, which is slow enough to notice, so it must not happen inside `Update`. A failure here is terminal for the run: the error is rendered in full (it is usually the Full Disk Access message, which is actionable) and any key quits. The error is also returned from `Run`.

### Select

Every shortcut is one row:

```
❯ [x] ██ Beta 🚀 Deploy                        12 actions · 2d ago
  [ ] ██ Zeta Secret                hidden · 4 actions · just now
```

The colour block is tinted with the shortcut's own icon colour via `archive.HexColor`, so the list reads like the Shortcuts app rather than a wall of text. Hidden shortcuts (`ZHIDDENFROMLIBRARYANDSYNC`) are labelled, because otherwise a shortcut you cannot see in the app appears in the backup with no explanation.

Filtering, pagination, and the status bar come from `bubbles/list`. `list` already does fuzzy filtering with match ranking, an input line, and correct pagination for a ~189-item library; reimplementing that would only add bugs. The custom `itemDelegate` exists purely to render the row — a checkbox, colour swatch, name, and right-aligned metadata — which the stock `DefaultDelegate` cannot do.

Rows are laid out by computing widths from `list.Model.Width()` on every render, so a `tea.WindowSizeMsg` needs no bookkeeping. The name is rendered into a fixed-width cell and the whole row is clipped with lipgloss, which measures grapheme width, so emoji and other wide runes cannot push the row past the terminal edge. When the terminal is too narrow to fit the metadata, the metadata is dropped rather than squeezed, and when it is narrow enough that even the name does not fit, the name alone is clipped. All colours are `lipgloss.AdaptiveColor`, so the UI is legible on light and dark terminals.

### Progress

The queue is the selected shortcuts in library order. One shortcut is backed up per `tea.Cmd`; each `backupResultMsg` appends to the log and issues the command for the next item. So the update loop is never blocked — the UI keeps redrawing, and cancellation is handled between items — while the work itself stays strictly sequential.

Sequential is not an incidental choice. `Backend.Backup` writes files into a git repository and may commit; running two of those concurrently would interleave writes and index operations on shared state. Fan-out would buy nothing anyway, since the cost is dominated by disk and git rather than by anything parallelisable.

Results carry a `Detail` string used as the per-item log line (typically the path written), and `Err` for failures. The log shows the tail that fits the terminal height with an `… n earlier` marker above it, so a 189-item run does not need a scrollback buffer to stay readable.

The progress bar is rendered with `progress.Model.ViewAs`, which draws a given fraction directly instead of animating towards it. There is no spring animation to tick, so the model needs no timers at all: every frame is a pure function of the results received so far, which is also why the tests are deterministic.

A `backupResultMsg` whose index is not the item currently in flight is dropped. That is what makes a cancelled run's in-flight result harmless when it lands after the model has already moved to Summary.

### Summary

Counts per outcome, then every failure with its error, then a hint naming what was written and where (`Options.Destination`). <kbd>q</kbd>, <kbd>enter</kbd>, or <kbd>esc</kbd> exits.

The summary is a screen rather than a print-and-quit because the failures need to be readable next to the counts; the CLI still gets the machine-readable version through the returned `[]Result`.

## Keymap

`bubbles/help` renders the bindings from `key.Binding` values, so the help line and the keymap cannot drift apart.

| Screen | Key | Action |
| --- | --- | --- |
| Select | <kbd>↑</kbd>/<kbd>↓</kbd>, <kbd>k</kbd>/<kbd>j</kbd> | Move the cursor |
| Select | <kbd>←</kbd>/<kbd>→</kbd>, <kbd>pgup</kbd>/<kbd>pgdn</kbd>, <kbd>g</kbd>/<kbd>G</kbd> | Page and jump (from `bubbles/list`) |
| Select | <kbd>/</kbd> | Filter by name |
| Select | <kbd>esc</kbd> | Clear the filter |
| Select | <kbd>space</kbd> | Toggle the highlighted shortcut |
| Select | <kbd>a</kbd> | Select all shown, or deselect them if all are already selected |
| Select | <kbd>x</kbd> / <kbd>n</kbd> | Clear the whole selection |
| Select | <kbd>enter</kbd> | Start the backup |
| Select | <kbd>?</kbd> | Expand the help |
| Select | <kbd>q</kbd> / <kbd>ctrl+c</kbd> | Quit |
| Progress | <kbd>ctrl+c</kbd> | Cancel |
| Summary | <kbd>q</kbd> / <kbd>enter</kbd> / <kbd>esc</kbd> | Quit |

While the filter input is focused, every key except <kbd>ctrl+c</kbd> goes to the list. Otherwise typing a name containing `a`, `x`, or a space would silently change the selection instead of the filter.

<kbd>enter</kbd> with nothing selected does not advance. It shows a one-line notice naming the keys that select something, which is cheaper than a Progress screen with an empty queue.

## Why selection is keyed on `WorkflowID`

The selection is a set of workflow IDs, not names and not list indexes.

Shortcut names are not unique — the Shortcuts app happily holds two shortcuts called `Duplicate`, and `shortcuts.Library.FindByName` returns a slice for that reason — so a name-keyed set would select or skip the wrong one. Indexes are worse: `bubbles/list` renumbers visible items as the filter changes, so an index recorded before a keystroke refers to a different shortcut after it. The workflow ID is the database's own stable identity for the shortcut and survives filtering, sorting, and renaming.

Because the ID set is shared between the model and the row delegate through a pointer, checkbox state, the header count, and the backup queue are all reading one source of truth rather than three copies that could disagree.

## Why select-all respects the active filter

<kbd>a</kbd> operates on `list.VisibleItems()`, which is the filtered set when a filter is applied.

Filtering to `deploy` and pressing "all" means "all of these", not "all 189 shortcuts including the ones I just filtered away" — the second reading would make the key dangerous with no undo. The same key toggles: when every visible row is already selected it deselects the visible rows, which makes "select these, filter again, add those" a natural sequence. Rows selected under a previous filter stay selected, since the selection is keyed on ID and nothing about clearing a filter implies discarding a choice. The header count is always the full selection, so a filtered view can never hide how much is about to be backed up.

## Why the work arrives through `Backend`

`internal/tui` does not import the backup pipeline. It takes a `Backend` that lists shortcuts and backs one up.

That keeps the dependency pointing one way: the CLI wires the archive, git, and config packages together and hands the TUI a closure over them. The TUI cannot deadlock on, or accidentally reorder, work it does not own, and the same UI drives `sss` with no arguments, `sss backup --all`, and any future subcommand that wants a progress display. It also makes the package testable without a Shortcuts database, a git repo, or a TTY: the tests pass a fake `Backend` returning canned shortcuts and canned outcomes.

Returning a `Result` value rather than an `error` is deliberate. "Unchanged" is a normal, common outcome for a backup that runs repeatedly, and it is not an error; collapsing it into `error == nil` would lose the distinction the summary is built on.

## Cancellation

Progress creates a `context.WithCancel` from `Options.Context`, and each `Backend.Backup` call gets that context.

<kbd>ctrl+c</kbd> during Progress cancels it and moves straight to Summary. The item in flight and everything still queued are recorded as `OutcomeSkipped` with detail `cancelled`, so the counts always add up to the size of the queue and the summary can say what actually completed. The command already in flight is not waited on: its `Result` arrives later and is dropped by the stale-index check. Each queued command also re-checks `ctx.Err()` before calling into the backend, so a cancelled run performs no further file or git work even if a command was already scheduled.

`Options.Context` lets the CLI hand in a context wired to signal handling, so a `SIGINT` delivered outside the Bubble Tea input loop cancels the same work.

## Tests

`internal/tui/tui_test.go` drives the model directly — no TTY, no `tea.Program`. A small harness sends `tea.KeyMsg`, `tea.WindowSizeMsg`, and result messages into `Update`, runs the returned commands, and feeds their messages back in, which is exactly what the runtime does. Commands that do not return promptly are abandoned by the harness, because the filter input's cursor blink is a timer that would otherwise add half a second per keystroke.

Covered: select-all under a filter touches only the visible matches; <kbd>space</kbd> toggles by ID and the selection survives clearing the filter; filter keystrokes do not mutate the selection; <kbd>enter</kbd> with nothing selected does not advance; the backup calls the backend once per selected shortcut in library order; Progress advances to Summary on the last result; a failing item and its error reach the summary; cancellation marks the remainder skipped, keeps the backend from being called again, and drops the late result; out-of-order results are ignored; preselection by name and by ID; `SkipSelect`; and that no rendered line exceeds a 28-column terminal.

# How a shortcut is rendered

This document explains `internal/render`, the package that turns a `shortcuts.Workflow` into the three human-readable artefacts: the per-shortcut web review, the per-shortcut `README.md`, and the GitHub Pages site. It is the counterpart to [archive.md](archive.md): that document covers the machine-readable files, this one covers the ones a person reads.

The structure of the web review — the collapsible per-action buttons, the colour groups, the class names on the action markup — is taken from gluebyte's Shortcut Source Tool, a Shortcut that renders a shortcut as an HTML page. Its logic was extracted and reimplemented in Go, and the class names (`button`, `content`, `contentloop`, `nonbottom`, `gYellow`, …) are kept so the two can still be compared side by side. The visual design is not the reference's: it follows the Shortcut Atlas mockup, described in [The site design](#the-site-design) below.

## The API

| function | returns |
| --- | --- |
| `WebReview(w)` / `WebReviewWithOptions(w, opts)` | one self-contained HTML page (CSS inlined, no external requests, no images) |
| `ShortcutREADME(w)` / `ShortcutREADMEWithOptions(w, opts)` | Markdown for `shortcuts/<slug>/README.md` |
| `Site(entries, opts)` | `map[relative path][]byte` — `index.html`, `shortcuts/<slug>.html` per entry, `assets/style.css` |
| `Stylesheet()` | the concatenated stylesheet (`assets/radix-colors.css`, `assets/site.css`, `assets/actions.css`, in that order), for callers that write it themselves |

`Options.ExpandAll` disables the collapse-large-content heuristic. `Options.SiteURL` switches the README's review link from the sibling `review.html` to the hosted page. `Options.ShortcutFile` names the importable binary the README links to (default `shortcut.shortcut`), because the caller chooses that name. `SiteOptions.BaseURL` is the prefix for a GitHub Pages *project* site (`/repo-name`); every internal link is built through it, and when it is empty the site is linked relatively (`../assets/style.css`) so it also works from a `file://` checkout.

Everything is deterministic: maps are always walked in sorted order, so two runs over the same workflow produce byte-identical output and a git diff means something changed in the shortcut.

## Text is never concatenated into markup

Every string that comes out of a shortcut — action names, parameter values, variable names, prompts, dictionary keys — is untrusted. Shortcuts routinely contain `<`, `&`, quotes, and whole HTML documents as literal text.

So the renderer never builds HTML by string concatenation. It builds a tree of `node` values (`node.go`) and hands the tree to `html/template`, which escapes each leaf in its context:

| kind | rendered as |
| --- | --- |
| `text` | escaped text |
| `escape` | `<b>\uXXXX</b>` — a visible stand-in for an invisible character |
| `box` | `<span class="box">…</span>`, a parameter value inline in a title |
| `var` / `magic` / `flag` | a variable, a clickable magic variable, a flagged (broken) reference |
| `bold` / `italic` / `break` | `<b>` / `<i>` / `<br>` |

The two places where trusted-but-structured text has to reach the output are handled with the types `html/template` provides for exactly that, rather than by disabling escaping: the stylesheet is inlined as `template.CSS`, and the icon colour reaches `style` attributes as `template.CSS` — as `background-color` on the swatch and as the `--icon` custom property on the shortcut tile.

Magic-variable links carry their target in `data-target` and are wired up by a listener in the page's static script. There is no interpolation into a JavaScript context anywhere, which both avoids `html/template`'s JS-context padding of numbers and keeps the markup readable.

### Invisible characters are made visible

Invisible characters in shortcut text are a real debugging problem: a zero-width space inside a variable name or a non-breaking space in a URL is invisible in the Shortcuts editor and in a browser. `visibleText` replaces control characters, zero-width and bidi marks, the various exotic spaces (including U+3000 and U+2800) and U+FEFF with a bold `\uXXXX` marker, and turns *trailing* whitespace at the end of a line into `\u{space}` / `\u{tab}` markers, since only trailing runs are ambiguous.

The separators the renderer itself emits (the `　` before `➔`, `»` and `▵`) are U+3000 and are written as plain text nodes, not passed through `visibleText` — otherwise the renderer would escape its own punctuation.

This visualisation is **HTML-only**. In the README a `\u{space}` reads as corruption rather than as a hint, so the Markdown walker (`markdownText`) renders each escape node from the original character it stands for instead of from its label: whitespace-like characters become a plain space, which the summary's whitespace collapsing then trims away, and zero-width, bidi and control characters are dropped entirely. Each escape node therefore carries both its label and its `Raw` original, and the choice of which to use belongs to the output format, not to the escaping pass.

## Control flow nests as real DOM nesting

`WFControlFlowMode` in an action's parameters says what a control-flow action does: `0` opens a block, `1` starts another branch of the open block (Otherwise, Otherwise If, a Menu Item), `2` closes it. Blocks are matched by `GroupingIdentifier`, not by position, and an action is only control flow at all if the catalog has more than one mode for its identifier (`catalog.Action.ControlFlow`).

`flow.go` walks the actions once with a LIFO stack of open blocks. Instead of emitting `<div>` and `</div>` strings as it goes, it builds a **tree** of `item` values: the actions inside a block become the `Children` of the item that opened it, and a recursive template emits `<button>` + `<div>` + children + `</div>`. Balanced HTML is therefore structural, not something the state machine has to get right.

| mode | stack | tree |
| --- | --- | --- |
| `0` | push `{identifier, grouping, opener}` | append an item to the current level; descend into its `Children` |
| `1`, matching top of stack | keep the frame, move its `opener` to this index | append a *sibling* item at the parent level; descend into the new item's `Children` |
| `2`, matching top of stack | pop | append a leaf item at the parent level |

The depth of an item is its number of ancestors, and it drives three cues, all applied in `finalize`:

- the left border style of the content div cycles `["", " dotted", " dashed"][depth % 3]`, so three nesting levels are distinguishable without indentation eating the page width;
- anything below the top level gets the `inside` class, which removes the right border and rounds only the left corners;
- openers get `nonbottom`, branches get `nonbottom nontop` and closers get `nontop`, so a block reads as one continuous card rather than a stack of separate ones.

A control-flow action whose block has no rows and no children (an `If` with an empty branch) is emitted as a bare button with no content div, matching the reference's post-processing pass, so empty branches do not leave an empty box behind.

### Unbalanced control flow is flagged, never propagated

Unbalanced blocks occur in the wild — Shortcuts itself will happily store them, and a shortcut that was edited by a script can easily lose an `End If`. Three cases are handled:

1. **A mode 1/2 action with nothing open, or whose `GroupingIdentifier` does not match the innermost open block.** The stack is left untouched and the action is rendered as a flagged leaf (`(corrupted block)` in red) at the current level. If that grouping identifier was already closed earlier, the flag links to the action that closed it, so it is obvious that the block was closed twice.
2. **A mode 1/2 action whose grouping matches but whose identifier does not** — an `End Repeat` closing an `If`. The block *is* closed (the grouping identifier is authoritative) and the action is flagged.
3. **Blocks still open at the end of the workflow.** Nothing to do structurally, because the tree closes them; the header records which action opened the block that never closes.

In every case the review grows a notice above the actions with a red `corrupted at action N` link to the offending action, a `Flagged` marker in the heading's metadata line and red rows in the Details panel, and the README grows a warning callout. Rendering always continues, and the page is always well-formed — the golden tests assert both, including tag balance.

### Values of the wrong type are flagged, not coerced

`WFControlFlowMode` and `WFCondition` are integers in every real export, but hand-written and generated plists get them wrong — storing `<string>0</string>` for the mode, or a word like `"Equals"` for the condition, is the most common mistake in generated shortcuts. Treating a missing integer as `0` would silently render a string `"2"` (End If) as a second opener and an `"Equals"` comparison as `<`.

`controlFlowModeOf` and `conditionMalformed` (`flow.go`) catch both before rendering:

- a mode string that parses as an integer (`"0"`, `"2"`) is used for nesting, so the structure the author meant still renders, and the action is flagged;
- a mode that does not parse (`"start"`) makes the action a flagged leaf, since there is no way to know which block it belongs to;
- a non-integer `WFCondition` is shown verbatim in a flagged box in the condition, and the action is flagged.

The reasons for one action are joined, the first malformed action gets a `Malformed value` row and the `corrupted` link in the header, and the README gets its own warning callout and a `— **malformed value**` marker in the outline. A numeric condition code the catalog does not know renders as `operator <code>` rather than as an empty box.

## Magic variables are cross-referenced by UUID

Before rendering anything, `newReviewer` builds `map[UUID]int` from every action's `WFWorkflowActionParameters.UUID` to that action's index. This has to happen first because a reference can point forwards (a `Repeat` body can consume an output produced later in the source order of a menu).

A variable reaches the renderer in more than one shape, and the shapes nest: a parameter can hold `{"WFSerializationType": "WFTextTokenAttachment", "Value": {…}}`, or `{"Type": "Variable", "Variable": {…}}` wrapping one of those, and real shortcuts contain both — sometimes doubled. `unwrapVariable` peels those wrappers in a loop (attachment → `Value`; a `Variable`-typed map with no `VariableName` → `Variable`) up to a depth cap, so the rendering code only ever sees the innermost descriptor. Missing this unwrapping is silent: the variable renders as an empty `[]` rather than failing.

Once unwrapped, a descriptor whose `Type` is:

- `ActionOutput` renders as `[<producer index> <OutputName>]`, clickable: the page's script expands every collapsed ancestor of `#m<index>` and scrolls to it. The consumption is also recorded in reverse, so the producing action's own title gains `» 3 7` — the indices that use its output. That reverse list is what makes a long shortcut readable: you can see at a glance whether an action's output is used at all.
- `ActionOutput` with an `OutputUUID` that is in no action renders flagged (`[? Name]` in red) rather than silently as text. A dangling output means the shortcut lost an action, and silence would hide it.
- `Variable` renders the `VariableName`; the built-in sources are spelled the way the Shortcuts editor spells them (`ExtensionInput` → Shortcut Input, `CurrentDate` → Current Date, `DeviceDetails` → Device Details, `Ask` → Ask Each Time, with its prompt in quotes); any other type renders verbatim.

The same descriptors can also appear *bare* — `{"Type": "CurrentDate"}` directly as a parameter value, with no attachment wrapper. `mapping` routes a bare dict to the variable renderer when its `Type` is `Variable`, `ActionOutput` or one of the built-in sources above; any other `Type` still renders as `?type?<Type>` so an unknown shape stays visible.

Aggrandizements modify the variable in place: `WFCoercionVariableAggrandizement` → ` as <Type>` (the `WF…ContentItem` wrapper stripped, `String` read as Text and `Workflow` as Shortcut), `WFDictionaryValueVariableAggrandizement` → `.<DictionaryKey>`, `WFPropertyVariableAggrandizement` → `’s <PropertyName>`. Anything else is dumped as sorted `key value` pairs rather than dropped.

## Token strings splice by numeric offset

`WFTextTokenString` holds `Value.string` — the literal text with a U+FFFC object-replacement character wherever a variable sits — and `Value.attachmentsByRange`, a map whose keys are strings shaped like `{3466, 1}`: an offset and a length into the string.

The renderer parses the first integer out of each key, sorts the attachments by it **numerically**, then walks the rendered text nodes and replaces successive U+FFFC placeholders with the sorted attachments. Sorting those keys as strings would order `{10, 1}` before `{2, 1}` and scramble the text — the variables would come out in the wrong holes, which is silently wrong rather than visibly broken. `TestTokenStringSplicesByNumericOffset` pins this.

Splicing is done on the node list produced by `visibleText`, not on the raw string, so the trailing-whitespace rule sees real line ends: a space before a variable is a space, not a suspicious trailing blank.

Extra placeholders with no attachment are left as the literal character; extra attachments with no placeholder are dropped, exactly as the reference does.

## Slots: how an action title is filled in

The catalog gives each action a name template with numbered slots, e.g. `Set Variable $￼1 to $￼0`, plus a map from parameter key to slot (`WFVariableName` → `$￼1`) and a map of slot defaults (`$￼0` → `1` for `Wait $￼0 seconds`). `fillSlots` walks the template and, for each slot:

1. if a parameter mapped to that slot was present, substitutes its rendered value in a `<span class="box">`;
2. otherwise, if the catalog has a default for the slot, substitutes that default as plain text — the reference deliberately shows defaults unboxed, because they are not part of the shortcut's data;
3. otherwise substitutes an empty box, so the shape of the sentence survives.

Slot 0 doubles as the primary input: `catalog.Action.SlotFor` maps `WFInput` to `$￼0` when the name has that slot and no other parameter has claimed it. Slot 9 is the toggle slot: the parameter named by `Action.Toggle.Key` (usually `state`) is looked up in the toggle's value dictionary and rendered inline for "Turn …"-style actions, or as a labelled row otherwise. Slots 7 and 8 are entity slots, whose catalog value is a dotted path (`ratMode.title.key`) into the parameters; arrays render as one box per element.

Parameters that are not consumed by a slot become rows under the button:

- the parameter the catalog marks as the action's body (`#`, e.g. `WFTextActionText`) renders full width with no label, since its label would repeat the action name;
- `CustomOutputName` is appended to the title as `➔ name`;
- `WFMenuItems` on a Choose from Menu opener renders as a `Menu items` row with one item per line, rather than as a bracketed list;
- everything else becomes a labelled row whose label is the key with a leading `WF` stripped;
- `UUID`, `GroupingIdentifier`, `WFControlFlowMode` and `AppIntentDescriptor` are structural and never rendered (`catalog.IsIgnoredParameter`).

Rows are emitted in sorted key order. The reference emits them in plist order, which is not stable across reads and would make goldens and diffs noisy.

### Conditions

`If`-style actions read better with the condition in the title than as parameter rows, so for `is.workflow.actions.conditional` in modes 0 and 1 the condition is inlined and its parameters are consumed:

- the single form is `WFInput`, the operator from `catalog.Condition(WFCondition)`, and one operand picked from the keys the operator implies (`WFConditionalActionString`, `WFNumberValue`, `WFMeasurement`, `WFDuration`, `WFDate`, `WFEnumeration`, `WFLinkEnumeration`);
- codes 100 and 101 ("has any value" / "does not have any value") and 1002 ("is today") take no operand;
- code 1003 ("is between") takes two, from whichever `WF…`/`WFAnother…` pair is present;
- the multi-condition form (`WFConditions`) renders `Any`/`All` — `WFActionParameterFilterPrefix == 0` means Any — followed by one line per entry of `WFActionParameterFilterTemplates`, each rendered by the same single-condition code;
- mode 1 prefixes ` if ` so an `Otherwise If` reads as one sentence.

The same machinery renders `WFContentPredicateTableTemplate` values (the filter tables in "Find … where"), using `catalog.Filter` for the property's type and unit enumeration and `catalog.Unit` to name the unit code. A filter's operand is looked up in `Values` under the property's catalog type, then `String`, then `Number`, because real shortcuts are not consistent about which key a value lands under: an `Album` filter is typed `Enumeration` in the catalog but stored under `String`. When no operand is stored at all, the catalog's default for the property (`Recents`, `Image`, `Up`, …) is what Shortcuts uses, so that is what is shown. Operators `100`/`101` ("has any value" / "does not have any value") and `1002` ("is today") take no operand. Boolean properties read as a phrase — `Is Favorite` / `Is Not Favorite` — negated by either a `false` value or operator `5` ("is not"), and the two cancel out.

Enumeration operands arrive wrapped as `WFStringSubstitutableState`, which, like the number and date substitutable states, renders its `Value`.

## Colour groups

The group is a class on the button, not a style. It colours the action's step-number badge, by setting `--g-bg` and `--g-text` to steps 4 and 11 of a Radix scale, so dark mode comes for free when the scale switches:

| class | meaning | Radix scale |
| --- | --- | --- |
| `gGray` | control flow and scripting | slate |
| `gGreen`, `gYellow`, `gOrange`, `gRed`, `gBlue` | the catalog's group for the action | teal, amber, orange, ruby, indigo |
| `gApple` | not in the catalog, but an `is.workflow.actions.` / `com.apple.` identifier | violet |
| `gUnknown` | not in the catalog and not Apple's — a third-party app intent | cyan |

Control-flow blocks are tinted separately, by the `nonbottom` / `nontop` classes rather than the group, so a block's opener, branches and closer read as one indigo frame whatever the catalog group of the action.

An action missing from the catalog is expected, not exceptional: it means the shortcut uses an intent from an OS or app newer than `catalog/data/actions.json`. Its title comes from `catalog.DisplayName`, and `gApple` versus `gUnknown` tells the reader which kind of gap they are looking at.

## Collapsing

Every action with content is collapsible, by CSS plus a few lines of script that toggle a `closed` class (which turns the chevron) and the sibling div's `display`. Following a magic variable expands every collapsed `.content` ancestor of the target, scrolls it to the middle of the screen and briefly rings it. No framework, no images: a review of a 400-action shortcut is one file that opens instantly.

Content that would drown the page is collapsed to start with: a body over 10 000 characters or 80 newlines, or any labelled row over 1 000 characters. The thresholds are the reference's, and they are about the page, not the data — `Options.ExpandAll` renders everything open for when the reader wants to search the whole page with the browser's find.

## The README and the site

Both the review's Details panel and the README metadata table show the workflow-level settings a reader needs to use the shortcut, each only when it is set, so a shortcut without them gets no empty rows: `Accepts` and `Output` (the input and output content item classes, named the same way as a coercion and split into words, so `WFSafariWebPageContentItem` reads as Safari Web Page; an app-entity class such as `WFLinkEntityContentItem_com.apple.mobilenotes_SavePhotosEntity` is reduced to its entity name, Save Photos), `Output fallback`, `Shortcut input variables`, and one `Import question` row per entry of `WFWorkflowImportQuestions` — the question's text, the parameter it fills in, a link to the action it targets, and its default value when there is one. The README lists the import questions as their own section above the outline. Those defaults are exactly where a shared shortcut's author leaves their own API key, which is why the backup scans import questions before anything is written ([secret-scanning.md](secret-scanning.md)).

The README is deliberately plain Markdown so GitHub renders it: a metadata table, links to the importable binary and the web review, and the action outline as a nested bullet list, one line per action, indented by control-flow depth with the action's index in a code span. The outline summary is the same title the web review shows, with markup and the review's navigation affordances stripped and whitespace collapsed. A line break inside a title, which only the grouped-condition form of `If` produces, becomes `; ` so the conditions do not run together on one line. There is no colour swatch image, because the only ways to draw one in GitHub Markdown are an external image service or inline styles GitHub strips; the hex value is in the table and the site draws the real colour.

The site is returned as a map of relative path to bytes rather than written directly, so the caller owns file writing, change detection and git. Slugs come from `archive.Slug` (or `Entry.Slug`), with `-2`, `-3` suffixes if two shortcuts collide. The index is a grid of cards, one per shortcut, sorted case-insensitively by name for a stable file; what the cards show and how they are filtered is described next.

## The site design

The look of the site — palette, type, spacing, the topbar, the card library and the shortcut page — follows the Shortcut Atlas mockup. That mockup is a browser-only app that imports exports, draws a dependency map and shares links; only the visual design and the browsing affordances that work on a static, generated page were ported.

### Colour and theming

Colours come from Radix Colors 3.0.0 (MIT; the licence is kept beside the file as `assets/radix-colors.LICENSE`). `assets/radix-colors.css` is generated from the published `@radix-ui/colors` scales, keeping only the nine the design uses: slate for neutrals; indigo for the accent, control flow and `gBlue`; amber for warnings and `gYellow`; ruby for flagged text and `gRed`; teal, orange, violet and cyan for the other colour groups; and pink only in the brand mark's cyan-indigo-pink gradient. Each scale is declared three times — light on `:root`, dark under `:root[data-theme="dark"]`, and dark under `prefers-color-scheme: dark` unless `data-theme="light"` — so the system setting is the default and an explicit choice wins in both directions. `assets/site.css` maps those steps onto a handful of semantic tokens (`--bg`, `--surface`, `--line`, `--muted`, `--accent`, `--warning`, `--danger`) and everything else uses the tokens, so nothing else needs a dark-mode rule.

The sun button in the topbar cycles automatic → dark → light and stores the choice in `localStorage` under `sss-theme`. The stored value is applied by the first statement of the page's only script, in `<head>`, so a page never flashes the wrong theme; the rest of that script waits for `DOMContentLoaded`. Keeping one script per page is deliberate: the escaping test counts `<script` occurrences to prove shortcut text cannot add one.

Nothing is fetched at view time: the stylesheet is one file (or inlined in the standalone review), icons are inline SVG, and fonts are the system stack. The shortcut tile is drawn from the shortcut's real icon colour, mixed toward the surface for the background and toward the text colour for the glyph with `color-mix`, so it stays legible in both themes; the glyph itself is a generic layers icon, because the Shortcuts glyph number has no public artwork.

### The index

A heading with the site title and a `Last change` badge (the newest modification date across the library), then a collapsible Library overview with the shortcut, action and flagged counts. Below it, a toolbar with search, a filter (all shortcuts or only flagged ones) and a sort (name, most actions, recently modified), then the cards. A card shows the colour tile, the minimum iOS version when known, the name, the action count and the modification date, and a warning line when the review flags the shortcut — "flagged" means exactly what the review's notice means: corrupted control flow or a malformed value.

The toolbar is `hidden` in the markup and revealed by the script, so without JavaScript the page is simply the full list in name order. The script filters by matching the search text against each card's name and toggling `hidden`, and sorts by reordering the existing cards using `data-actions` and `data-modified` (RFC 3339, so it sorts as a string); "name" restores the order the server wrote. `/` focuses the search box. At phone width the cards collapse into a single bordered list of rows, as in the mockup.

### The shortcut page

A breadcrumb back to the index, a heading with the colour tile and the name, a metadata line (actions, iOS version, modification date, `Flagged` when it is), then two columns: the action sequence and a sticky Details panel holding the colour and every metadata row, import question and flag reason the header used to carry. Below 850px the panel moves above the actions and starts collapsed, so a phone opens on the actions with the details one tap away.

Each action is a card. The step number moved out of the title text into a `step` badge coloured by the action's group; that is the only change to the action markup, and the README is unaffected because it never used the label. Parameter rows are a two-column grid, a full-width body is a monospaced block, comments are amber, and parameter values inside a title are small chips. Control-flow nesting keeps the reference's structure and classes: a block is the opener, an indigo-bordered `contentloop` per branch and the closer, joined into one frame, with the left border cycling solid, dotted and dashed by depth and nested items sharing their parent's right edge (`inside`).

### Deliberately left out

- **The sidebar.** Its entries in the mockup — collections, the dependency map — have no counterpart in a backup, and a one-link sidebar would take width the action titles need.
- **Import, share and download dialogs, and the dependency map.** They need user uploads or relationships the renderer does not compute. Resolving Run Shortcut actions across the library would make a map possible, but it is a feature, not a restyle.
- **App icons.** The mockup looks artwork up from the App Store at view time, which would be an external request.
- **Library-wide numbers on shortcut pages.** Putting the library count or a "called by" list on every page would make each new shortcut rewrite every page in the site, and a git diff is supposed to mean the shortcut changed.

## Tests

`render_test.go` drives fifteen hand-built workflows through all three outputs and compares against golden files in `testdata/`; run `go test ./internal/render/ -update` to regenerate them, and read the diff as the review it is. The fixtures cover a linear shortcut (with a dictionary and a slot default), nested `If`/`Otherwise`/`End If` including the multi-condition and "is between" forms, a `Repeat` containing a `Menu` containing an `If` (which is what exercises all three depth cues), unbalanced blocks in all three broken shapes, a magic-variable chain with aggrandizements and a dangling output, a token string whose attachment offsets sort differently as numbers than as strings, uncatalogued Apple and third-party actions, text made of `<script>`, quotes, zero-width, non-breaking and control characters, and variables buried in one and two layers of serialization wrapper. Six more are adapted from the examples in the shortcuts-generator reference (see [reference-notes.md](reference-notes.md)): a Find Photos filter table covering every operand shape, a menu with `WFMenuItems`, a Repeat with Each using Repeat Item/Index and the built-in variable sources, string-typed modes and conditions, a token string whose attachment offset is off by one, and a workflow with import questions and input/output classes.

Beyond the goldens the tests assert what the goldens cannot: that every output is byte-identical across two runs, that every opened tag closes in LIFO order (a hand-rolled scan, to avoid taking a dependency on `golang.org/x/net/html`), that shortcut text cannot introduce a tag, that no README contains an HTML-only `\u…` escape, that the token-string splice order follows the numeric offsets, and that `ExpandAll`, base URLs, relative links and slug collisions behave.

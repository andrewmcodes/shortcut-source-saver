# How a shortcut is rendered

This document explains `internal/render`, the package that turns a `shortcuts.Workflow` into the three human-readable artefacts: the per-shortcut web review, the per-shortcut `README.md`, and the GitHub Pages site. It is the counterpart to [archive.md](archive.md): that document covers the machine-readable files, this one covers the ones a person reads.

The structure of the web review — the collapsible per-action buttons, the colour groups, the class names on the action markup — is taken from gluebyte's Shortcut Source Tool, a Shortcut that renders a shortcut as an HTML page. Its logic was extracted and reimplemented in Go, and the class names (`button`, `content`, `contentloop`, `nonbottom`, `gYellow`, …) are kept so the two can still be compared side by side. The visual design is not the reference's: it takes visual inspiration from the Cupertino concept, described in [The site design](#the-site-design) below.

## The API

| function | returns |
| --- | --- |
| `WebReview(w)` / `WebReviewWithOptions(w, opts)` | one self-contained HTML page (CSS inlined, no external requests, no images) |
| `ShortcutREADME(w)` / `ShortcutREADMEWithOptions(w, opts)` | Markdown for `shortcuts/<slug>/README.md` |
| `Site(entries, opts)` | `map[relative path][]byte` — `index.html`, `dependencies.html`, `shortcuts/<slug>.html` per entry, `assets/style.css` |
| `Stylesheet()` | the concatenated stylesheet (`assets/radix-colors.css`, `assets/site.css`, `assets/actions.css`, in that order), for callers that write it themselves |

`Options.ExpandAll` disables the collapse-large-content heuristic. `Options.SiteURL`, `Options.SiteRoot`, and `Options.Slug` decide the README's review link (see [The README's review link](#the-readmes-review-link)). `Options.ShortcutFile` names the importable binary the README links to (default `shortcut.shortcut`), because the caller chooses that name. Every link inside the site is relative (`assets/style.css` from the index, `../assets/style.css` from a shortcut page). A relative link resolves the same way from a `file://` folder, a local static server, a GitHub Pages user site at `/` and a project site at `/<repo-name>/`, so the site needs no knowledge of where it is served and no build step to be viewed locally. Root-absolute links (`/assets/style.css`) only work when the site is served at a domain root; opened from disk they resolve to the filesystem root and the browser refuses to load them.

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

Every action with content is collapsible, by CSS plus a few lines of script that toggle a `closed` class (which turns the chevron) and the sibling div's `display`. Following a magic variable expands every collapsed `.content` ancestor of the target, moves focus to it, scrolls it to the middle of the screen and briefly rings it.

Magic variables — the variable chips, the `» 3 7` consumer lists, the `▵` block links, import-question targets and the corrupted notice — are spans with `role="link"` and `tabindex="0"`, so they are in the tab order and announced as links. Enter or Space on a focused one jumps to its target; the handler stops the key event so it does not also toggle the action button that contains it, and Space's default page scroll is suppressed. A focused link shows a cyan focus ring. They remain spans inside the action `<button>` rather than real anchors, because the reference's markup makes the whole title row a button; a screen reader may therefore flatten them into the button's name, which is a known limitation of that structure. No framework, no images: a review of a 400-action shortcut is one file that opens instantly.

Content that would drown the page is collapsed to start with: a body over 10 000 characters or 80 newlines, or any labelled row over 1 000 characters. The thresholds are the reference's, and they are about the page, not the data — `Options.ExpandAll` renders everything open for when the reader wants to search the whole page with the browser's find.

## The README and the site

Both the review's Details panel and the README metadata table show the workflow-level settings a reader needs to use the shortcut, each only when it is set, so a shortcut without them gets no empty rows: `Accepts` and `Output` (the input and output content item classes, named the same way as a coercion and split into words, so `WFSafariWebPageContentItem` reads as Safari Web Page; an app-entity class such as `WFLinkEntityContentItem_com.apple.mobilenotes_SavePhotosEntity` is reduced to its entity name, Save Photos), `Output fallback`, `Shortcut input variables`, and one `Import question` row per entry of `WFWorkflowImportQuestions` — the question's text, the parameter it fills in, a link to the action it targets, and its default value when there is one. The README lists the import questions as their own section above the outline. Those defaults are exactly where a shared shortcut's author leaves their own API key, which is why the backup scans import questions before anything is written ([secret-scanning.md](secret-scanning.md)).

The README is deliberately plain Markdown so GitHub renders it: a metadata table, links to the importable binary and the web review, and the action outline as a nested bullet list, one line per action, indented by control-flow depth with the action's index in a code span. The outline summary is the same title the web review shows, with markup and the review's navigation affordances stripped and whitespace collapsed. A line break inside a title, which only the grouped-condition form of `If` produces, becomes `; ` so the conditions do not run together on one line. There is no colour swatch image, because the only ways to draw one in GitHub Markdown are an external image service or inline styles GitHub strips; the hex value is in the table and the site draws the real colour.

### The README's review link

The README links to the shortcut's page in the generated site, chosen in this order:

1. **`SiteURL` is a full URL with a scheme and host** (`https://you.github.io/my-shortcuts/`): link the hosted page, `SiteURL` + `shortcuts/<slug>.html`.
2. **Otherwise, `SiteRoot` is set**: link the page by relative file path, `SiteRoot` + `/shortcuts/<slug>.html`, URL-escaped. The backup passes the path from the README's own directory to the site directory, so with the default layout a README at `shortcuts/<slug>/README.md` links `../../docs/shortcuts/<slug>.html`.
3. **Neither**: link a sibling `review.html`, for callers that render a review next to the README themselves.

`Slug` is the directory the backup actually chose, so shortcuts whose names collide (see [backup.md](backup.md)) link to their own page rather than to a slug recomputed from the name.

The backup never passes `SiteURL`, so READMEs always get the relative file path. A root-absolute link resolves against whatever is showing the README: on github.com it points at `github.com/shortcuts/…`, and from a folder on disk at the filesystem root, so it was broken everywhere it was read. The relative file path works in GitHub's repository browser, in an editor, and in a backup written with `--output` and opened from disk. It does not open the rendered Pages site from github.com; that would need the site's host, which the config does not record.

The site is returned as a map of relative path to bytes rather than written directly, so the caller owns file writing, change detection and git. Slugs come from `archive.Slug` (or `Entry.Slug`), with `-2`, `-3` suffixes if two shortcuts collide. The index is a grid of cards, one per shortcut, sorted case-insensitively by name for a stable file; what the cards show and how they are filtered is described next.

## The site design

The site takes visual inspiration from the Cupertino concept: neutral grouped surfaces, subtle borders, blue links, rounded cards, and compact spacing. The existing library, detail, and dependency navigation remain the source of truth; the concept’s sample data and simplified action renderer are not used. The system font stack selects San Francisco on macOS and iOS without downloading fonts. Pointer controls are compact on desktop, while touch controls retain their larger targets. How shortcuts are linked to each other is described in [Run Shortcut and the dependency map](#run-shortcut-and-the-dependency-map).

### Colour and theming

Colours come from Radix Colors 3.0.0 (MIT; the full licence is kept beside the file as `assets/radix-colors.LICENSE`). The first line of `assets/radix-colors.css` is a one-line `/*! … */` notice naming the project, its licence and its copyright holders; because that file is concatenated first, the notice opens the published `assets/style.css` and the stylesheet inlined in standalone reviews, so every copy of the colours carries it. It is an approved exception to the project's no-comments rule, made because the MIT licence asks for its notice to travel with copies, and it is the only comment in the generated output. `assets/radix-colors.css` is generated from the published `@radix-ui/colors` scales, keeping only the nine the design uses: slate for neutrals; indigo for the accent, control flow and `gBlue`; amber for warnings and `gYellow`; ruby for flagged text and `gRed`; teal, orange, violet and cyan for the other colour groups; and pink only in the brand mark's cyan-indigo-pink gradient. Each scale is declared three times — light on `:root`, dark under `:root[data-theme="dark"]`, and dark under `prefers-color-scheme: dark` unless `data-theme="light"` — so the system setting is the default and an explicit choice wins in both directions. `assets/site.css` defines the neutral surfaces and blue accents with paired `light-dark()` semantic tokens (`--bg`, `--surface`, `--line`, `--muted`, `--accent`); warnings, action groups, and control flow keep the Radix scales. The root `color-scheme` follows the system unless the existing appearance control selects an explicit light or dark theme.

The shared footer pairs a linked © Andrew Mason credit on the left with a three-button appearance group on the right: system (monitor), light (sun), and dark (moon). Each button exposes its selection with `aria-pressed`; manual choices use `localStorage` under `sss-theme`, and choosing system removes the override. The stored value is applied by the first statement of the page's only script, in `<head>`, so a page never flashes the wrong theme; the rest of that script waits for `DOMContentLoaded`. Keeping one script per page is deliberate: the escaping test counts `<script` occurrences to prove shortcut text cannot add one.

Nothing is fetched at view time: the stylesheet is one file (or inlined in the standalone review), icons are inline SVG, and fonts are the system stack. The shortcut tile is drawn from the shortcut's real icon colour, mixed toward the surface for the background and toward the text colour for the glyph with `color-mix`, so it stays legible in both themes; the glyph itself is a generic layers icon, because the Shortcuts glyph number has no public artwork.

Mixing does not rescue a black icon: in dark mode a black tile on a near-black surface with a mostly-black glyph all but disappears, and black is the single most common colour in a real library. So `isDarkColor` computes the colour's WCAG relative luminance in Go and, below 0.05 (black and the near-black greys; the mid greys `#A9A9A9` and `#B4B2A9` stay coloured), the tile gets a `dark-icon` class. That class ignores the colour and uses neutral steps instead — `slate-4` background, `slate-12` glyph, a `slate-7` ring — which flip with the theme on their own: a light-grey tile with a black glyph in light mode, a lifted dark-grey tile with a white glyph in dark mode. The decision is made at build time from the stored colour, so it is deterministic and needs no relative-colour CSS support in the browser.

### The index

A “My shortcuts” heading and a `Last change` badge (the newest modification date across the library), then a collapsible Library overview with the shortcut, action, flagged and connected counts, plus a deleted count when the backup keeps any deleted shortcuts (see [Deleted shortcuts](#deleted-shortcuts)). Below it, a toolbar with search, a filter (all shortcuts, flagged, connected, standalone, with a missing reference, or deleted), an output-type multi-select, a sort (name, most actions, recently modified) and a `Show deleted` checkbox, then the cards, and a footer line linking the dependency map. A card shows the colour tile, the minimum iOS version when known, the name, the action count, the modification date, and how it is connected — `N calls` when it runs other shortcuts, `N callers` when it only is run, `Standalone` otherwise — plus a warning line when the review flags the shortcut and another when it runs a shortcut that is not in the backup. "Flagged" means exactly what the review's notice means: corrupted control flow or a malformed value; a missing reference is a warning, not a flag, because the shortcut itself is intact.

The toolbar is `hidden` in the markup and revealed by the script, so without JavaScript the page is simply the full list in name order. The script filters by matching the search text against each card's name and toggling `hidden`; each filter option carries the CSS selector its cards must match in `data-match` (`[data-flagged]`, `[data-connected], .with-deleted [data-linked]`, `:not([data-connected]):not(.with-deleted [data-linked])`, `[data-missing]`, `[data-deleted]`), so a new filter is one attribute on the card and one option, not new script. The script recounts every option's number from the cards whenever the deleted checkbox changes, so the counts describe the category before search and output-type filtering.

The page honours a hash naming a filter: `index.html#standalone` opens on the Standalone filter and `index.html#deleted` on the deleted shortcuts (ticking the checkbox, since the filter would otherwise be empty). Changing the filter rewrites the hash with `history.replaceState`, so the address bar can be shared and Back does not step through filter changes. The dependency map's footnote links to `#standalone` and its heading to `#deleted`. It sorts by reordering the existing cards using `data-actions` and `data-modified` (RFC 3339, so it sorts as a string); "name" restores the order the server wrote. `/` focuses the search box. At phone width the cards collapse into a single bordered list of rows, as in the mockup.

Every page of the site carries a `Dependency map` link in the topbar; below 620px its label is visually hidden but stays its accessible name.

### The shortcut page

A topbar link back to the library, a heading with the colour tile and the name, a metadata line (actions, iOS version, modification date, `Flagged` when it is), then the action sequence alongside a fixed, independently scrollable Details sidebar. Runs and Called by appear before the complete metadata, import questions, and flag reasons in the Shortcut information disclosure. At 850px and below, Details starts closed and opens as a modal drawer from the navbar.

### Deleted shortcuts

The backup never removes a shortcut, because the point of it is to find an old solution again; instead `metadata.json` records whether a shortcut is still live (see [backup.md](backup.md#shortcuts-that-are-no-longer-active)). The site treats both recorded states — `deleted` (no longer in the library) and `conflict` (a sync-conflict copy the Shortcuts app hides) — as **inactive**, and calls both "Deleted" to the reader, giving the reason wherever there is room: "Deleted from your library" or "Sync-conflict copy of *original*".

- **Marking.** Inactive shortcuts get a ruby `Deleted` badge with a bin glyph (the same pill shape as the other badges) and muted styling: a dashed border, a faded tile and a muted name. The shortcut page adds the badge to the metadata line and a banner under it saying why; a conflict copy's banner links to the original's page when the original is in the backup (matched by workflow id, then name), and names it unlinked otherwise.
- **Hidden until asked for.** The index and the map open on active shortcuts only. Both pages have a `Show deleted` checkbox with the number of inactive shortcuts beside it, so they stay discoverable, and the choice is stored in `localStorage` under `sss-show-deleted` like the theme. The key is shared: someone who wants deleted shortcuts on the map wants them in the index too. Without JavaScript nothing is hidden: both pages list everything, marked, in their usual order.
- **Separate, not mixed.** Runs and Called by on a shortcut page list only active shortcuts; inactive ones go in "Runs (Deleted)" and "Called by (Deleted)" sections below them, each omitted when empty. Missing, run-time and self calls are not about another shortcut's status, so they stay in Runs. A Run Shortcut action whose target is inactive says so in its `Shortcut` row.
- **Counts describe active relationships.** A relationship counts as active when the shortcut on the other end is active. The index's overview numbers (shortcuts, actions, flagged, connected) count active shortcuts only, with deleted ones as their own number. A card's `N calls` / `N callers` / `Standalone` counts active neighbours, and adds `+N deleted` (or `N deleted links` when it has no active ones) so a relationship with a deleted shortcut is never silently dropped. `data-connected` marks a card connected through an active neighbour and `data-linked` connected at all, which is why the Connected and Standalone filters switch selector when deleted shortcuts are shown.

Deciding activeness by the neighbour, not by the pair, keeps every page's split a function of its direct neighbours' status, so a status change rewrites only the changed shortcut's page and its neighbours' (see [What a change rewrites](#what-a-change-rewrites)).

Each action is a card. The step number moved out of the title text into a `step` badge coloured by the action's group; that is the only change to the action markup, and the README is unaffected because it never used the label. Parameter rows are a two-column grid, a full-width body is a monospaced block, comments are amber, and parameter values inside a title are small chips. Control-flow nesting keeps the reference's structure and classes: a block is the opener, an indigo-bordered `contentloop` per branch and the closer, joined into one frame, with the left border cycling solid, dotted and dashed by depth and nested items sharing their parent's right edge (`inside`).

### Deliberately left out

- **Import, share and download dialogs.** They need user uploads or a server.
- **App icons.** The mockup looks artwork up from the App Store at view time, which would be an external request.
- **Library-wide numbers on shortcut pages.** Putting the library count on every page would make each new shortcut rewrite every page in the site, and a git diff is supposed to mean the shortcut changed. Runs and Called by are on the page because they only change when a direct neighbour does; see [What a change rewrites](#what-a-change-rewrites).
- **The mockup's change-impact panel and the whole-library graph.** Transitive "depends on this" lists would make a page change whenever anything upstream changes, and a graph of every connection is unreadable at phone width on a real library; the map page shows one hop around one shortcut instead (see [The map page](#the-map-page)).

## Run Shortcut and the dependency map

`deps.go` finds every `is.workflow.actions.runworkflow` action in the library and decides which shortcut it runs; `graph.go` turns the result into the ranked rows of the map page. Both work on the entries `Site` already receives — the backup passes the whole repository, not just the shortcuts of the current run — so nothing re-reads the Shortcuts database and `internal/shortcuts` stays the only package that knows Apple's schema. A standalone `WebReview` has no library, so it renders Run Shortcut exactly as before and has no Runs or Called by sections.

### Reading the target

A real library stores the target in three shapes, sometimes in one action:

| shape | example | read as |
| --- | --- | --- |
| `WFWorkflow` descriptor | `{"workflowIdentifier": "D93F…", "workflowName": "get_apple_app", "isSelf": false}` | identifier, name, and whether it is the shortcut itself |
| `WFWorkflowName` string | `"get_apple_app"`, the older form, kept alongside the descriptor | the name, when the descriptor has none |
| a variable in either key | `WFWorkflow` and `WFWorkflowName` both a `WFTextTokenAttachment` | dynamic: the target is chosen at run time |

`isSelf` is authoritative: a shortcut imported from someone else keeps the author's identifier in its self-calls, so the stored identifier of a self-call usually matches nothing.

### Resolution order

1. A variable target is **dynamic** and is never guessed at.
2. `isSelf` is **self**.
3. The identifier, compared case-insensitively against every entry's `WorkflowID`: exactly one match is **linked** (or **self**, if it is the caller). If several entries share the identifier, the one whose name also matches wins.
4. The name, compared exactly: exactly one match is **matched by name** (or **self**). A shortcut that is deleted and re-imported gets a new identifier while its callers keep the old one, so name fallback is what keeps those calls linked; on the real library it resolves 11 of 115 calls, all from the two copies of Shortcut Source Tool to a Shortcut Source Helper whose stored identifier is not in the library.
5. Otherwise the call is a **missing reference** — including when several shortcuts share the name, because picking one would be a guess. "Missing" means not in this backup, which also covers a shortcut excluded by the config.

On the author's library of 217 shortcuts the 115 Run Shortcut actions resolve as 94 linked, 11 by name, 8 self, 1 missing and 1 dynamic.

### On the shortcut page

The Details panel gains a **Runs** section, one entry per distinct target in order of first use, with the action numbers that call it as magic links, and a **Called by** section listing every other shortcut that runs this one, sorted by name. Linked targets show the tile and link to the page; a target whose stored name differs from its current one says what it was recorded as; a name match says the stored identifier is not in the backup; missing targets are amber with the identifier in monospace; self and dynamic calls are plain rows. An `Open dependency map` button links to `dependencies.html#map-<slug>` for a connected shortcut. Inactive neighbours are listed in separate "Runs (Deleted)" and "Called by (Deleted)" sections; see [Deleted shortcuts](#deleted-shortcuts).

The Run Shortcut action itself keeps the reference's title, `Run Shortcut [name]`, but the name becomes a link to the target's page, amber when it is missing, and the raw `WFWorkflow` dictionary row is replaced by a `Shortcut` row saying how it was resolved. The link is a `magic` span with `data-href` instead of `data-target`, because the title is inside the action's `<button>` and an `<a>` there would be invalid nested interactive content; the page script follows `data-href` on click, Enter or Space (Cmd/Ctrl opens a new tab), so it has the same keyboard behaviour as every other magic link. `html/template` treats `data-href` as a URL attribute and sanitises it.

### The map page

`dependencies.html` is a focus view over a ranked list. It replaced a generated SVG per connected group: on the real library the largest group was about 70 shortcuts in one tall map with crossing edges, followed by dozens of small maps and a relationship list that repeated every connection, and none of it could be read at phone width. Most questions a reader brings to the page are about one shortcut — what runs this, what does it run, is anything missing — so the page answers those one shortcut at a time instead of drawing the whole library.

**The ranked list** is the page's content. It lists every connected shortcut (one that runs or is run by another), ranked by its number of connections: distinct other shortcuts that run it plus distinct targets it runs, counting a missing target and "chosen at run time" as one target each and leaving out a self-call, which is shown as "runs itself" instead. Ties are broken by name and then slug, so the order is stable. Each row shows the rank, the colour tile (with `dark-icon`), the name, `N callers · M runs`, and a warning line when the shortcut runs something not in the backup. Standalone shortcuts are not listed; a footnote counts them and links to `index.html#standalone`, which opens the index on the Standalone filter.

**Deleted shortcuts** are left out of the focus diagram, the search suggestions and the ranked list until the `Show deleted shortcuts` checkbox is ticked; then they appear with the `Deleted` badge, faded and with dashed borders, and their edges are drawn faint and dashed. Ranks and connection counts are computed over what is shown, not over the whole backup: with deleted shortcuts hidden, a deleted caller does not count toward a shortcut's rank, and a shortcut whose only connections are deleted ones counts as standalone and drops out of the list. That way every number on the screen can be checked against the rows and the diagram beside it, and the order does not reward a shortcut for being called by copies nobody runs any more. The footnote's standalone count switches with the checkbox for the same reason. A `#map-<slug>` link naming a shortcut that is only listed with deleted shortcuts shown — an inactive one, or an active one connected only to inactive ones — ticks the checkbox for that visit (without storing it), so links from deleted shortcut pages always land on the shortcut; unticking it while such a shortcut is focused clears the hash and returns to the top-ranked one.

**Without JavaScript** the list is the whole page and is still complete, deleted shortcuts included and marked, ranked over every connection: each row links to the shortcut's page and has a `<details>` holding its callers and runs as links, missing targets in amber with the reason, dynamic targets, and "Itself" for a self-call. Each row carries `id="map-<slug>"`, so the `dependencies.html#map-<slug>` links on shortcut pages land on the right row even with scripting off.

**With JavaScript** the script reads the graph back out of that same markup — the row's `data-key`, `data-order` (its position in name order) and `data-inactive`, its tile, its name and status text, and the `data-relation` / `data-key` / `data-kind` / `data-note` / `data-inactive` attributes on the details entries — then removes the details and points each row's link at `#map-<slug>`. It recomputes each row's counts and rank for the current checkbox state, re-sorting by count and then `data-order`, so its tie-break is the server's name order rather than JavaScript's string comparison. The graph is therefore never serialised into the script: there is no embedded JSON, the page keeps its single `<script>` (so the escaping test's count still proves shortcut text cannot add one), and every name reaches the screen through `textContent`. The script also removes the rows' ids, so selecting a row or a neighbour changes the hash without the browser jumping down to the list.

Above the list, a search box and a focus card are revealed:

- **The focus card** shows the focused shortcut in the middle, the shortcuts that run it in a left column and what it runs in a right column, one hop each way. Edges are drawn in an absolutely positioned SVG after layout, from each node's measured box: caller edges fan into a short trunk with one arrowhead, so a shortcut with thirteen callers gets one clean arrow rather than a comb of arrowheads, and outgoing edges fan out from a trunk to an arrowhead on each target. Missing targets are amber dashed nodes and edges, run-time targets dotted, and a self-call is a "Runs itself" badge under the focused node. A `ResizeObserver` redraws the edges whenever the card's size changes; hovering or focusing a neighbour highlights its edge. The focused node links to the shortcut's page; every neighbour that is in the backup is a link to `#map-<slug>`, so clicking it or pressing Enter refocuses on it, Cmd/Ctrl-click opens it in a new tab, and Back returns to the previous focus. After a refocus from a click or Enter, keyboard focus moves to the new focused node and the page scrolls to it only when it is off screen.
- **At 760px and below** the three columns stack — Called by, then the focused shortcut, then Runs — with a vertical arrow between them; the edge SVG and the legend are hidden, since each node already says what it is ("Not in this backup", "From a variable"). Each column shows its first four neighbours and a "Show N more" button, so the focused shortcut stays near the top of a phone screen even when thirteen shortcuts run it.
- **The search box** is an ARIA combobox: typing filters the ranked list live and offers up to eight matches (names starting with the query first, then by rank) in a listbox. Arrow keys move through the options, Enter focuses the highlighted one, Escape closes the options and then clears the search, and `/` focuses the box from anywhere on the page, as on the index.
- **The URL hash** is the single source of the focus: search, rows and neighbours all set `#map-<slug>`, and one `hashchange` handler renders the card. With no hash, or a hash naming no connected shortcut, the card opens on the top-ranked shortcut. A live region announces each new focus with its counts, and the selected row is marked `aria-current` and highlighted.

The arrowhead marker is defined once in the edge SVG and filled with `context-stroke`, so a highlighted edge recolours its arrow. The glyphs the script needs (warning, run-time, loop, chevron) are cloned from an inert `<template>` in the page rather than built from strings.

### What a change rewrites

Identical input still produces byte-identical output, and the order of the entries does not matter: the map's rows are ranked by connection count and then name and slug, callers are sorted by name, and runs follow the order of the caller's own actions, which a test checks by reversing the entries. What a change to one shortcut rewrites:

- **Its own page**, as before.
- **The pages of its direct callers and callees**, when the change touches what they show of it: its name, slug, colour, status, or whether it still runs them. A shortcut being deleted or restored moves it between a neighbour's active and deleted sections, which a test checks. A caller's page also changes when the caller's own Run Shortcut actions move, because Runs lists action numbers.
- **The index and the map**, which are library-wide by design.
- **Nothing else.** Adding a shortcut that nobody runs and that runs nothing rewrites only the index, the map and its own page, which a test checks against the fixtures and which a run against the real library confirmed.

The one indirect effect is name fallback: a new shortcut that shares the name of a target matched by name makes that name ambiguous, so the caller's page changes to show a missing reference. That is the honest outcome — the backup can no longer tell which shortcut is meant — and it only happens to calls that had already lost their identifier.

## Tests

`render_test.go` drives fifteen hand-built workflows through all three outputs and compares against golden files in `testdata/`; run `go test ./internal/render/ -update` to regenerate them, and read the diff as the review it is. The fixtures cover a linear shortcut (with a dictionary and a slot default), nested `If`/`Otherwise`/`End If` including the multi-condition and "is between" forms, a `Repeat` containing a `Menu` containing an `If` (which is what exercises all three depth cues), unbalanced blocks in all three broken shapes, a magic-variable chain with aggrandizements and a dangling output, a token string whose attachment offsets sort differently as numbers than as strings, uncatalogued Apple and third-party actions, text made of `<script>`, quotes, zero-width, non-breaking and control characters, and variables buried in one and two layers of serialization wrapper. Six more are adapted from the examples in the shortcuts-generator reference (see [reference-notes.md](reference-notes.md)): a Find Photos filter table covering every operand shape, a menu with `WFMenuItems`, a Repeat with Each using Repeat Item/Index and the built-in variable sources, string-typed modes and conditions, a token string whose attachment offset is off by one, and a workflow with import questions and input/output classes.

Beyond the goldens the tests assert what the goldens cannot: that every output is byte-identical across two runs, that every opened tag closes in LIFO order (a hand-rolled scan, to avoid taking a dependency on `golang.org/x/net/html`), that shortcut text cannot introduce a tag, that no README contains an HTML-only `\u…` escape, that the token-string splice order follows the numeric offsets, that `ExpandAll`, relative links and slug collisions behave, that the index marks and counts exactly the shortcuts the reviews flag, and that no generated file references an external resource.

`deps_test.go` covers the dependency features with one three-shortcut library whose caller runs a shortcut by identifier, by a stale identifier that falls back to the name, itself, a missing shortcut, a variable target, a bare `WFWorkflowName`, and a stale name: it asserts each resolution, that an identifier matching the caller is a self-call, that an ambiguous name is missing, the Runs and Called by sections and index cards that result, that adding an unrelated shortcut leaves every shortcut page byte-identical, that the site does not depend on the order of the entries, that the map ranks by connections with ties by name and leaves standalone shortcuts out, that its no-JavaScript list carries every caller and run as the right link, and that hostile names — including one containing `</script>` — are escaped and cannot add a tag or a second script. `site-map.html` and `site-calls-page.html` are its goldens. A second library — an original, its sync-conflict copy, a shortcut both run, and a deleted shortcut it runs — covers inactive shortcuts: the active and deleted split of Runs and Called by, the conflict banner linking the original (and an escaped, unlinked original name in a standalone review), the index's marks, counts and filter options, the map's marked rows and relations with the checkbox off by default, the hash deep-link and footnote contract, and that restoring the deleted shortcut rewrites only its own page and its caller's. What the map's script does with that markup — hiding, re-ranking, the deep link ticking the checkbox — needs a browser, so it was checked with headless Chrome rather than in Go.

### Cupertino behavior preservation

The redesign changes shared styles and header markup only. `action.go` owns summaries and complete parameter rows; `value.go` and `variable.go` own nested values, whitespace, inline tokens, and action references; `flow.go` owns order, nesting, reference indicators, and default collapse decisions. The `items`, `row`, and `node` templates and the review interaction script retain their existing markup and behavior. Shortcut resolution stays in `deps.go`, and the dependency page retains its existing graph and keyboard navigation. Library and dependency links sit in a sticky top navbar; appearance controls sit in the shared footer. An icon-only home link replaces the paired icon and site-title branding. The library link remains available from both detail and dependency pages.

Generated pages append a deterministic SHA-256 content version to the relative stylesheet URL. This prevents an updated page from using cached CSS from an earlier build; the file remains `assets/style.css`, and no server support is needed.

The Details panel stacks each metadata label above its complete value so identifiers and timestamps can use the panel width. The action column keeps its existing width and styles. The library navigation indicates the current page on the index and the current section on shortcut pages.

### Details sidebar and drawer

Shortcut pages show Details in a fixed, independently scrollable right sidebar on desktop. The navbar panel icon and the close button toggle it, releasing its width back to the page when closed. Below 851px it starts closed and opens as a native modal dialog, with focus containment, Escape dismissal, a backdrop, and background scroll locking. Selecting an action reference from the mobile drawer closes it before the existing action-navigation handler focuses the target. Resizing across the breakpoint closes the mobile drawer or restores the desktop sidebar. Without JavaScript, the open dialog renders Details in normal document flow after the actions. No action markup, styles, collapse defaults, or handlers are changed by the drawer.

The header groups the shortcut title and metadata beside its original icon. Navigation sits beside the home icon, with appearance and drawer controls at the trailing edge. In Details, Runs and Called by appear first; complete metadata remains available in the native “Shortcut information” disclosure below them. This disclosure is independent of the protected Actions renderer.

The library Output types filter uses declared `OutputContentItemClasses`, with the same human-readable names as shortcut details. Multiple selections match any selected type and intersect with the existing category, search, and deleted visibility filters. Missing or invalid declarations are labeled “Not specified”; outputs are not inferred from actions. Clear search and filters also resets output selection.

All controls outside `.actions` share `--control-radius` (9px), including search fields, selects, disclosures, icon buttons, appearance controls, and checkboxes. Checkbox styling retains native input semantics and keyboard behavior. Action controls are explicitly excluded from these rules.

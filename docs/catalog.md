# The action catalog

This document explains `internal/catalog`, the package that answers "what is this action called, and what colour is it?" for a Shortcuts action identifier. A renderer walking a shortcut's actions needs, per action: a display name with its parameters substituted in, a colour group, which parameter is the action's body, and which parameters not to show. This package is the source of all four.

The code has no comments by project convention, so everything non-obvious about it is here.

## Where the data comes from

A shortcut's plist stores actions as opaque identifiers — `is.workflow.actions.repeat.count`, `com.apple.Health.OpenDataTypeIntent` — and a bag of parameters. There is no display name, no colour, and no indication of which parameters Shortcuts draws inline in the action's sentence. macOS ships no machine-readable catalog of this either: the `shortcuts` CLI offers only run/list/view/sign, and the names live inside Shortcuts.app's localised bundles.

gluebyte's [Shortcut Source Tool](https://routinehub.co/shortcut/5256/) (RoutineHub #5256) solved this by hand. That shortcut embeds a ~123 KB JavaScript renderer inside a single Text action, and the first ~100 KB of that program is four lookup tables covering 729 action identifiers. `scripts/extract-catalog.mjs` lifts those tables into `internal/catalog/data/`:

| file | entries | what it holds |
| --- | --- | --- |
| `actions.json` | 729 identifiers | display templates, colour groups, parameter slots |
| `conditions.json` | 17 | `WFCondition` code → operator text |
| `filters.json` | 20 | filter property → value type, default, unit enumeration |
| `units.json` | 2 enumerations | `Unit` and `ByteCountUnit` code → unit name |

### Regenerating

The reference export (`.ai/shortcut_source_tool.json`) is gitignored, so this only works on a machine that has it:

```
mise run extract-catalog
```

The generated JSON **is** committed and is what the build depends on — `//go:embed data/*.json` compiles it into the binary, so lookups never touch the filesystem. `TestEmbeddedDataParsesWithExpectedCounts` and `TestEmbeddedDataHasExpectedActionsPerColourGroup` assert the counts in the tables above; if a regeneration changes them, those tests fail rather than the change showing up later as missing names in a rendered page.

## Reading the catalog

`Default()` returns a lazily-parsed singleton (via `sync.OnceValues`) that is safe for concurrent reads. It panics if the embedded JSON is malformed, which is a build-time defect rather than a runtime condition: the data is compiled in and the tests parse it. `Load()` is the same work with the error returned, for callers who want it.

Nothing in the package mutates after `Load`. An `Action` is returned by value but the maps inside it are shared across callers, so treat them as read-only. `Groups()` and `IgnoredParameters()` return copies because they are small and callers do iterate them.

## Actions are indexed by control-flow mode

Every identifier in `actions.json` maps to an **array of states, indexed by `WFControlFlowMode`**:

| index | meaning |
| --- | --- |
| 0 | the action, or a block's opening |
| 1 | a block's middle — `Otherwise`, `Menu Item` |
| 2 | a block's closing — `End If` |

`is.workflow.actions.conditional` is the clearest example: `["If ", "Otherwise", "End If"]`. A plain action has a single-element array. `Catalog.Modes(identifier)` reports the length, which is how a renderer knows to open and close nesting — `Action.ControlFlow` is the same fact as a boolean.

Two things make a requested mode unavailable. It can be out of range (mode 2 of a plain action, or a negative value from a malformed plist), or the data can mark it unused: `is.workflow.actions.repeat.count` is `[{...}, {}, {...}]`, because a Repeat block has no middle. In both cases `Lookup` falls back to mode 0 so the caller always gets a usable name, and sets `Action.Mode` to the mode it actually resolved. Compare that against what you asked for to detect the fallback.

`"If "` keeps its trailing space on purpose: the condition text is appended to it.

## Name templates and slots

`Action.Name` is a template, not a finished string:

```
Repeat $￼0 Times
```

`￼` there is U+FFFC OBJECT REPLACEMENT CHARACTER — the character Shortcuts itself uses to mark where a parameter is drawn inside an action's sentence. A slot token is `$` followed by it and a digit, so slot 0 is the four-byte-ish string `$￼0`. Use `SlotN(0)` rather than writing that out, and `Slot.Index()` to go back.

`Action.Slots` maps a real parameter key to the slot its value belongs in — `{"WFRepeatCount": "$￼0"}`. A parameter absent from that map has no inline position and should be rendered as a labelled row instead.

### Slot 0 falls back to `WFInput`

An action's primary input gets no catalog entry, because it is always `WFInput`. So when a template contains slot 0 and nothing in `Slots` claims it, `WFInput` fills it. `is.workflow.actions.appendvariable` (`"Add $￼0 to Variable $￼1"`, mapping only `WFVariableName` to slot 1) relies on this; `is.workflow.actions.getvariable` maps `WFVariable` to slot 0 explicitly and so must **not** also let `WFInput` claim it.

`Action.SlotFor(parameter)` implements both halves of that rule. Prefer it over reading `Slots` directly.

### Slot defaults

Shortcuts omits parameters that are still at their default value, so an action saved untouched stores nothing at all and its sentence cannot be completed from the plist alone. The catalog records those defaults as extra entries in the same map, keyed by the slot token instead of a parameter key:

```json
{ "name": "Repeat $￼0 Times", "param": { "WFRepeatCount": "$￼0", "$￼0": "1" } }
```

`Action.Defaults` holds only those entries (`{"$￼0": "1"}`), separated out during parsing. A slot left unfilled after substituting parameters should fall back to `Defaults[slot]`, then to an empty placeholder.

### `#` marks the body parameter

Eight actions have a parameter that is the action's payload rather than a setting — a comment's text, a Run JavaScript script, a dictionary's contents. The catalog flags them by mapping the parameter to the literal string `#` in place of a slot token:

```json
{ "name": "Comment", "group": "gYellow", "param": { "WFCommentActionText": "#" } }
```

`Action.Body` holds that parameter key (`""` when there is none), and it is deliberately **not** also present in `Slots`. Render it full width with no label. The eight are `comment`, `dictionary`, `gettext`, `list`, `runapplescript`, `runjavascriptforautomation`, `runjavascriptonwebpage`, and `showresult`.

### Slot 9 is a state toggle

Around 100 actions are of the form `Turn Wi-Fi On`. The state itself lives in a parameter, but which parameter and how its value spells out both vary: older `*.set` actions use `OnValue` with 0/1, Focus uses `Enabled`, App Intents use `state`, and `is.workflow.actions.appearance` uses `style` with `"dark"`/`"light"`. Slot 9 is numbered out of band so it can carry that indirection.

`Action.Toggle` resolves it, and is always populated — with the catalog-wide defaults when the entry overrides nothing:

| field | default | meaning |
| --- | --- | --- |
| `Key` | `state` | the parameter holding the state |
| `Default` | `1` | the `Values` key to use when `Key` is absent from the plist |
| `Values` | `0/1/false/true/off/on → Off/On` | stored value → display text |

An entry's `"$￼9"` object overrides only the fields it names, **except** `dict`, which replaces `Values` wholesale — `appearance` must not inherit `On`/`Off` alongside its own vocabulary. A numeric `default` (`{"default": 0}`) is normalised to the string `"0"`, since it is a key into `Values`. `Toggle.Text(storedValue)` does the lookup and treats `""` as "the parameter was absent".

### Slots 7 and 8 are App Intent entities

App Intent parameters can be structured objects, and the text to show sits at a nested path inside one. Slots 7 and 8 hold a dotted path rather than a value:

```json
{ "name": "Move Shortcuts $￼7 to $￼8",
  "param": { "$￼7": "shortcuts.title.key", "shortcuts": "$￼9",
             "$￼8": "folder.title.key", "folder": "$￼9", "$￼80": "All Shortcuts" } }
```

`Action.Entities` maps the slot to an `Entity` whose `Path` is that reference split on dots — `Path[0]` is the parameter key, available as `Entity.Parameter()`. When the parameter holds an array, resolve the remaining path against each element and show the results together.

`"$￼80"` (and `"$￼70"`) is the **fallback label** for entity slot 8 (7): what to display when the path resolves to nothing. Only one action uses it, `MoveShortcutToFolderAction`, whose folder defaults to "All Shortcuts".

## Colour groups

`Group` is both the group name and the CSS class, because the action markup keeps the class names of the shortcut it was lifted from; `internal/render/assets/actions.css` maps each one to a Radix colour scale for the step badge (see [rendering.md](rendering.md#colour-groups)).

| group | colour | actions |
| --- | --- | --- |
| `gGray` | Gray | 23 |
| `gGreen` | Green | 42 |
| `gYellow` | Yellow | 58 |
| `gOrange` | Orange | 29 |
| `gRed` | Red | 37 |
| `gBlue` | Blue | 80 |
| `gApple` | uncatalogued Apple action | — |
| `gUnknown` | uncatalogued third-party action | — |

The remaining 466 catalogued states have no group and get `GroupDefault` (`""`), which has no class.

`gApple` and `gUnknown` are not in the data; they are for identifiers the catalog does not contain at all. `FallbackGroup(identifier)` picks between them so a missing action reads as "newer than this catalog" rather than "broken" — an intent from an OS release after the extraction looks distinctly different from a third-party app's action. `Groups()` returns all eight in display order for legends, and `Group.ColorName()` gives a human label.

## `DisplayName` for uncatalogued actions

`DisplayName(identifier)` is the **fallback** when `Lookup` misses. Call it only then; it works from the identifier alone. The rules, in order:

1. **Third-party identifiers** (not `is.workflow.actions.` or `com.apple.`) matching `^.+?\..+?\.(.+?)\.(.*)$` render as `$1 > $2`, so `AsheKube.app.a-Shell-mini.ExecuteCommandIntent` becomes `a-Shell-mini > ExecuteCommandIntent`. The leading components are the developer's reverse-DNS prefix and carry nothing a reader wants. The pattern is non-greedy from the left, so it consumes one prefix component at a time: `com.example.dev.MyApp.Nested.DoThing` gives `dev > MyApp.Nested.DoThing`. The app and intent are kept verbatim — they are already the developer's own capitalisation, and guessing at word breaks would mangle them.
2. **Apple identifiers** have their `is.workflow.actions.` or `com.apple.` prefix stripped and the remainder prettified.
3. **Anything else** is prettified as-is.

`IsApple(identifier)` is the prefix test on its own, since the renderer needs it for `FallbackGroup` too.

### Prettifying

Prettifying turns an identifier fragment into spaced Title Case. Segments split on `.`, `_`, and spaces; hyphens are preserved inside a segment, so `display.always-on.set` becomes `Display Always-On Set`.

Each segment then splits into words two different ways:

- **Mixed case** splits on case boundaries, including before the last capital of an acronym run, so `ExecuteCommandIntent` gives three words and `AXToggleColorFilters` keeps `AX` whole.
- **All-lowercase runs** have no boundaries to split on. `getvariable` has to become `Get Variable`, so these are segmented against a vocabulary of ~700 words that appear in Shortcuts identifiers, choosing the cover with the **fewest** words. A run that cannot be covered exactly is left intact: a missing vocabulary word costs a slightly worse label (`Qqzzxx`) and never produces chopped-up noise.

Words are then title-cased, with two exception tables: acronyms that should be upper case (`url` → `URL`, `pdf` → `PDF`) and names whose capitalisation is irregular (`javascript` → `JavaScript`, `wifi` → `Wi-Fi`, `ios` → `iOS`).

The vocabulary is a heuristic that only ever affects actions the catalog does not know, so adding a word is safe and never changes a catalogued action's name. Spot-checked across all 729 real identifiers, it produces `Download URL`, `Run JavaScript On Web Page`, `Get IP Address`, and `Add Frame To GIF`.

## The rest of the tables

**`Condition(code)`** maps a `WFCondition` integer to operator text: 4 → `is`, 999 → `does not contain`. The upstream table stores these ready for HTML (`&lt;`), which would double-escape in a Go template, so they are unescaped on load and this package returns plain text — `Condition(0)` is `<`. Escaping is the renderer's job.

**`Unit(enumeration, code)`** decodes a unit code within `Unit` (durations) or `ByteCountUnit` (sizes). The codes are bit flags, so they are sparse and non-contiguous: `Unit` 8192 is `weeks` while 16 is `days`.

**`Filter(property)`** returns the value type and default for a filter property used by the `Filter Files`-style actions. A `Number` property may name the unit enumeration that decodes its unit code — `File Size` uses `ByteCountUnit`, `Duration` uses `Unit` — which is how the two tables connect.

**`MinimumOSVersion(clientVersion)`** maps `WFWorkflowMinimumClientVersion` to the iOS release that can run the shortcut. Shortcuts bumps that value whenever a shortcut uses a newer feature, and only nine values are known:

| client version | iOS |
| --- | --- |
| 467 | 12 |
| 900 | 13 |
| 1106, 1113 | 15 |
| 2025 | 17 |
| 3010 | 18 |
| 3216 | 18.2 |
| 5001, 5023 | 27 |

The nearest entry at or below the input is returned, so a shortcut saved by a future Shortcuts build reports the newest iOS we know of rather than nothing at all. A value below 467 reports `false`.

**`IgnoredParameters()`** is the set of plist keys that carry structure rather than content — `UUID` (identity), `GroupingIdentifier` and `WFControlFlowMode` (block membership), and `AppIntentDescriptor` (App Intent bookkeeping). A renderer listing an action's parameters should skip them; `IsIgnoredParameter(key)` is the single-key test.

# Notes on the shortcuts-generator reference

`.ai/generate-shortcuts-skill/` holds a copy of the third-party [shortcuts-generator](https://github.com/drewocarr/shortcuts-generator) Claude skill: a set of Markdown files describing the Shortcuts plist format so a model can *write* `.shortcut` files. It was used to find gaps in how `sss` renders shortcuts, and the fixtures `find-filter`, `menu-demo`, `repeat-each`, `malformed-values`, `weather-llm` and `import-questions` in `internal/render/render_test.go` are adapted from its examples.

It is a useful map of the format, but in several places it is wrong, and where it disagrees with this codebase the code is right. This file records those places so nobody "fixes" the code to match the reference.

## Where the reference is wrong

| topic | the reference says | what is actually true | evidence |
| --- | --- | --- | --- |
| `WFItemType` in a dictionary item (`PARAMETER_TYPES.md`) | 1 = Number, 3 = Dictionary | 1 = Dictionary, 3 = Number, and 5 = a variable holding the whole value | `dictionaryItemValue` in `internal/render/value.go`, reimplemented from the Shortcut Source Tool, which renders real shortcuts |
| Icon colour (`PLIST_FORMAT.md`) | unsigned ARGB integers; its colour table gives Blue and Red the same value | RGBA with alpha in the low byte, written as a **signed** 32-bit integer in exports | [archive.md](archive.md), [extraction.md](extraction.md) |
| `WFDictionaryFieldValueItems` (`VARIABLES.md`) | listed as a serialization type | a key inside a `WFDictionaryFieldValue` value | every dictionary in a real export |
| `WFCondition` (`CONTROL_FLOW.md`) | strings such as `"Equals"`, `"Contains"` | integer operator codes (`4` is, `99` contains, `100` has any value, …) from `internal/catalog/data/conditions.json` | every `If` in a real export; FILTERS.md's own operator table uses the integers |
| The `Album` filter (`FILTERS.md`) | a `String` property | typed `Enumeration` (default `Recents`) in the catalog | `internal/catalog/data/filters.json`; the renderer accepts the value under either key |
| Date unit for "days" (`FILTERS.md`) | TBD | `16`; hours, minutes and seconds are `32`, `64`, `128` | `internal/catalog/data/units.json` |

`WFControlFlowMode` must also be an integer: the reference says so itself, and calls a string mode the most common mistake. The renderer flags both string modes and string conditions rather than guessing ([rendering.md](rendering.md#values-of-the-wrong-type-are-flagged-not-coerced)).

## The action and AppIntent lists are not an identifier source

`ACTIONS.md` and `APPINTENTS.md` claim 427 actions and 728 intents, but most identifiers in them are guessed from Objective-C class names (`WFTranslateTextAction` → `translatetext`). That contradicts the reference's own note that the mapping is irregular, and the guesses mostly do not match what real shortcuts contain:

- Only 150 of their 428 `is.workflow.actions.*` identifiers appear in the embedded 729-entry catalog, and many of the rest are in it under their real names.
- Only 8 of the 161 distinct AppIntent names match a catalog entry. The others are either named differently (`ToggleAxVoiceOverIntent` vs the real `AXToggleVoiceOverIntent`) or written as wildcards.
- Neither file gives per-action parameter labels, except hand-written tables for `downloadurl`, `filter.photos` and `deletephotos`, all of which are already catalogued.

So the catalog ([catalog.md](catalog.md)) is not extended from them. Some examples of the guessed identifier and the real one:

| reference | real identifier (`is.workflow.actions.` omitted) |
| --- | --- |
| `translatetext` | `text.translate` |
| `createfolder` | `file.createfolder` |
| `deletefile` | `file.delete` |
| `makearchive` / `extractarchive` | `makezip` / `unzip` |
| `expandurl` | `url.expand` |
| `getwebpage` | `getwebpagecontents` |
| `runosascript` | `runapplescript` |
| `batterylevel` | `getbatterylevel` |
| `getdictionaryvalue` | `getvalueforkey` |
| `calculate` | `math` |
| `randomnumber` | `number.random` |
| `timeuntildate` | `gettimebetweendates` |
| `setappearance` | `appearance` |
| `setwifi` | `wifi.set` |
| `toggledonotdisturb` | `dnd.set` |

## What it got right and is now used

- The filter operator and unit tables in `FILTERS.md` match the catalog, and its example filter shapes (boolean, enumeration wrapped in `WFStringSubstitutableState`, `String`-valued album, "is today", "is in the last") are what drove the Find/Filter fixes in [rendering.md](rendering.md#conditions).
- `VARIABLES.md`'s built-in variable sources (`CurrentDate`, `Clipboard`, `DeviceDetails`, `Ask`) are now spelled the way the editor shows them.
- `PLIST_FORMAT.md`'s top-level keys led to showing import questions, input and output types, and the output-fallback flag in the web review and README, and to scanning import questions for credentials.

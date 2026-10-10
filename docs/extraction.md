# How shortcuts are extracted

This document explains `internal/shortcuts`, the package that reads the local Apple Shortcuts library. Everything that knows about Apple's private database schema lives there, so that a macOS update breaking the schema only breaks one package.

## Why we read the database at all

macOS ships no way to export a shortcut from the command line. The `shortcuts` binary offers exactly four subcommands — `run`, `list`, `view`, and `sign` — and none of them dump a shortcut's contents. The Shortcuts app can export one shortcut at a time through a share sheet, which does not scale to a library of a couple hundred.

So the only viable source is the Core Data store the app writes to:

```
~/Library/Shortcuts/Shortcuts.sqlite
```

Reading it requires that your terminal has **Full Disk Access** (System Settings → Privacy & Security → Full Disk Access). Without it, `Open` returns an error wrapping `ErrNoAccess` that says so, rather than surfacing a bare SQLite error.

## The WAL trap

This is the single most important detail in the package, and getting it wrong loses data silently.

Shortcuts keeps the database in [WAL mode](https://sqlite.org/wal.html) and leaves recent writes sitting in the `Shortcuts.sqlite-wal` sidecar, sometimes for a long time — the sidecar was 2.5 MB when this was investigated. The obvious way to read a database you do not own is SQLite's `immutable=1` flag, which promises the file will not change and lets SQLite skip locking:

```
file:/…/Shortcuts.sqlite?immutable=1
```

**`immutable=1` also tells SQLite to ignore the WAL.** Measured on a real library, that read returned 188 shortcuts while the same database read correctly returned 189. The missing one was a shortcut created minutes earlier — precisely the shortcut a user is most likely to be trying to back up.

`Open` therefore copies `Shortcuts.sqlite` **together with its `-wal` and `-shm` sidecars** into a temporary directory and opens the copy with `mode=ro`, which lets SQLite replay the WAL normally. The sidecars are optional: a cleanly checkpointed database has none, and their absence is not an error.

Copying rather than reading in place also means a concurrently running Shortcuts app cannot checkpoint the WAL out from under us mid-read. `Close` removes the snapshot.

`TestSnapshotMatchesAWALAwareReadOfTheLiveDatabase` asserts the snapshot holds exactly the same shortcuts, by workflow ID, as a read-only (`mode=ro`) connection to the live database, which does honour the WAL. Comparing against the deliberately-wrong `immutable=1` read instead does not work: the WAL holds deletions as well as additions, so a shortcut deleted since the last checkpoint is still visible to the WAL-ignoring read and the correct snapshot legitimately sees *fewer* shortcuts. If `Open` ever stopped carrying the WAL across, the snapshot would match the `immutable=1` read and differ from the live one, and the test fails whenever the WAL holds any change to the shortcut list.

## Field mapping

A `.shortcut` file is a plist dictionary. Every key in it has a database source, verified by round-tripping a real shortcut:

| plist key | source |
| --- | --- |
| `WFWorkflowActions` | `ZSHORTCUTACTIONS.ZDATA` — a binary plist array |
| `WFWorkflowIcon.WFWorkflowIconGlyphNumber` | `ZSHORTCUTICON.ZGLYPHNUMBER` |
| `WFWorkflowIcon.WFWorkflowIconStartColor` | `ZSHORTCUTICON.ZBACKGROUNDCOLORVALUE` |
| `WFWorkflowClientVersion` | `ZSHORTCUT.ZLASTMIGRATEDCLIENTVERSION` |
| `WFWorkflowMinimumClientVersion(String)` | `ZSHORTCUT.ZMINIMUMCLIENTVERSION` |
| `WFWorkflowInputContentItemClasses` | `ZSHORTCUT.ZINPUTCLASSESDATA` — a binary plist |
| `WFWorkflowOutputContentItemClasses` | `ZSHORTCUT.ZOUTPUTCLASSESDATA` — a binary plist |
| `WFWorkflowImportQuestions` | `ZSHORTCUT.ZIMPORTQUESTIONSDATA` — a binary plist |
| `WFWorkflowHasOutputFallback` | `ZSHORTCUT.ZHASOUTPUTFALLBACK` |
| `WFWorkflowHasShortcutInputVariables` | `ZSHORTCUT.ZHASSHORTCUTINPUTVARIABLES` |
| `WFWorkflowTypes` | derived from flags — see [archive.md](archive.md) |
| `WFQuickActionSurfaces` | no known source; written as `[]` |

Rows are filtered to `ZTOMBSTONED = 0` (deleted-but-not-yet-purged shortcuts) and to rows that actually have a name and an actions row. `ZHIDDENFROMLIBRARYANDSYNC = 1` marks shortcuts hidden from the library; they are returned with `Hidden` set so the caller can decide, and are excluded by default.

### Fidelity

Decoding `ZSHORTCUTACTIONS.ZDATA` for a 485-action shortcut produced a structure that compares equal to a real Shortcuts export of the same shortcut. The only difference across the entire 419 KB payload was `WFRepeatCount: 5.0` versus `5` — an artefact of how the reference file was serialised to JSON, not a difference in the data. Extraction is faithful.

`TestExtractedActionsMatchTheReferenceExport` in `internal/integration` keeps checking this against the export in `.ai/`, which is gitignored and never refreshed automatically. The installed copy of the reference shortcut keeps getting edited, so an exact comparison would eventually fail on an edit rather than on a bug. The test therefore requires an exact match only when the installed shortcut has not been modified since the export file was written. When it has, the test trims the actions that match at both ends and allows the difference to be one contiguous block of at most 20 actions, which is what an edit looks like. Every other action must still match exactly, and the log names the edited block. A larger or scattered difference fails with a request to re-export.

### `ZACTIONCOUNT` is unreliable

The column reads `0` for some shortcuts that plainly have actions. `Load` overwrites `ActionCount` with the real decoded length; `List` reports the column value, which is good enough for a picker and avoids decoding every shortcut just to render a list.

## Timestamps

`ZCREATIONDATE` and `ZMODIFICATIONDATE` are Core Data timestamps: seconds (with a fractional part) since **2001-01-01 UTC**, not the Unix epoch. `coreDataTime` converts them and returns local time. A `NULL` column becomes the zero `time.Time`.

## Icon colour

`ZBACKGROUNDCOLORVALUE` is a 32-bit colour, and two things about it are easy to get wrong.

**It is RGBA, not ARGB.** The alpha byte is the *low* byte. Across all 25 distinct values in a real library, the low byte was only ever `0x00` or `0xFF` while the high byte took 13 different values — which settles it. Decoding as `(value >> 8) & 0xFFFFFF` yields exactly Apple's stock palette:

| hex | colour |
| --- | --- |
| `#FF4351` | red |
| `#FD6631` | orange |
| `#FE9949` | light orange |
| `#FEC418` | yellow |
| `#FFD426` | yellow |
| `#19BD03` | green |
| `#55DAE1` | teal |
| `#1B9AF7` | light blue |
| `#3871DE` | blue |
| `#7B72E9` | purple |
| `#DB49D8` | magenta |
| `#ED4694` | pink |
| `#B4B2A9` | taupe |
| `#A9A9A9` | grey |

**The sign is inconsistent.** The same colour appears in the column as both a positive and a negative integer depending on when it was written — `4292093695` and `-2873601` are both `0xFFD426FF`. So the value must be masked to 32 bits rather than interpreted as signed or unsigned. `Icon.Color` is a `uint32` holding the masked value; writing it back into a plist requires converting to a signed `int32`, which is what real exports contain.

Values of `0` and `255` also occur and must degrade gracefully rather than being treated as a real colour.

## Folders

Folder membership is **not** available. `ZCOLLECTION` holds the folder names, but there is no join table linking shortcuts to collections — `ZLIBRARY` turns out to be CloudKit sync metadata, not folder structure. The ordering appears to live inside an opaque `ZLASTREMOTESHORTCUTORDERINGDATA` blob.

If folder grouping is wanted later, the tractable route is shelling out to `shortcuts list --folder-name <name>` per folder and intersecting by name. Out of scope for now.

## What the system does not provide

Two things you might expect to find on disk and will not:

- **An action catalog.** `WorkflowKit.framework`'s `Localizable.loctable` has 5042 keys but only one mentioning an `is.workflow.actions.*` identifier, and there is no `Actions.plist`. Human-readable action names come from a vendored catalog instead — see [catalog.md](catalog.md).
- **Step-by-step descriptions.** `ZSHORTCUT.ZSTEPBYSTEPDESCRIPTION` exists and holds an `NSKeyedArchiver` array of prose descriptions, but it was populated for only 3 of 189 shortcuts. Not dependable.

## Cross-checking against the CLI

`ZWORKFLOWID` matches the UUIDs printed by `shortcuts list --show-identifiers` exactly, which makes it a stable primary key and gives a cheap way to sanity-check extraction against Apple's own view of the library.

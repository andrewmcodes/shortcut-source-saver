# How a shortcut is written to disk

This document explains `internal/archive`, the package that turns a `shortcuts.Workflow` read out of the database into its on-disk form. It is the counterpart to [extraction.md](extraction.md): that document covers getting the data out, this one covers what we do with it.

Each shortcut becomes a directory named by `Slug(name)` holding three files:

| file | what it is |
| --- | --- |
| `shortcut.json` | canonical JSON — the git-diffable source of truth |
| `shortcut.shortcut` | the signed binary, importable by Shortcuts.app |
| `metadata.json` | a small human- and grep-readable summary |

## The plist dictionary

`BuildWorkflow` assembles the dictionary that becomes a `.shortcut` file. The key mapping was verified by round-tripping a real export; the per-key database sources are tabulated in [extraction.md](extraction.md#field-mapping).

Two keys deserve explanation.

### `WFWorkflowIconStartColor` must be a signed int32

`ZBACKGROUNDCOLORVALUE` stores the same colour as both a positive and a negative integer depending on when the row was written, so `shortcuts.Icon.Color` holds it masked to 32 bits as a `uint32`. Real exports contain the **signed** reading of those same bits: the database's `0xFFD426FF` appears in an exported file as `-2873601`. `signedIconColor` does that conversion. Writing the unsigned value produces a file Shortcuts still imports, but with the wrong icon colour.

`HexColor` is the other direction, for `metadata.json`: the packing is RGBA, so the **low** byte is alpha and has to be shifted off rather than kept as the blue channel — hence `(color >> 8) & 0xFFFFFF`. The evidence for RGBA over ARGB is in [extraction.md](extraction.md).

### `WFWorkflowTypes` is derived, and best-effort

There is no column for it. It records which surfaces a shortcut is offered on, so `WorkflowTypes` reconstructs it from the flags that imply each surface, in this order:

| entry | condition |
| --- | --- |
| `ActionExtension` | `len(InputContentItemClasses) > 0` — it accepts input, so it can appear in the share sheet |
| `NCWidget` | `!HiddenFromWidget` |
| `WFWorkflowTypeShowInSearch` | `ShowInSearch` |

This is a guess, and deliberately a cheap one: Shortcuts.app recomputes the array when it imports the file, so being wrong costs nothing beyond a slightly noisier diff.

`WFQuickActionSurfaces` is written as an empty array. It is present in every export and has no counterpart anywhere in the database.

`WFWorkflowMinimumClientVersion` is the string form parsed as an integer, and is **omitted** when it does not parse (some rows hold dotted versions like `2.2.1`). Writing a placeholder `0` would be a claim we cannot support; omitting the key lets Shortcuts fill it in.

## Canonical JSON

`shortcut.json` is the file a human reads and git diffs, so its bytes have to be a pure function of the shortcut's contents. `CanonicalJSON` guarantees that with: sorted keys (`encoding/json` sorts map keys for us), a fixed two-space indent, and exactly one trailing newline.

It also sets **`SetEscapeHTML(false)`**. Shortcut text routinely contains `<`, `>` and `&` — URLs, HTML snippets, "if x > 5" comparisons — and Go's default encoder rewrites all three as unicode escapes. That makes the file unpleasant to read and its diffs almost useless.

### Tagged `data` and `date` values

`howett.net/plist` decodes `<data>` to `[]byte` and `<date>` to `time.Time`, neither of which survives a JSON round trip usefully: a byte slice becomes an array of numbers, and a timestamp's format is a guess. `Canonicalize` rewrites them recursively into tagged objects before marshalling, matching the convention the reference tooling uses:

```json
{ "__type__": "data", "__value__": "3q2+7w==" }
{ "__type__": "date", "__value__": 1709294400 }
```

Plist integers decode as `uint64`, which becomes a plain JSON number (as an `int64` when it fits, so it renders without surprises). `canonicalizeByKind` is the reflection fallback for anything else the decoder hands back: named types over JSON-safe kinds such as `plist.UID`, typed slices and maps, named byte slices, and nil pointers.

Nothing reads the tagged form back yet. When something needs to (an import or verify command), the decoder belongs in this package next to `Canonicalize`.

### Known limits of the tagged form

Two plist values do not survive into canonical JSON exactly:

- **`plist.UID` becomes a bare integer.** It is a named `uint64`, so `canonicalizeByKind` emits it as a number indistinguishable from a real integer.
- **Dates lose sub-second precision**, because the tag stores `time.Unix()`.

Neither matters today. UIDs only occur inside `NSKeyedArchiver` archives, and Shortcuts stores those as opaque `<data>` blobs, which are tagged and preserved byte for byte. A scan of a real 219-shortcut library found 20 `<data>` values, and no UIDs or dates at all. And the signed `.shortcut` is built from the decoded plist, not from the JSON, so nothing importable is affected.

Fixing either would change `shortcut.json` only for shortcuts that contain such a value, so it would not churn an ordinary library. It is worth doing at the same time as the decoder mentioned above, since that is the first point at which the distinction can be lost in practice.

## Why `WriteJSON` reports whether anything changed

`WriteJSON` returns `changed bool`, and does not touch the file when the bytes it would write are already there. That is not a micro-optimisation — it exists to gate signing.

**`shortcuts sign` is not deterministic.** Its output is an AEA1 encrypted archive, and the bytes differ on every invocation for byte-identical input. It is also roughly six times the size of the JSON. So signing unconditionally on every run would add a fresh multi-megabyte binary blob to git history each time, for shortcuts nobody had edited, and the repository would grow without bound while the diffs showed nothing meaningful.

`Sign` therefore skips its work entirely when `jsonChanged` is false and the output file already exists. The canonical JSON is the change detector for the signed artefact: if the JSON is identical, the signed file that was produced from it is still valid, whatever its bytes happen to be.

The consequences to keep in mind:

- Any change to `CanonicalJSON`'s output — key tagging, indentation — invalidates every signed file at once and re-signs the whole library on the next run.
- A signed file deleted by hand is regenerated, because the skip requires the file to exist.
- A corrupted signed file is **not** detected. Nothing validates it; recovery is to delete it.

Both writes are atomic (temp file in the destination directory, then rename) and create parent directories, so an interrupted run cannot leave a half-written `shortcut.json` that a later run would treat as the previous state.

`Sign`'s share mode accepts `anyone` or `people-who-know-me`, and `NormalizeMode` falls back to `anyone` for anything else rather than failing. A mistyped flag should not abort a backup of two hundred shortcuts. `shortcuts sign`'s stderr is folded into the returned error, because its exit codes on their own say very little.

## Directory names

`Slug` has to survive real shortcut names, which contain emoji, slashes, `?`, `…` and non-Latin script. It keeps `[a-z0-9]`, lowercases `A-Z`, collapses every other ASCII run into a single `-`, trims the edges, and truncates at 80 bytes to stay well inside the 255-byte limit filesystems impose on one path component.

Non-ASCII runes are **dropped**, not transliterated — there is no correct ASCII spelling for most of them and no dependency worth adding for the ones there is. Two consequences: `Café Order` slugs to `caf-order`, and a name made entirely of emoji or CJK slugs to nothing, in which case `Slug` returns the literal `shortcut` so callers never have to handle an empty path component.

Names are not unique in a Shortcuts library, so slugs are not either. Disambiguation is the caller's problem; the workflow ID is the stable key.

## Status in `metadata.json`

A backup keeps every shortcut it has ever written, so `metadata.json` records whether the shortcut is still live. Two optional keys carry it, and both are absent for an active shortcut:

| key | value |
| --- | --- |
| `status` | `"conflict"` for a sync-conflict copy the Shortcuts app hides, `"deleted"` for a shortcut no longer in the library |
| `conflict_of` | `{"name": ..., "workflow_id": ...}` naming the shortcut a conflict copy was split from |

```json
{
  "action_count": 12,
  "color": "#FF4351",
  "conflict_of": {
    "name": "get_apple_app",
    "workflow_id": "D93F4368-D3D6-4695-A5CB-2EFE615A348D"
  },
  "created": "2024-03-01T09:12:44-08:00",
  "glyph": 59511,
  "name": "_Get App from Input",
  "status": "conflict",
  "workflow_id": "55992F98-1B4D-44CF-94C8-9314FF434579",
  ...
}
```

Absent means active, rather than writing `"status": "active"`, so that adding the field did not rewrite the `metadata.json` of every shortcut already backed up. `status` is a single value: a conflict copy that is later deleted becomes `"deleted"` and keeps its `conflict_of`, so the history stays readable. `conflict_of` is omitted when the database points at an original row that no longer exists. Its `name` is the original's name at the time of the last write, so renaming the original rewrites the copy's metadata on its next backup.

`NewMetadata` fills both keys from `Workflow.Status` and `Workflow.ConflictOf`, so conflict copies are marked by the same write that backs them up, and `sss export` includes the marking because it reuses `NewMetadata`. `"deleted"` is never produced here: a deleted shortcut cannot be loaded from the library, so it is set by the backup run on the files already on disk — see [backup.md](backup.md#shortcuts-that-are-no-longer-active). `Metadata.ShortcutStatus` and `Metadata.Original` convert the keys back into the `shortcuts` types.

## Tests

`internal/archive/archive_test.go` is table-driven and needs neither the Shortcuts database nor the `shortcuts` binary. The load-bearing cases:

- `TestSignSkipsWhenJSONUnchangedAndOutputExists` pins the skip described above. It passes without `shortcuts` installed by construction — if signing were attempted, the call would fail.
- `TestSignRunsWhenOutputMissing` does the real thing, and skips via `exec.LookPath` when the binary is absent.
- `TestWriteJSONChangeDetection` asserts the unchanged file's mtime does not move, which is stronger than checking the returned flag, and that no temp files leak.
- `TestCanonicalJSONDoesNotEscapeHTML` and `TestCanonicalJSONIsDeterministic` guard the two properties the git-diffability rests on.

## Canonicalising structs

`Canonicalize` handles maps, slices, scalars, `[]byte` and `time.Time` explicitly. Structs are converted by marshalling to JSON and back, which honours `json` tags and recurses into nested fields.

Returning the struct untouched, as an earlier version did, meant a `[]byte` or `time.Time` nested inside a struct field bypassed the tagging that top-level values receive. Nothing in the current code hits that case — plist decoding only ever produces maps, slices, scalars, `[]byte` and `time.Time`, so the struct branch is reached solely by `WriteMetadata` — but the inconsistency was a trap for the next caller.

One visible consequence: `metadata.json` keys are now emitted in alphabetical order rather than struct-declaration order, because the value passes through a map. The content is identical, and sorted keys match the convention every other generated JSON file in the repository already follows.

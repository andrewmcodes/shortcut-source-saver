# The backup run

This document explains `internal/backup`, which orchestrates a run: read a shortcut, guard it, write it, regenerate the site, commit.

## Repository layout

```
shortcuts/
  <slug>/
    shortcut.json        canonical JSON, the diffable source of truth
    metadata.json        name, id, icon, action count, dates, versions
    <Name>.shortcut      signed and importable
    README.md            generated documentation
docs/                    the generated site (site_dir)
  index.html
  shortcuts/<slug>.html
  assets/style.css
```

## Directory names must be unique, and shortcut names are not

Slugging a shortcut name is lossy, and Apple places no uniqueness constraint on names. Both problems are real in an ordinary library — a 188-shortcut library produced three collisions:

| slug | colliding names |
| --- | --- |
| `shortcut-source-tool` | `Shortcut Source Tool`, `Shortcut - Source Tool` |
| `shortcut-source-helper` | `Shortcut Source Helper`, `Shortcut - Source Helper` |
| `summarize-pdf` | `Summarize PDF`, `Summarize PDF` |

The first two pairs are genuinely different shortcuts that differ only by punctuation the slug discards. The third is two distinct shortcuts with the same name, which Shortcuts allows.

Without handling, each collision silently overwrites the previous shortcut's backup: the run reports success for every shortcut while writing fewer files than it claims. That is the worst class of bug for a backup tool, so it is worth the ugliness of the fix.

`ResolveSlugs` therefore assigns names over the **whole library at once**. A slug used by exactly one shortcut is used as-is. When two or more shortcuts share a slug, **every** member of that group gets the first 8 hex digits of its `WorkflowID` appended:

```
shortcut-source-tool-540bfd88
shortcut-source-tool-857a73fd
summarize-pdf-071a045c
summarize-pdf-9dea5922
```

Properties this gives:

- **Deterministic.** The suffix comes from the shortcut's own identifier, so it does not depend on iteration order, and a re-run produces identical paths. `TestResolveSlugsIsStableAcrossCalls` asserts this by shuffling the input.
- **Collision-free.** `WorkflowID` is unique and matches `shortcut list --show-identifiers`.
- **Unobtrusive.** 185 of 188 directories keep a clean name.

Collisions are resolved against the full library including hidden shortcuts, so passing `--include-hidden` cannot change the directory a visible shortcut lands in.

The one wart: if you delete one of a colliding pair, the survivor's slug becomes unique again and its directory is renamed on the next run. That shows up as a rename in git, which is recoverable and rare.

## Signed file names carry the shortcut's name

The `.shortcut` file is named after the shortcut, not a fixed `shortcut.shortcut`. This is not cosmetic: **the plist contains no name field**, so Shortcuts derives the imported shortcut's name from the filename. A fixed filename would import every shortcut as "shortcut".

Only path-hostile characters are replaced (`/`, `\`, `:`, control characters); spaces, emoji, and case are preserved so the import reads correctly. The README link is URL-escaped accordingly.

## Order of operations

1. **Load** the shortcut's actions.
2. **Scan** the actions and the import questions for credentials, *before writing anything*. A hit fails that shortcut with a redacted summary unless `--allow-secrets` is set. Guarding before the write means a run that would leak never touches the repo. See [secret-scanning.md](secret-scanning.md).
3. **Write** `shortcut.json` first, because its `changed` result drives everything after it.
4. **Write** `metadata.json` and `README.md`, each only when the contents differ, so an unchanged shortcut produces no filesystem churn.
5. **Sign** — skipped entirely when the JSON did not change. See [archive.md](archive.md) for why this matters.
6. **Regenerate the site** from the repository.
7. **Stage and commit** the touched paths, unless `--no-commit` or `auto_commit = false`.

Writes go through a temp file and a rename, so an interrupted run cannot leave a half-written `shortcut.json` in the repo.

## The site is built from the repository, not the library

`BuildSite` reads `shortcuts/*/shortcut.json` and `metadata.json` back off disk and reconstructs enough of a workflow to render, rather than rendering from the live Shortcuts database.

This means `sss build` regenerates every web review without touching the Shortcuts database, so it works on a machine without Full Disk Access, and shortcuts that have since been deleted from the library still appear in the site as long as their backup is committed. The cost is that a few fields not persisted in `metadata.json` are reconstructed approximately.

The one the site does render is the workflow-types row, which is read back out of `WFWorkflowTypes` in `shortcut.json`. `NCWidget` is present only when a shortcut is *not* hidden from the widget, so a reloaded workflow starts with `HiddenFromWidget = true` and the presence of `NCWidget` clears it. Starting from Go's zero value instead would put every widget-hidden shortcut back in the widget on the next `sss build`.

## Outcomes

Each shortcut reports one of:

| outcome | meaning |
| --- | --- |
| `written` | the canonical JSON changed and was rewritten |
| `unchanged` | the JSON was byte-identical; nothing was rewritten |
| `failed` | loading, scanning, or writing failed; the error is reported |
| `skipped` | the run was cancelled before reaching this shortcut |

A run where everything is `unchanged` creates no commit and leaves the working tree clean. That property is the observable proof that the signing skip works, and it is worth checking after any change to this package.

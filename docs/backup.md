# The backup run

This document explains `internal/backup`, which orchestrates a run: read a shortcut, guard it, write it, regenerate the site, commit.

## Repository layout

```
shortcuts/
  <slug>/
    shortcut.json        canonical JSON, the diffable source of truth
    metadata.json        name, id, icon, action count, dates, versions, status
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

### Directories already in the destination are claimed

Backups of deleted shortcuts are never removed, so the destination holds directories for shortcuts that are no longer in the library. Each one's `metadata.json` names the workflow id that owns it. `ResolveSlugsAgainst` starts from `ResolveSlugs` and then applies those claims, in this order:

1. If the shortcut already owns the plain slug of its current name, it keeps it.
2. Otherwise, if it already owns the suffixed form (`<slug>-<first 8 hex of its id>`), it keeps that.
3. Otherwise, if the slug it would get is owned by a different workflow id, it takes the suffixed form instead (and the full id if even that is taken).

Rule 3 is the important one: a new shortcut named like a deleted one would otherwise land in the deleted one's directory and overwrite its `shortcut.json`, `metadata.json` and `.shortcut`, destroying exactly the old version the backup exists to keep. Rules 1 and 2 remove the old wart where deleting one of a colliding pair made the survivor's slug unique again and moved it to a fresh directory. A directory whose `metadata.json` has no `workflow_id` claims nothing.

A renamed shortcut is never blocked by its own old directory: rule 3 only yields to a *different* workflow id, so the new name's slug is free and the old directory is moved there, as described next.

### Renamed shortcuts move their directory

When a workflow id that is still in the library owns a directory other than the one its current name resolves to, the shortcut was renamed (or a collision changed its slug), and the old directory is a stale duplicate. The user does not want to see it, and its content stays in git history, so the backup moves it instead of leaving it behind:

- **When the shortcut is backed up** and its new directory does not exist yet, `adoptPreviousDirs` renames the old directory to the new slug with `os.Rename` before writing, so git records a rename and the new files overwrite the moved ones. If the new directory already exists, the old one is removed. Either way the run records a `Rename`.
- **The signed file is carried over, not re-signed.** Its name follows the display name, so a rename needs a file under the new name, but `shortcuts sign` is non-deterministic. When `shortcut.json` is unchanged and no file exists under the new name, the existing `.shortcut` is renamed to it and signing is skipped as usual; when the JSON changed, the new file is signed and the old one removed. Any other `.shortcut` in the directory is removed. The same rule fixes a rename that keeps the slug (for example a change of case), which previously re-signed and left the old file behind.
- **Stale duplicates of shortcuts this run did not back up** (hidden, or not selected) are cleaned up by `ReconcileStatus`: when the directory for the shortcut's current slug exists and is owned by its workflow id, every other directory with that workflow id is removed. If the current directory does not exist yet, nothing moves until a run backs the shortcut up, because only a backup can write the new name's content.
- **Never touched:** directories whose workflow id is not in the library (deleted shortcuts) and directories without a readable workflow id.
- **The old site page** `<site_dir>/shortcuts/<old-slug>.html` is removed by `BuildSite`, so the site does not keep a page for a directory that no longer exists.

Removed paths are staged with `git rm -r --cached --ignore-unmatch` (`Repo.Remove`) before the new files are added, so the commit shows the move as a rename. The next run finds nothing to move, so it is idempotent. Renames are printed and listed in the commit message as `Renamed: Old Name → New Name`; a move that kept the display name prints `Name (old-slug → new-slug)`.

The claims are read once, before the run writes anything, so slugs stay stable within a run. Slugs in an `sss export` zip still come from the library alone, because a zip has no existing directories to protect.

## Shortcuts that are no longer active

The backup keeps every shortcut it has ever written, and records in `metadata.json` which of them are no longer live, so the site can mark, filter and separate them. The shape of the `status` and `conflict_of` keys is in [archive.md](archive.md#status-in-metadatajson). There are two kinds.

**Sync-conflict copies** are still in the library but hidden by the Shortcuts app (see [extraction.md](extraction.md#sync-conflict-copies)). They are **backed up by default**, because they are often the only copy of an old version, and marked `"status": "conflict"` by the same write that backs them up. They take part in slug assignment like any other shortcut, so a conflict copy with the same name as its original suffixes both, as before.

**Deleted shortcuts** have a directory in the destination but no row in the library: absent entirely, or tombstoned. Backup never removes their files; `ReconcileStatus` marks them `"status": "deleted"`.

`ReconcileStatus` runs once at the end of every run that backed up at least one shortcut, before the site is built, in the repository, `--no-commit` and `--output` modes alike. It walks every `shortcuts/*/metadata.json` with a workflow id and compares it against the **whole** library as `List` returns it, hidden shortcuts included, never against the shortcuts this run selected. A shortcut excluded by `--include-hidden` being off, by `sss backup <names>`, or by the picker is still in the library, so it is not deleted. For each directory:

| workflow id | recorded status |
| --- | --- |
| in the library, a conflict copy | `conflict`, with `conflict_of` refreshed from the library |
| in the library, otherwise | none |
| not in the library | `deleted`, keeping any `conflict_of` |

Only `status` and `conflict_of` are touched, and `metadata.json` is rewritten only when one of them differs from what is on disk, so two runs in a row leave the working tree clean. A shortcut that reappears (undeleted, or synced back from another device) has its status cleared on the next run whether or not that run backs it up. A status change never rewrites `shortcut.json`, and signing is driven solely by `shortcut.json` changing, so it never re-signs a `.shortcut`.

It returns one `StatusChange` per directory whose status differs from what was on disk before the run started, so a shortcut that this run both restored and backed up is still reported as restored. The run prints them, and the commit message adds a section for each kind:

```
chore(shortcuts): back up 2 shortcuts

- Daily Note
- Encode URL

Renamed:
- Foo → Bar

Marked deleted (no longer in the library):
- Old Helper

Restored (back in the library):
- Came Back

Marked as sync-conflict copies:
- _Get App from Input
```

A conflict copy backed up for the first time is not a change; existing backups of conflict copies are reported once, on the first run after upgrading. `sss build` has no library to compare against, so it renders whatever status the files already record.

`sss export` reads only the library, so its zip contains conflict copies marked `conflict` (via `NewMetadata`) and never contains deleted shortcuts.

### What the site receives

`BuildSite` reloads each workflow from disk and copies the recorded status onto it, so every `render.Entry` carries it without `render.Entry` changing:

- `entry.Workflow.Status` is a `shortcuts.Status`: `StatusActive` (`""`), `StatusConflict` or `StatusDeleted`.
- `entry.Workflow.ConflictOf` is a `shortcuts.Original{WorkflowID, Name}`, the zero value when there is none. The original's own entry, when it is in the destination, is the one whose `Workflow.WorkflowID` matches.

## Signed file names carry the shortcut's name

The `.shortcut` file is named after the shortcut, not a fixed `shortcut.shortcut`. This is not cosmetic: **the plist contains no name field**, so Shortcuts derives the imported shortcut's name from the filename. A fixed filename would import every shortcut as "shortcut".

Only path-hostile characters are replaced (`/`, `\`, `:`, control characters); spaces, emoji, and case are preserved so the import reads correctly. The README link is URL-escaped accordingly.

## Order of operations

1. **Load** the shortcut's actions.
2. **Scan** the actions and the import questions for credentials, *before writing anything*. A hit fails that shortcut with a redacted summary unless `--allow-secrets` is set. Guarding before the write means a run that would leak never touches the repo. See [secret-scanning.md](secret-scanning.md).
3. **Move** a directory left under the shortcut's previous slug to its current one. See [above](#renamed-shortcuts-move-their-directory).
4. **Write** `shortcut.json` first, because its `changed` result drives everything after it.
5. **Write** `metadata.json` and `README.md`, each only when the contents differ, so an unchanged shortcut produces no filesystem churn.
6. **Sign** — skipped entirely when the JSON did not change, after carrying over a signed file left under the shortcut's old name. See [archive.md](archive.md) for why this matters.
7. **Record status**: remove stale duplicates, mark deleted shortcuts and clear reappeared ones, once per run. See [below](#shortcuts-that-are-no-longer-active).
8. **Regenerate the site** from the repository.
9. **Stage and commit** the touched and removed paths, unless `--no-commit`, `auto_commit = false`, or `--output` (which never commits; see below).

Writes go through a temp file and a rename, so an interrupted run cannot leave a half-written `shortcut.json` in the repo.

## The site is built from the repository, not the library

`BuildSite` reads `shortcuts/*/shortcut.json` and `metadata.json` back off disk and reconstructs enough of a workflow to render, rather than rendering from the live Shortcuts database.

This means `sss build` regenerates every web review without touching the Shortcuts database, so it works on a machine without Full Disk Access, and shortcuts that have since been deleted from the library still appear in the site as long as their backup is committed. The cost is that a few fields not persisted in `metadata.json` are reconstructed approximately.

The one the site does render is the workflow-types row, which is read back out of `WFWorkflowTypes` in `shortcut.json`. `NCWidget` is present only when a shortcut is *not* hidden from the widget, so a reloaded workflow starts with `HiddenFromWidget = true` and the presence of `NCWidget` clears it. Starting from Go's zero value instead would put every widget-hidden shortcut back in the widget on the next `sss build`.

## Backing up into a plain directory (`--output`)

`sss -o DIR` and `sss backup -o DIR` write a backup into any directory, with no config file, no git repository, and no commit. It exists for "I just want the files here" — a one-off copy, a folder to zip and send, a backup on a machine where nothing has been set up.

```sh
sss backup --all -o .
sss backup "Daily Note" "Encode URL" --output ./a/folder
sss -o ~/Desktop/shortcuts
```

### Why `-o/--output` and not a positional argument or `--inline`

- `-o/--output` is the name nearly every CLI uses for "where results go" (`go build -o`, `curl -o`, `gcc -o`, `pandoc -o`), so it needs no explaining. [clig.dev](https://clig.dev/#arguments-and-flags) recommends standard flag names over invented ones, and a flag over a second positional because positionals are hard to tell apart.
- `backup` already uses its positional arguments for shortcut names. A destination positional would make `sss backup Foo bar` ambiguous: is `bar` a shortcut or a folder?
- It is a local flag on the root command (the picker) and on `backup` only. A persistent flag would also appear on `list`, `build`, and `init`, where it means nothing, and `list -o` would read as "write the list to a file".

### What changes in this mode

| | backup repository (default, `--repo`) | `--output DIR` |
| --- | --- | --- |
| config file | optional, but `repo_path` must come from somewhere | optional; never needed |
| destination | `repo_path` (must be absolute) | `DIR`, resolved against the working directory; `~` is expanded even in `--output=~/x` |
| git repository | required to commit | never needed |
| directory missing | created on first write; quitting the picker leaves nothing behind | same |
| commit / push | per `auto_commit`, `auto_push`, `--no-commit`, `--push` | never, even when `DIR` is inside a git repository |
| site | `site_dir`, relative links | `DIR/<site_dir>`, relative links |

**It never touches git, even when `DIR` happens to be inside a repository.** Running `sss backup --all -o ./shortcuts` from a project's root would otherwise commit a backup into that unrelated project. Committing is what `--repo` and `repo_path` are for: they name a repository you chose for backups, `--output` names a folder. Because git is never invoked, this mode also works without git installed.

**`--output` conflicts with `--push` and `--repo`, enforced with cobra's `MarkFlagsMutuallyExclusive`.** With no commit there is nothing to push, and silently ignoring `--push` would let someone believe a backup was published when it was not. `--repo` names a different destination, so the two together have no sensible meaning. Both conflicts are rejected before the Shortcuts database is opened. `--no-commit` is allowed and redundant.

**A config file is read if one exists, but only for how files look, never for where they go or what happens after.** `sign_mode`, `include_signed`, `include_hidden`, and `site_title` still apply, so a backup into a folder is signed for the same audience as your repository backup. `repo_path`, `auto_commit`, and `auto_push` are ignored. An absolute `site_dir` is replaced by the default `docs`, because it would otherwise write the site into whatever real repository it points at; everything this mode writes stays inside `DIR`.

**The site is still built.** The layout is identical to a backup repository — `shortcuts/` plus `docs/` — so `DIR` can later be turned into one with `git init` and used with `--repo` without reshuffling anything, and the web reviews are browsable straight from the folder. `BuildSite` already reads from the destination directory rather than the library, so the index covers every shortcut previously backed up into `DIR`, not only this run's.

**Re-running into the same directory is idempotent.** Nothing here bypasses the write-if-changed and signing-skip rules above; slugs still come from the whole library, with directories already in `DIR` claimed by their workflow ids as described above, and deleted or conflict status is recorded there exactly as in a repository. A second run with no edits reports every shortcut `unchanged` and leaves every file's bytes and modification time untouched, including the `.shortcut` files.

### The overwrite guard and `--force`

`-o .` run from a project root would write `shortcuts/` and `docs/` beside the project's own files and replace its `docs/index.html`. So before anything is written — before the Shortcuts database is opened, and before the picker appears — `InspectDir` classifies `DIR`:

| `DIR` is | result |
| --- | --- |
| missing | allowed; created on first write |
| empty, or holds only `.DS_Store` and `.git` | allowed, so `mkdir x && cd x && git init && sss backup --all -o .` works |
| an sss backup | allowed, without `--force`, so re-running into the same folder just works |
| anything else | refused: the error names `DIR`, says sss would write `shortcuts/` and the site directory into it, lists which of those already exist, and points at `--force` |

**"An sss backup" means `DIR/shortcuts/<any>/metadata.json` exists and decodes to JSON with a non-empty `name` and `workflow_id`.** That file is written by every successful backup of every shortcut, and those two keys are the ones sss always fills from the library, so one backed-up shortcut is enough. It is checked by content, not by directory names alone, because `shortcuts/` and `docs/` are ordinary names a project may already have — a folder of someone's own notes under `shortcuts/` does not count. Other files beside the backup (a README, `.gitignore`, the `.github/` workflow `sss init` scaffolds) do not matter, so an output folder can be turned into a repository and still be written to without `--force`.

**`--force` skips the check and writes anyway**, replacing any file at a path sss writes; it never deletes anything. It has no short form: `-f` is not used elsewhere in the CLI and an overwrite should be typed out. It applies only with `--output` — given alone it is rejected rather than ignored, because a backup repository has no guard to skip and a silently ignored `--force` would teach the wrong thing.

## Outcomes

Each shortcut reports one of:

| outcome | meaning |
| --- | --- |
| `written` | the canonical JSON changed and was rewritten |
| `unchanged` | the JSON was byte-identical; nothing was rewritten |
| `failed` | loading, scanning, or writing failed; the error is reported |
| `skipped` | the run was cancelled before reaching this shortcut |

A run where everything is `unchanged` creates no commit and leaves the working tree clean. That property is the observable proof that the signing skip works, and it is worth checking after any change to this package.

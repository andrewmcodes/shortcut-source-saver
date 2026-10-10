- Commit often using conventional commit messages
- Don't add code comments, instead write documentation in the README and other documents in the `.ai/` folder or `docs/` folder

## What this is

`sss` — a Go CLI/TUI that backs up Apple Shortcuts into a git repository as diffable JSON plus an importable `.shortcut`, and generates a GitHub Pages site of per-shortcut web reviews.

## Commands

Use `./cmd/... ./internal/...`, **never `./...`** — locally installed agent skill packs live in the repo root and their example sources break a tree-wide walk.

```sh
mise run check            # fmt, vet, test
mise run build            # -> bin/sss
mise run extract-catalog  # regenerate the action catalog (needs .ai/shortcut_source_tool.json)
```

## Layout

| path | role |
| --- | --- |
| `internal/shortcuts` | reads `~/Library/Shortcuts/Shortcuts.sqlite`; the only package that knows Apple's private schema |
| `internal/archive` | rebuilds the `.shortcut` plist, canonical JSON, signing |
| `internal/catalog` | embedded 729-entry action-identifier → display name catalog |
| `internal/render` | web review, README, site index |
| `internal/backup` | orchestrates a run; owns slug assignment |
| `internal/tui` | Bubble Tea picker |
| `internal/config`, `internal/scan`, `internal/gitrepo` | config, credential guard, git |
| `docs/*.md` | where all rationale lives (see the no-comments rule above) |

## Things that will bite you

- **Never open the Shortcuts database with `immutable=1`.** It makes SQLite ignore the WAL, and Shortcuts leaves recent edits there — a live library returned 188 shortcuts that way versus 189 read correctly. `shortcuts.Open` snapshots the database plus its `-wal`/`-shm` sidecars and opens the copy.
- **`shortcuts sign` is non-deterministic.** Identical input yields different bytes every time (AEA1 encrypted archive). Only re-sign when the canonical JSON changed, or every run adds a binary blob to history.
- **Icon colour is RGBA with alpha in the low byte**, and the column stores the same colour as both a positive and a negative integer — always mask with `& 0xFFFFFFFF`.
- **Shortcut names are not unique and slugs are lossy.** Slugs are assigned across the whole library at once, with colliding groups suffixed by workflow id. Do not compute a slug from a name in isolation.
- **The plist carries no name field** — Shortcuts derives an imported shortcut's name from the filename, so the signed file keeps the display name.
- Reading the library needs Full Disk Access; tests that touch it skip when it is unavailable.

## Verifying changes

`internal/integration` compares extraction against a real export in `.ai/` (485 actions, expect 0 mismatches; if the installed shortcut was edited after the export, it allows one small edited block and logs it — re-export to restore the exact comparison). After touching `internal/backup` or `internal/archive`, run a backup twice against a scratch repo and confirm `git status` is clean — that is the observable proof the signing skip still works.

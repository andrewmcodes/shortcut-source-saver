# Shortcut Source Saver

Back up your Apple Shortcuts into a git repository, with generated documentation and a browsable web review of every shortcut.

`sss` opens a terminal UI listing your Shortcuts library, you pick what to save, and it writes each shortcut to a git repo as diffable JSON, an importable `.shortcut` file, and a README — then regenerates a static site that renders every shortcut action by action.

## Why

Apple gives you no way to bulk-export Shortcuts. The `shortcuts` command line tool can run, list, view, and sign — but not export. The app exports one shortcut at a time through a share sheet. So a library built up over years lives in a single opaque database, with no history, no diffs, and no backup.

This reads that database directly and turns it into something git can track.

## Install

Requires macOS.

```sh
brew install andrewmcodes/tap/sss
```

Or from source, with [mise](https://mise.jdx.dev):

```sh
git clone https://github.com/andrewmcodes/shortcut-source-saver
cd shortcut-source-saver
mise run build      # -> bin/sss
```

### Grant Full Disk Access

Reading the Shortcuts database requires it. Open **System Settings → Privacy & Security → Full Disk Access** and enable your terminal (Terminal, iTerm, Ghostty, VS Code — whichever you run `sss` from), then restart it.

Without this you will get a clear error rather than a crash, but nothing will work.

## Quick start

```sh
sss init                 # write a config file and set up the backup repo
sss                      # pick shortcuts in the TUI and back them up
```

Just want the files? Skip the setup and write straight into a folder — no config, no git:

```sh
sss backup --all -o .                         # into the current directory
sss backup "Daily Note" -o ~/Desktop/shortcuts
sss -o ./exports                              # pick in the TUI, write to ./exports
```

## Usage

| Command | What it does |
| --- | --- |
| `sss` | Open the TUI: filter, multi-select, back up |
| `sss list` | Print every shortcut in your library |
| `sss backup <name>...` | Back up shortcuts by name, no TUI |
| `sss backup --all` | Back up the entire library |
| `sss backup --all -o <dir>` | Back up into any directory, with no config or git repo |
| `sss build` | Regenerate the web reviews and site index |
| `sss init` | Write an example config and scaffold the backup repo |
| `sss export [-o <path>]` | Write every shortcut's JSON into one `.zip` — no config, git, or signing |

Useful flags:

| Flag | Effect |
| --- | --- |
| `--config <path>` | Use a specific config file |
| `--repo <path>` | Override `repo_path` for this run |
| `-o, --output <dir>` | Write into `<dir>` instead of the backup repo; never commits. On `sss` and `sss backup`; cannot be combined with `--repo` or `--push` |
| `--force` | With `--output`, write into a non-empty directory that is not already an sss backup (refused by default) |
| `--no-commit` | Write files but do not commit |
| `--push` | Push after committing |
| `--include-hidden` | Include shortcuts hidden from your library |
| `--allow-secrets` | Proceed even if the credential scan finds something |

In the TUI: type to filter, <kbd>space</kbd> toggles selection, <kbd>a</kbd> selects all, <kbd>enter</kbd> confirms, <kbd>q</kbd> quits.

## What gets written

```
shortcuts/
  my-shortcut/
    shortcut.json        # canonical, sorted keys — this is what git diffs
    metadata.json        # name, id, icon, action count, dates, versions
    My Shortcut.shortcut # signed and importable
    README.md            # generated documentation
docs/
  index.html             # links to every web review
  dependencies.html      # which shortcuts run which
  shortcuts/
    my-shortcut.html     # action-by-action review
  assets/style.css
```

`shortcut.json` is the source of truth, because it is the only form that produces a readable diff. The `.shortcut` file is a build artefact — it is only rewritten when the JSON actually changes, since signing is non-deterministic and re-signing every run would add a new binary blob to history each time. A backup run that changes nothing leaves the working tree clean.

## One-off export

For a quick snapshot without setting anything up, `sss export` writes the whole library into a single zip:

```sh
sss export                         # ./shortcuts-20261009-212106.zip
sss export -o ~/Downloads          # a directory gets the timestamped name inside it
sss export -o ~/Desktop/lib.zip    # or name the file yourself
```

The zip holds `shortcuts/<slug>/shortcut.json` and `metadata.json` for every shortcut — byte-for-byte what a backup would write, minus the signed file and README. It needs no config file and never touches git. An existing file is never overwritten, and every shortcut is read and checked before anything is written, so a failed export leaves nothing behind. `--include-hidden`, `--allow-secrets` and `--database` apply as they do to `backup`. Details: [docs/export.md](docs/export.md).

## Configuration

`sss` reads TOML from the first of these that exists:

1. the path given to `--config`
2. `$XDG_CONFIG_HOME/shortcut-source-saver/config.toml` (default `~/.config/shortcut-source-saver/config.toml`)
3. `/etc/shortcut-source-saver/config.toml`

```toml
repo_path      = "~/git/my-shortcuts"  # required
sign_mode      = "anyone"              # or "people-who-know-me"
site_dir       = "docs"
site_title     = "My Shortcuts"
base_url       = "/my-shortcuts/"      # for GitHub Pages project sites
auto_commit    = true
auto_push      = false
include_signed = true
include_hidden = false
```

Full reference: [docs/configuration.md](docs/configuration.md).

## Publishing the web reviews

`sss init` scaffolds a GitHub Actions workflow in your backup repo that publishes `docs/` to GitHub Pages. Push the repo, then enable **Settings → Pages → GitHub Actions**.

The index lists every shortcut and links to its review. Each review renders the shortcut action by action: control-flow blocks nest visually, magic variables link back to the action that produced them, and large text blocks collapse. The details panel lists what the shortcut accepts and outputs, any questions it asks on import, which shortcuts it runs and which run it. Run Shortcut actions are matched to shortcuts in the backup by workflow identifier, then by name, and the dependency map draws the result, flagging calls to shortcuts that are not in the backup.

## Secrets

Shortcuts frequently embed API keys directly in `Text` and `Get Contents of URL` actions. Publishing a backup to a public repo would publish those keys, permanently.

So before writing anything, `sss` scans the decoded actions and import questions for credential-shaped strings and stops if it finds any, telling you which shortcut and which action. Override with `--allow-secrets` once you have looked. `auto_push` is off by default for the same reason.

The scan is deliberately conservative about false positives — see [docs/secret-scanning.md](docs/secret-scanning.md).

## How it works

- [docs/extraction.md](docs/extraction.md) — reading the Shortcuts database, the WAL trap, the plist field mapping, icon colour encoding
- [docs/archive.md](docs/archive.md) — building `.shortcut` files, canonical JSON, why signing is skipped when nothing changed
- [docs/export.md](docs/export.md) — the one-shot zip export and its no-overwrite, all-or-nothing guarantees
- [docs/catalog.md](docs/catalog.md) — where human-readable action names come from
- [docs/rendering.md](docs/rendering.md) — how a web review is generated
- [docs/configuration.md](docs/configuration.md) — every config option
- [docs/git-integration.md](docs/git-integration.md) — how commits are made
- [docs/secret-scanning.md](docs/secret-scanning.md) — the credential guard
- [docs/reference-notes.md](docs/reference-notes.md) — where the third-party shortcuts-generator reference is wrong

## Credits

The action catalog and the web-review design are derived from [gluebyte](https://routinehub.co/user/gluebyte)'s **Shortcut Source Tool** ([RoutineHub #5256](https://routinehub.co/shortcut/5256/)), which embeds a 729-entry mapping of action identifiers to human-readable names. macOS ships no equivalent, and this project would have been far poorer without it.

## Development

```sh
mise run check            # fmt, vet, test
mise run test
mise run build
mise run extract-catalog  # regenerate the action catalog (needs the reference export)
```

## License

MIT

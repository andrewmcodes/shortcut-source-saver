# Configuration

This document is the full reference for `internal/config`: where the configuration file is looked for, every option and its default, and the reasoning behind the parts that are not obvious.

## Where the file is read from

`Load(explicitPath)` consults, in order, and the first file that exists wins outright — configuration is *not* merged across files:

1. the path passed to `--config`
2. `$XDG_CONFIG_HOME/shortcut-source-saver/config.toml`, falling back to `~/.config/shortcut-source-saver/config.toml` when `XDG_CONFIG_HOME` is unset or empty
3. `/etc/shortcut-source-saver/config.toml`

The search path is deliberately short. A backup tool has one meaningful scope — the person running it — plus a machine-wide default for a shared or provisioned Mac. Per-directory or per-repo configuration would invite a `sss` run whose destination depends on the working directory, which is the opposite of what a backup should be.

A missing file is not an error when no `--config` was given: the defaults plus a `repo_path` are all that is required, so the tool stays usable before `sss init` has ever run. A missing file *is* an error when `--config` was given, because the user named a specific file and silently ignoring it would write the backup somewhere they did not ask for. That error wraps `os.ErrNotExist`, so callers can match it with `errors.Is`.

With `--output DIR` no file is needed at all, and a file that does exist only shapes the output (`sign_mode`, `include_signed`, `include_hidden`, `site_title`); its destination and git settings are ignored. See [backup.md](backup.md#backing-up-into-a-plain-directory---output).

`Config.Path` records the file that was actually loaded, or is empty when defaults are in use. The CLI reports it so "why did it use that repo?" is answerable without guessing which of the three locations won.

## Options

| Key | Default | Meaning |
| --- | --- | --- |
| `repo_path` | *(none — required)* | The git repository backups are written to. Must be absolute after `~` expansion. |
| `sign_mode` | `"anyone"` | Audience of the generated `.shortcut` files. Either `"anyone"` or `"people-who-know-me"`; these are the only two values `shortcuts sign --mode` accepts. |
| `site_dir` | `"docs"` | Where the generated GitHub Pages site is written. Relative to `repo_path` unless absolute. `docs` is the default because GitHub Pages can serve it from the default branch with no extra branch to maintain. |
| `site_title` | `"My Shortcuts"` | Title shown on the generated site. |
| `base_url` | `"/"` | URL prefix the site is served under. |
| `auto_commit` | `true` | Commit generated files after a successful backup. |
| `auto_push` | `false` | Push after committing. See below. |
| `include_signed` | `true` | Write the importable `.shortcut` next to the JSON. |
| `include_hidden` | `false` | Include shortcuts hidden from the Shortcuts library. |

`Example()` returns this same set as a commented TOML file, which is what `sss init` writes. Keeping the example in code rather than in a template file means it cannot drift out of sync with the struct without a test noticing — a test loads `Example()` and asserts it parses, validates, and mentions every key.

## The tilde trap

`repo_path = "~/git/my-shortcuts"` is expanded to the real home directory before anything else happens. This is not a convenience; it prevents a specific silent failure.

A tilde inside quotes is never expanded by the shell, and TOML values are quoted by definition. So the string that reaches the program is a literal `~/git/my-shortcuts`. A naive implementation passes that to `os.MkdirAll` and `git`, both of which treat it as a *relative* path — and quietly create a directory named `~` inside whatever the working directory happens to be. The backup appears to succeed. The user's real repo is untouched, and `~/git/my-shortcuts` is empty. The failure is only discovered later, when the backup is needed.

Two rules follow from that:

- `ExpandTilde` handles a bare `~` and a leading `~/`. It intentionally does not support `~otheruser`, which would require user-database lookups to support a case nobody backing up their own shortcuts needs.
- After expansion, a non-absolute `repo_path` is rejected outright with an error that shows the offending value. Expansion happens *before* validation so `~/…` passes and `./backups` or `my-shortcuts` does not. There is no plausible reading of a relative backup destination that is safer than refusing to guess.

`site_dir` gets the same expansion but only when it starts with `~`, because it is normally relative *to `repo_path`*, not to the working directory. Use `SitePath()` to get the resolved absolute directory: it returns `site_dir` as-is when absolute, otherwise `repo_path` joined with `site_dir`. Nothing in the codebase should join those two paths by hand.

## Why `auto_push` defaults to off

`auto_commit` is on and `auto_push` is off, which looks inconsistent until you consider which step is reversible.

A commit is local and undoable — `git reset`, amend, rewrite the history, nobody else ever sees it. A push to a public GitHub repository is not undoable in any meaningful sense. Shortcuts commonly carry API keys inline (see [secret-scanning.md](secret-scanning.md)), and a key that has been pushed to a public repo must be treated as compromised and rotated even if the commit is force-pushed away seconds later: it was fetched by crawlers, cached by the API, and mirrored.

So the default is: keep a local history automatically, publish on request. `--push` overrides it per run for anyone who has decided their repo is private and their shortcuts are clean.

## Unknown keys are rejected

Decoding uses BurntSushi's `toml.Decode`, whose returned `MetaData.Undecoded()` lists every key in the file that did not map to a struct field. If that list is non-empty, `Load` fails with the sorted key names and a pointer to `sss init`.

The alternative — ignoring keys the program does not recognise — is worse in exactly the case that matters. Write `autopush = true` instead of `auto_push`, and a lenient parser accepts the file, reports no problem, and never pushes. The user believes a setting is on when it is off. Typos in a nine-key config file are far more likely than a legitimate need to keep unrelated keys in it, so the strict reading wins.

## Validation and normalisation

`Validate()` is called by `Load` and is safe to call directly. It checks that `repo_path` is non-empty and absolute, that `sign_mode` is one of the two accepted values, and that `site_dir` is non-empty.

It also *normalises* `base_url` in place, trimming any slashes and re-adding exactly one at each end, so `my-shortcuts`, `/my-shortcuts`, and `//my-shortcuts//` all become `/my-shortcuts/`, and an empty value becomes `/`. Site generation can then build links by plain concatenation, with no per-call-site slash juggling and no `//` in the output. A GitHub Pages *project* site is served from `/<repo-name>/`, so getting this wrong breaks every asset and link on the published site; normalising once, centrally, is why it cannot be got wrong twice.

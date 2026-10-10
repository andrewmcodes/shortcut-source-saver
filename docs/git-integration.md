# How backups are committed to git

This document explains `internal/gitrepo`, the package that stages, commits, and optionally pushes the files a backup run produces (`shortcuts/<slug>/shortcut.json`, `README.md`, the signed `.shortcut`, and the regenerated `docs/` site).

## Why we shell out to the `git` binary

The package runs the user's own `git` executable instead of linking a pure-Go implementation such as [go-git](https://github.com/go-git/go-git). A backup repository is a *personal* repository, and everything that makes a personal repository work lives in configuration that only real `git` honours:

- **Credentials.** `credential.helper`, the macOS keychain helper, `gh auth`'s helper, SSH agents, and `~/.ssh/config` host aliases. A library would need its own credential plumbing and would still miss whatever the user actually has set up.
- **Commit signing.** `user.signingkey`, `commit.gpgsign`, and `gpg.format = ssh` — including 1Password's SSH signing agent. Signed commits are common on a repository that gets pushed to GitHub, and an unsigned commit in the middle of a signed history is a visible wart.
- **Hooks.** `pre-commit`, `commit-msg`, and `core.hooksPath`. If a user runs a linter or a secret scanner on commit, backups should be subject to it too — especially given shortcuts can contain secrets (see below).
- **Conditional config.** `includeIf` blocks that switch `user.email` per directory, the `~/work` vs `~/personal` split that many people rely on. Committing with the wrong identity is exactly the kind of quiet mistake this avoids.

The cost is a `git` binary dependency and subprocess overhead, both of which are irrelevant here: this is a macOS CLI that already assumes developer tooling, and a backup run makes a handful of git calls, not thousands. `exec.LookPath` is checked once and failure produces `ErrGitNotInstalled` with the `xcode-select --install` hint rather than a confusing "file not found".

## How git is invoked

Every invocation goes through one helper, and every call takes a caller-supplied `context.Context` so a TUI can cancel a slow push:

- `exec.CommandContext` with `Dir` set to the repository root, so the tool never depends on the process's working directory.
- Stdout and stderr are captured separately. Failures return a `*CommandError` carrying the arguments, the exit code, and git's stderr, so the surfaced message is git's own diagnosis rather than "exit status 128".
- `GIT_TERMINAL_PROMPT=0` is set on every call. Without it, a push with missing or expired credentials makes git block on a username prompt that it writes to the terminal — which, under a Bubble Tea TUI holding the screen in raw mode, looks exactly like a hang. With it set, git fails immediately with `could not read Username`, which the tool can show as an error the user can act on.
- `GIT_OPTIONAL_LOCKS=0` is set for read-only queries (`status`, `diff --cached`, `rev-parse`, `symbolic-ref`, `remote`). It stops those commands from taking `index.lock` to refresh the index, so running a backup while another git process holds the lock does not fail.
- Paths passed to `git add` are normalised to repository-relative and preceded by `--`. A shortcut named `--all` produces a slug that would otherwise be read as an option; the `--` terminator makes that impossible. Paths that resolve outside the repository are rejected with `ErrOutsideRepo` before git is called at all.

## The empty commit is a no-op, not an error

`Commit` returns `(committed bool, err error)`. When nothing is staged it returns `(false, nil)`.

Running a backup when nothing has changed is the *normal* case — a user re-running `sss` twice in a row, or a scheduled backup on a day when no shortcut was edited. If that returned an error, every caller would have to pattern-match on it to decide whether it was the boring case or a real failure, and a scheduled run would report a spurious failure most days.

The check is `git diff --cached --quiet`, whose exit code is 0 for "nothing staged" and 1 for "something staged". This is deliberately *not* done by running `git commit` and parsing its output for "nothing to commit, working tree clean" — that string is localised, has changed between git versions, and shares exit code 1 with genuine commit failures such as a rejecting `pre-commit` hook. Exit codes are stable; error text is not.

`--allow-empty` is not used: an empty commit would add noise to the history without recording anything.

## Conventional commit messages

The project requires [Conventional Commits](https://www.conventionalcommits.org/), so the package builds the messages rather than leaving them to callers. `CommitMessage` has a `Subject` and an optional `Body`, and `String()` joins them with a blank line.

One shortcut changed:

```
chore(shortcuts): back up Daily Note
```

Several shortcuts, listed alphabetically in the body (case-insensitive sort, so the order does not depend on capitalisation):

```
chore(shortcuts): back up 3 shortcuts

- apple
- Mango
- Zebra
```

Only the generated site changed:

```
chore(site): rebuild web reviews
```

`BackupMessage` is what a run uses: it takes the backed-up names plus a `StatusChanges` of shortcuts newly marked deleted, restored, or marked as sync-conflict copies, and appends one titled bullet section per non-empty group after the name list. With no status changes it is exactly `ShortcutBackupMessage`; with only status changes the subject is `chore(shortcuts): update shortcut status`. The example and the meaning of each status are in [backup.md](backup.md#shortcuts-that-are-no-longer-active).

Shortcut names are user-authored and arbitrary — they routinely contain emoji, quotes, and occasionally newlines. The subject line therefore:

- **collapses all whitespace and control characters to single spaces**, so `"Morning\nRoutine"` becomes `back up Morning Routine`. A newline in the subject would otherwise silently turn the rest of the name into the commit body.
- **is capped at 72 characters**, measured in runes rather than bytes so an emoji counts as one character and truncation never cuts a UTF-8 sequence in half. When a single name is long enough to be truncated, the untruncated name is written into the body as a bullet so nothing is lost.
- **needs no quoting or escaping.** The message is passed as a single `argv` entry to `git commit --message`; there is no shell involved, so quotes, backticks, and `$` in a shortcut name are literal.

Multi-line names in the body are kept readable by indenting their continuation lines by two spaces under the bullet.

## Why `auto_push` is opt-in

`Push` exists, but nothing calls it unless the user turns `auto_push` on in the configuration file. Committing is local and reversible; pushing is neither.

The reason is that **shortcuts can embed secrets**. A shortcut that calls an API typically has the key typed straight into a "Get Contents of URL" header, and the same is true of webhook URLs, personal Notion and Todoist tokens, home-automation endpoints, and phone numbers or addresses in text actions. All of that is preserved verbatim in the backed-up `shortcut.json` — the backup would be useless otherwise — and a backup repository is exactly the kind of repository a user makes public to share their shortcuts.

Pushing by default would mean a single command turning a private secret into a public one, with a permanent record in the remote's history that deleting the file does not remove. Making it opt-in means the first push is a decision, taken after the user has had a chance to look at what was written and to decide whether the remote should be private.

`Push` also refuses to guess: it defaults to `origin` and the current branch, but returns `ErrNoRemote` with the exact `git remote add origin <url>` command when there is no such remote, and `ErrDetachedHead` rather than pushing from a detached HEAD.

## Testing

The tests build real repositories in `t.TempDir()` and drive them with the real `git` binary. To keep them hermetic and to keep them passing on a machine with signing configured, each test sets `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` to `/dev/null`, sets `user.name`/`user.email` locally, and sets `commit.gpgsign=false` and an empty `core.hooksPath`. The suite exits early if `git` is not on `PATH`.

No test touches the user's own repositories and none makes a network call — `Push` is covered only through its no-remote error path.

# One-shot export

`sss export` writes the whole library into a single `.zip` and exits. It is for the times a full backup repository is more than you want: a snapshot before a macOS upgrade, a library to hand to another machine, a quick look at your shortcuts' JSON. It lives in `internal/export`, with the command in `cmd/sss/export.go`.

It was prompted by a standalone Python script that did the same job. What was carried over, and what was not, is recorded at the end.

## What is in the zip

```
shortcuts/
  my-shortcut/
    shortcut.json
    metadata.json
```

Each `shortcut.json` is produced by the same `archive.BuildWorkflow` → `archive.CanonicalJSON` path a backup uses, so the bytes are identical to what `sss backup` would write for that shortcut; `TestExportedJSONMatchesBackupBytes` pins that. Directory names come from `backup.ResolveSlugs` over the whole library, so colliding names get the same workflow-id suffixes they would get in a repository.

`metadata.json` comes from the same `archive.NewMetadata`, so sync-conflict copies carry `"status": "conflict"` and their `conflict_of` exactly as in a backup. Deleted shortcuts are never in an export, because an export reads only the library; `"status": "deleted"` exists only in a backup destination, which remembers what the library no longer has.

Mirroring the repository layout is deliberate: unzip an export anywhere and `sss build --repo <dir>` renders a site from it, because the site builder reads only `shortcut.json` and `metadata.json`.

Left out on purpose:

- **The signed `.shortcut`.** Signing shells out to `shortcuts sign` once per shortcut, which is slow across a whole library, and its output is non-deterministic (see [archive.md](archive.md)). A snapshot does not need it; the JSON is the source of truth.
- **The README and web reviews.** They are derived and can be regenerated with `sss build`.

Entries are sorted by path and stamped with each shortcut's modification time.

## Guarantees

**All or nothing.** Every shortcut is loaded, secret-scanned and encoded in memory before the output file is opened. Any failure — a row that will not decode, a credential the scan flags — is collected rather than returned immediately, so one run reports every broken shortcut by name and workflow id at once, and nothing is written. A whole library's JSON is a few megabytes, so holding it in memory costs nothing.

**Never overwrites.** The output is opened with `O_CREATE|O_EXCL`, so an existing file is refused atomically rather than checked-then-clobbered. The error wraps `export.ErrOutputExists`.

**No partial files.** If writing the zip fails after the file was created, `CreateExclusive` closes and removes it. The file is `fsync`ed before it is reported as written.

The output's parent directory is not created. A typo in `-o` should fail, not quietly make a directory tree.

## Flags

| flag | effect |
| --- | --- |
| `-o, --output <path>` | the zip to create; if `<path>` is an existing directory, the default name is used inside it. Defaults to `./shortcuts-<YYYYMMDD-HHMMSS>.zip` |
| `--include-hidden` | include shortcuts hidden from the library |
| `--allow-secrets` | export even when the credential scan finds something |
| `--database <path>` | read a different Shortcuts database |

No config file is read. `--repo`, `--no-commit` and `--push` are accepted, being global, but have no effect.

### Why the secret scan still applies

A zip on your own disk is less exposed than a public repository, but a snapshot is exactly the kind of file that gets attached to an email or dropped into a shared folder. It runs the same scan as `backup`, import questions included, so `sss` never writes a credential anywhere without being told to. The cost is one re-run with `--allow-secrets`.

## Reading the database

Export uses `shortcuts.Open`, so it reads the same WAL-including snapshot as every other command (see [extraction.md](extraction.md#the-wal-trap)). It contains no SQL of its own; `internal/shortcuts` remains the only package that knows Apple's schema. `export.Collect` takes a small `Source` interface rather than `*shortcuts.Library` so its behaviour can be tested without Full Disk Access.

## Compared with the Python script

| script behaviour | in sss |
| --- | --- |
| single zip of the whole library, no setup | ported as `sss export` |
| refuse to overwrite (`open("xb")`) | ported: `O_EXCL` |
| remove the zip if writing fails | ported, and strengthened: everything is validated before the file exists |
| fail with the shortcut's name on missing identifier or non-array actions | already present for actions (`decodePlistArray`, `Load`); `List` now names a shortcut with a `NULL` `ZWORKFLOWID` instead of surfacing a raw scan error |
| `sqlite3 ...?mode=ro` on the live file | not ported: `shortcuts.Open` snapshots the database with its `-wal` and `-shm` sidecars, which is what makes recent edits visible |
| one flat `NNNN-name.json` per shortcut, with name and id injected into the JSON | not ported: index prefixes shift whenever a shortcut is added, so two exports cannot be diffed; sss uses library-wide slugs and keeps name and id in `metadata.json`, leaving `shortcut.json` identical to the importable plist |
| `{"$type":"data","base64":...}` for `<data>` | already present as `{"__type__":"data","__value__":...}`; see [archive.md](archive.md#tagged-data-and-date-values) |
| dates as ISO strings | sss already tags dates, which is less ambiguous than an untagged string |
| `{"$type":"uid",...}` for `plist.UID` | not ported; see [archive.md](archive.md#known-limits-of-the-tagged-form) |
| default output in `~/Downloads` | the current directory instead, as CLI tools conventionally do; `-o ~/Downloads` gives the script's behaviour |

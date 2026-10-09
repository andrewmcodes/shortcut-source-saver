# The credential guard

This document explains `internal/scan`, the package that inspects a shortcut's decoded actions for embedded credentials before anything is written to the backup repository.

## Why it exists

Shortcuts has no notion of a secret store. If a shortcut calls an API, the key is typed into the shortcut — usually into a `Text` action, or straight into the `Authorization` header field of a `Get Contents of URL` action. It is stored in the workflow itself, in plain text, and it comes out in the export.

That makes a backup tool a publishing tool. Committing `shortcut.json` to a public GitHub repository publishes every key in it, permanently: pushed secrets are crawled within minutes, and removing the commit afterwards does not un-publish them. The only safe assumption is that a key which reached a public repo has to be rotated.

So `sss` scans every workflow it is about to write, names the shortcut and the action, and refuses to continue. `--allow-secrets` proceeds anyway, once the user has looked at the report. `auto_push` defaults to off for the same reason (see [configuration.md](configuration.md)).

## False positives are the real failure mode

This is the design constraint that shapes everything else in the package.

A scanner that flags a base64-encoded image, a UUID, or a URL slug trains the user to pass `--allow-secrets` by reflex — and a guard that is always overridden catches nothing at all. A missed key is bad; a guard nobody reads is worse, because it also hides the keys it *would* have caught.

The package is therefore biased towards silence, in a specific way: rules with a distinctive vendor prefix (`sk-ant-`, `ghp_`, `AKIA`, `AIza`) are near-zero-false-positive by construction and are trusted on the pattern alone, while generic keyword rules have to clear several additional filters before they report anything.

As a sanity check, the scanner is run against the project's 485-action reference shortcut, which contains realistic URLs, base64 blobs, UUIDs, hex colours, and file paths. It reports nothing.

## What is scanned

`Scan(actions []any)` takes the decoded `WFWorkflowActions` array — each element a `map[string]any` with an action type under `WFWorkflowActionIdentifier` and its parameters under `WFWorkflowActionParameters`. `ScanValue(index, identifier, value)` does the same for any other decoded tree.

The backup runs both: `Scan` over the actions, then `ScanValue` over each entry of `WFWorkflowImportQuestions`, reported against the question's `ActionIndex` under the identifier `import question`. Import questions are worth scanning on their own because a shared shortcut that asks "paste your API key" on import often ships with the author's own key as the `DefaultValue`, and the web review and README both show that default.

The walk is fully recursive through maps, slices, and strings, because a shallow pass over the parameter dictionary would miss almost everything. Shortcuts serialises typed text as a token-attachment structure, so a value a user typed into one field can sit four or five dictionaries deep:

```
WFWorkflowActionParameters
  WFHTTPHeaders
    Value
      WFDictionaryFieldValueItems[0]
        WFKey   → Value → string = "Authorization"
        WFValue → Value → string = "<the credential>"
```

Two things fall out of that shape:

- **The reported path is the full dotted route** to the string, indices included, so a user can find the field in the Shortcuts editor rather than being told "somewhere in this action".
- **The field *name* is a sibling value, not a path segment.** In the structure above, nothing in the path of the credential contains the word "Authorization" — it lives under `WFKey`. So the walker carries a *context*: the nearest meaningful field name, which is inherited through structural wrapper keys (`Value`, `string`, `WFValue`, `WFItems`, `WFDictionaryFieldValueItems`, `attachmentsByRange`) and is replaced by the resolved key string for a `WFKey`/`WFValue` pair. That context is what makes a bare token in an `Authorization` or `X-Api-Key` header detectable at all: the value has no keyword in it and no vendor prefix, and only its header name marks it as a credential.

Dictionary keys are walked in sorted order, so findings are deterministic across runs.

## Rules

Evaluated in this order. Vendor-specific rules come first so that when several rules match the same value, the most specific one is the one reported.

| Rule | Matches |
| --- | --- |
| `anthropic-api-key` | `sk-ant-…` |
| `openai-api-key` | `sk-…`, `sk-proj-…` (excludes `sk-ant-`, which the rule above owns) |
| `github-token` | `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_` + 36–255 alphanumerics, and `github_pat_…` |
| `aws-access-key-id` | `AKIA` or `ASIA` + 16 uppercase alphanumerics |
| `google-api-key` | `AIza` + 35 characters |
| `slack-token` | `xoxa-`, `xoxb-`, `xoxp-`, `xoxr-`, `xoxs-` |
| `private-key-pem` | `-----BEGIN [RSA/DSA/EC/OPENSSH/PGP/ENCRYPTED] PRIVATE KEY-----` |
| `jwt` | `eyJ` header followed by three dot-separated base64url segments |
| `bearer-token` | `Bearer <value>` / `token <value>` — entropy-checked |
| `basic-auth-credentials` | `Basic <base64>` — entropy-checked |
| `generic-secret-assignment` | `api_key`/`apikey`/`access_token`/`auth_token`/`secret_key`/`client_secret`/`secret`/`token`/`password`/`passwd`/`pwd` assigned a value — entropy-checked |
| `authorization-value` | a bare value whose *field name* is `Authorization`, `X-Api-Key`, `api_key`, `access_token`, `secret`, `password`, … — entropy-checked |

`Rules()` exposes the list with names and descriptions for `--help`-style output.

Every vendor pattern is anchored with a word boundary. Without it, `sk-` matches inside a URL slug like `/ask-questions-about-everything`, and `AIza`/`AKIA` match inside the middle of a base64 image blob — the two most common shapes in a real shortcut.

One value can satisfy several rules: a JWT in an `Authorization` header is also "Bearer *high-entropy blob*". Findings are deduplicated per location and per redacted value, keeping the first (most specific) hit, so that reports say `jwt` once rather than listing three rules for one string.

## The entropy check and its threshold

Entropy-checked rules require the candidate value to pass three filters, in this order — cheapest and most decisive first:

1. **Length ≥ 16** (`minGenericLength`). Shorter values cannot carry enough randomness to be distinguished from an ordinary word, and no real API key is that short.
2. **Not word-shaped** (`looksLikeWords`). Two or more separator-delimited segments (`_`, `-`, `.`, `/`, space) that are all purely alphabetic and at least two characters long. This rejects `backup_repository_path`, `my_test_value_here`, `docs/shortcuts/index`, and `shortcut-source-saver`.
3. **Shannon entropy ≥ 3.2 bits per character** (`minGenericEntropy`).

The threshold sits at 3.2 because repetitive or single-alphabet values fall below 3 bits, while random keys of 16 characters or more land between 3.8 (hex) and 4.7 (base64). Choosing the low end of that gap is the conservative choice *for this tool*: it lets an unusually repetitive real key slip through rather than flagging an ordinary string.

The word-shape filter in step 2 exists because entropy alone is a blunt instrument at these lengths, and it is worth being explicit about why. Shannon entropy measures bits *per character*, so a short string with varied letters scores high: `backup_repository_path` is 3.8 bits and `the quick brown fox` is 3.9 — both above a threshold that random keys also clear. Raising the threshold high enough to exclude them would exclude real keys too. The shape check does the work the number cannot, and the number then catches what shape cannot: long single-token values like a repeated-substring key or a run of hex.

Ordering matters for cost as well as correctness. The generic rules are the only ones that ever run these checks, so vendor-prefixed keys — the overwhelming majority of real findings — never pay for entropy computation.

## Placeholders

Every rule, including the vendor ones, skips values that are documentation filler. A README pasted into a comment action, or a shortcut shared as a template, routinely contains `sk-YOUR_OPENAI_API_KEY` — which is the right shape for the OpenAI rule and is not a secret.

Two checks, applied case-insensitively to the candidate value:

- **Phrase markers** (matched anywhere): `your_`, `your-`, `your `, `yourapi`, `yourtoken`, `yoursecret`, `youraccess`, `_here`, `-here`, ` here`, `goeshere`, `changeme`, `change_me`, `change-me`, `placeholder`, `notreal`, `not_real`, `replace_me`, `replaceme`, `replacethis`, `insert_your`, `putyour`, `put_your`, `xxxxxxxx`, `########`, `........`, `<`, `>`, `…`, `***`. These are long enough, or structurally odd enough, that a real key containing one is implausible.
- **Word markers** (must *dominate* the value): `example`, `dummy`, `sample`, `insert`, `redacted`, `removed`, `todo`, `fixme`, `fake`, `test`, `testing`, `abc123`, `foobar`, `foo`, `bar`, `baz`, `none`, `null`, `nil`, `empty`, `unset`, `default`, `secret`, `password`, `token`, `apikey`, `key`. Separators are stripped and the matched words must cover at least 60% of what remains, so `test_test_test_test` is a placeholder while `sk-proj-testXyZwVuTsRqPoNmLkJiHgFeDcBa` is not.

Splitting the list in two fixes a real false negative. Previously every marker was matched as a bare substring against the whole value, so a genuine key that merely *contained* `0000`, `abc123`, `foobar`, `test` or `example` anywhere in its random body was silently discarded — a false negative in a security guard, which is the wrong direction to fail. Short, common words now only suppress a finding when they account for most of the value.
- **Two or fewer distinct characters**, which covers `xxxxxxxxxxxx`, a run of dashes, and `aaaa-bbbb`.

The `<` and `>` markers are worth noting: they make every `<token>`, `<INSERT_YOUR_KEY_HERE>`, and `<api-key>` quiet in one stroke, since angle brackets never appear in a real credential.

This list is a deliberate false-negative surface. A key that literally contains the word `example` will not be reported. That is an acceptable trade for silence on template shortcuts, and it is documented here rather than being a surprise.

## Reporting and redaction

A `Finding` carries the action index, the action identifier, the dotted parameter path, the rule name, and a **redacted** excerpt. The excerpt reveals only a *known public vendor prefix* when the value starts with one, then masks everything else and appends the length and a short fingerprint: `sk-proj-****** (51 chars, sha256:0c4bf7ee)`. A value with no recognised prefix reveals nothing at all: `****** (24 chars, sha256:dd5d4a4e)`.

Nothing in the package ever emits secret content. Findings are printed to terminals, piped into scripts, and — in a scheduled backup — written to CI logs, which are frequently public and are exactly the kind of place a secret should not be copied into a second time.

An earlier version kept the first six characters of every match. For a vendor-prefixed key those six characters are boilerplate, but for a generic match — an `Authorization` value, say — they were six characters of the real secret, which for a short token is a meaningful fraction of it. The prefix is now allow-listed rather than positional: it is shown only when it is one of the publicly documented vendor markers (`sk-ant-`, `sk-proj-`, `sk-`, `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`, `AKIA`, `ASIA`, `AIza`, `xoxb-` and the other Slack variants, `eyJ`, `-----BEGIN`), all of which identify the vendor while revealing none of the key.

The `sha256:` fingerprint is a truncated digest, not reversible, and exists so that two different secrets of the same length at the same path stay distinguishable — findings are deduplicated by excerpt, and without it two distinct keys would collapse into one report. Multi-line matches such as a PEM header are flattened to one line so a report cannot be broken up by embedded newlines. Tests assert, for every rule, that neither the finding nor the rendered report contains the original value.

`Summary(findings)` renders a compact report grouped by action index — one block per action, its identifier in the heading, and one line per finding with the rule, the redacted excerpt, and the path. It returns an empty string when there are no findings, so the caller does not need a length check before printing.

## Overriding, and tuning

`--allow-secrets` proceeds despite findings. It is the only override, it is per-run, and there is no configuration key for it: a persistent "ignore secrets" setting would be switched on once, during an inconvenient run, and never switched off again.

If a rule is wrong for a specific corpus, the change belongs in this package rather than in a user-facing allowlist:

- To add a vendor rule, append to `rules` with its pattern and a word boundary. Leave `minEntropy` at zero if the prefix is distinctive.
- To quiet a recurring false positive, prefer a placeholder marker or a shape filter over lowering `minGenericEntropy` — the threshold is shared by every generic rule, and moving it moves all of them.
- Add both a positive and a negative test. The negative table is the more valuable half of the suite: it holds realistic shortcut content (URLs, a 400-character base64 blob, UUIDs, hex colours, file paths, SHA-1 digests, prose, and every placeholder shape above) and each entry is scanned both as plain text and nested inside an HTTP header, since those are two different code paths.

## Signing mode is validated, not coerced

Related, in `internal/archive`: `ParseMode` accepts an empty mode as the documented default (`anyone`) but returns `ErrUnknownSignMode` for anything it does not recognise, and `Sign` refuses to run on an unknown mode.

An earlier version silently coerced any unrecognised value to `anyone`, which is the *broader* audience. A typo such as `people-who-know-mee` would therefore have widened who could import the generated shortcut — failing in the permissive direction, which is the wrong way for a setting that controls distribution. `NormalizeMode` is retained for callers that want a non-erroring form, and it now falls back to the restrictive `people-who-know-me`.

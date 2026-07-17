# Command reference

This reference describes the supported v0.1.3 command tree. Global flags may
appear before or after a command path:

```text
pushover setup
pushover send [message] [flags]
pushover profiles add <name>
pushover profiles list
pushover profiles show <name>
pushover profiles use <name>
pushover profiles remove <name>
pushover recipients add <alias>
pushover recipients list
pushover recipients show <alias>
pushover recipients validate <alias>...
pushover recipients remove <alias>
pushover sounds
pushover limits
pushover auth status
pushover auth backend
pushover auth backend set <native|file>
pushover info
pushover version
pushover completion <bash|zsh|fish>
```

Global flags are `--profile <name>`, `--json`, `--plain`, `--no-color`, and
`--timeout <duration>`. A timeout must be bounded from one second through
fifteen minutes; it never enlarges an individual network phase. `--json` and
`--plain` are mutually exclusive.

## Send flags and conflicts

`send` accepts repeatable `--to <alias>` and `--device <name>`, plus
`--title`, `--priority`, `--sound`, `--timestamp`, `--ttl`, `--url`,
`--url-title`, `--html`, `--monospace`, `--attachment`, `--attachment-url`,
`--encrypted`, `--retry`, `--expire`, `--validate`, `--validate-network`,
`--dry-run`, `--yes`, `--stdin`, `--file`, and `--credentials-stdin`.

Exactly one message source is required: one positional body, `--stdin`, or
`--file <path>`. `--credentials-stdin` conflicts with `--stdin`, `--profile`,
`--to`, and a complete environment credential pair. Local and remote
attachments are mutually exclusive. HTML and monospace are mutually exclusive;
encryption rejects both formatting flags and attachments. URL title requires a
URL. Emergency priority (`2`) requires retry/expiry and explicit confirmation;
saved profile defaults are limited to `-2`, `-1`, and `0`.

`--validate` performs an explicit remote recipient validation before sending.
Remote attachment fetching during a dry run requires `--validate-network`.
There is no automatic send retry, even for a retryable transport error.

`--credentials-stdin` accepts one bounded JSON object with exactly these string
fields:

```json
{"token":"<application-token>","user_key":"<recipient-key>","e2ee_key":"<optional-64-hex-key>"}
```

`token` and `user_key` are required. `e2ee_key` is optional. Unknown,
duplicate, missing, null, or wrongly typed fields are rejected. Redirect a
protected file or use safe process substitution; do not construct the object
with an `echo` command containing secrets.

## Output schemas

Every JSON document has integer `schema_version: 1`, string `command`, and
string `status`. A success document has `result` and no `error`. A command
failure has `status: "error"`, no `result`, and an `error` object with string
`code`, string `category`, redacted string `message`, boolean `retryable`, and
boolean `ambiguous`.

A send result has boolean `dry_run`, ordered array `targets`, and object
`counts`. Send status is one of `dry_run`, `success`, `failed`, `ambiguous`,
`interrupted`, or `partial`. Each target object has exactly these keys:
`alias`, `outcome`, `priority`, `request_id`, `receipt_id`, `quota_remaining`,
and `error`.

Their types and meanings are:

- string `alias`;
- `outcome`: `planned`, `sent`, `failed`, `ambiguous`, or `skipped`;
- integer `priority`;
- nullable strings `request_id` and `receipt_id`;
- nullable integer `quota_remaining`;
- nullable `error`. A non-null target error object has exactly `code`,
  `category`, `message`, `retryable`, and `ambiguous`: the first three are
  redacted strings and the last two are booleans.

`counts` always contains exactly the integer fields `planned`, `sent`,
`failed`, `ambiguous`, and `skipped`. Other successful result schemas are:

| Command | JSON result fields |
| --- | --- |
| `send` | `dry_run`, `targets`, `counts` as defined above |
| `profiles list/show` | `rows` of `name`, `default`, `priority`, `title`, `sound` |
| `recipients list/show` | `rows` of `alias`, `kind`, `validated`, `devices` |
| `recipients validate` | `rows` of `alias`, `valid`, `devices`, `licenses`, `error_code` |
| `sounds` | `rows` of `name`, `description` |
| `limits` | `limit`, `remaining`, `reset_unix` |
| `auth status` | `profile`, `backend`, `configured`, `token_resolves` |
| `auth backend` | `backend` |
| `info` | `config_path`, `profile`, `backend`, `version`, `commit`, `build_date`, `features` |
| `version` | `version`, `commit`, `build_date` |
| `setup`; profile/recipient mutations; `auth backend set` | `resource`, `name`, `action`, `warnings` |
| `completion` | `shell`, `script` |

Arrays are encoded as JSON arrays, including an empty `warnings` array.

Plain output is headerless TSV. Its columns, in order, are:

| Command | Plain TSV columns |
| --- | --- |
| `send` | `alias`, `outcome`, `priority`, `request_id`, `receipt_id`, `quota_remaining`, `error_code`, `error_category` |
| `profiles list/show` | `name`, `default`, `priority`, `title`, `sound` |
| `recipients list/show` | `alias`, `kind`, `validated`, `devices` |
| `recipients validate` | `alias`, `valid`, `devices`, `licenses`, `error_code` |
| `sounds` | `name`, `description` |
| `limits` | `limit`, `remaining`, `reset_unix` |
| `auth status` | `profile`, `backend`, `configured`, `token_resolves` |
| `auth backend` | `backend` |
| `info` | `config_path`, `profile`, `backend`, `version`, `commit`, `build_date`, `features` |
| `version` | `version`, `commit`, `build_date` |

`setup`, mutation commands, and `completion` reject `--plain` as a usage error.

## Exit codes

| Code | Meaning |
| ---: | --- |
| 0 | Complete success |
| 1 | Internal or unclassified failure |
| 2 | Command usage error |
| 3 | Configuration, secret-store, or authentication failure |
| 4 | Local or remote validation failure |
| 5 | Quota or API rate-limit failure |
| 6 | Definite network failure with no delivery |
| 7 | Partial, interrupted, or mixed fan-out result |
| 8 | Required confirmation was not supplied |
| 9 | Ambiguous delivery with no completed delivery |
| 130 | Interrupted before any delivery completed and no delivery remains ambiguous |

## Shell completion

`completion bash`, `completion zsh`, and `completion fish` render deterministic
static scripts. They contain command paths, flags, shell values, and native/file
backend values only. They never discover configured profile names, recipient
aliases, secrets, filesystem state, or network data.

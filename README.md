# pushover-cli

`pushover-cli` is an unofficial, outbound-only Go client for the Pushover
Message API. It is intended for people, scripts, and explicitly authorised AI
agents. `v0.1.3` is the supported pre-1.0 release. Review every send before
allowing an external side effect.

## Installation

`v0.1.0` is tagged, but its release workflow failed before publishing
artifacts. `v0.1.1` was released, but its Darwin arm64 binary reported
development metadata and its Homebrew publication pushed `Formula/pushover.rb`
directly to the tap's `main` branch; the resulting `main` to `main` pull request
failed nonfatally.
`v0.1.2` was released, but versioned `go install` reported `version=dev` and
`commit=unknown`; it is unsupported.
`v0.1.3` is the supported pre-1.0 release; `v0.1.0` through `v0.1.2` are
unsupported.

Release archives and Homebrew install a binary with linker-injected version,
commit, and build date metadata. Versioned `go install` discovers its version
from Go module metadata; commit and build date may remain `unknown`.

### Build from source

```sh
git clone https://github.com/jurgenj136/pushover-cli.git
cd pushover-cli
go install ./cmd/pushover
```

### Install supported v0.1.3

Use one of these supported installation paths:

```sh
brew install jurgenj136/tap/pushover
```

```sh
go install github.com/jurgenj136/pushover-cli/cmd/pushover@v0.1.3
```

For an archive, download the archive for your operating system and architecture
plus its matching `checksums.txt` from the `v0.1.3` GitHub Release.
Archive names are `pushover_0.1.3_<os>_<arch>.tar.gz` on macOS and Linux, and
`pushover_0.1.3_<os>_<arch>.zip` on Windows.

Before extracting an archive, verify the downloaded archive against the
published checksum. Use the command for your operating system from the
directory containing the archive and `checksums.txt`.

On macOS, replace the archive name with the file you downloaded:

```sh
archive=pushover_0.1.3_darwin_arm64.tar.gz
checksum=$(grep " $archive$" checksums.txt) && printf '%s\n' "$checksum" | shasum -a 256 -c -
```

On Linux:

```sh
archive=pushover_0.1.3_linux_amd64.tar.gz
checksum=$(grep " $archive$" checksums.txt) && printf '%s\n' "$checksum" | sha256sum --check -
```

In Windows PowerShell:

```powershell
$archive = "pushover_0.1.3_windows_amd64.zip"
$entry = Select-String -Path checksums.txt -Pattern (" " + [regex]::Escape($archive) + '$')
if ($null -eq $entry) { throw "No checksum entry found for $archive" }
$expected = ($entry.Line -split '\s+')[0].ToUpperInvariant()
$actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash
if ($actual -ne $expected) { throw "Checksum verification failed for $archive" }
```

The release workflow also publishes GitHub artifact attestations for every
archive and for `checksums.txt`. These are attestations, not detached signature
files. With the GitHub CLI installed, verify both the archive provenance and
the checksum file:

```sh
gh attestation verify "$archive" --repo jurgenj136/pushover-cli
gh attestation verify checksums.txt --repo jurgenj136/pushover-cli
```

The portable Agent Skill is in `.claude/skills/pushover-cli/SKILL.md`. Skill
version `0.1.0` requires CLI version `0.1.0` or newer. The skill does not
install the native `pushover` binary. The published skill is available with the
pinned skills CLI:

```sh
npx --yes skills@1.5.9 add jurgenj136/pushover-cli --skill pushover-cli --yes
```

From a clone, install only this skill for an agent with the pinned skills CLI:

```sh
npx --yes skills@1.5.9 add . --skill pushover-cli --agent codex --yes
```

Omit `--skill pushover-cli` to install every discoverable skill in the
repository. Inspect the installed policy before use.

## First run

`pushover setup` is transactional. It stages a backend, profile, and recipient
aliases, validates answers, displays a redacted summary, and writes only after
final confirmation. Cancellation leaves the existing configuration unchanged.
The focused `profiles add` and `recipients add` commands are composable but
still interactive: they require a terminal so secrets can be prompted for
without entering argv. Recipient kinds are `user`, `group`, and `team-group`;
aliases and profile names are local names, never secret keys.

Before a real send, build a plan:

```sh
pushover --json send --to <recipient-alias> --dry-run "message"
```

Then make a separate, explicit send request:

```sh
pushover --json send --to <recipient-alias> "message"
```

Sends are non-idempotent. The complete batch is validated before the first
request, targets are attempted sequentially at most once, and ambiguous
delivery is never retried automatically.

## Credentials and message fields

Credentials belong to the user. Sign in to the
[Pushover dashboard](https://pushover.net/) to obtain your account's user key,
then register your own Pushover application to obtain its API token. This
project does not supply or share credentials and is not supported by Pushover.

Keep credentials in the configured native keychain or age-encrypted file
backend; do not put tokens or recipient keys in argv, shell history, logs,
fixtures, or documentation. For ephemeral automation, provide both
`PUSHOVER_TOKEN` and `PUSHOVER_USER_KEY` through the environment. Alternatively,
`--credentials-stdin` reads exactly one bounded JSON object containing required
string fields `token` and `user_key`, plus optional string field `e2ee_key`.
Unknown, duplicate, missing, null, or wrongly typed fields are rejected. Never
use `echo` to put a secret on a command line. Redirect a protected file instead,
with the message kept positional:

```sh
pushover --json send --credentials-stdin "message" < ./credentials.json
```

The two environment variables are atomic: exactly one is an error, and they
cannot be combined with `--profile`, `--to`, or `--credentials-stdin`.

`send` supports repeatable recipients and devices; title, priority (`-2` to
`2`), sound, timestamp, TTL, HTTPS URL and URL title, HTML or monospace
formatting, `--attachment` or `--attachment-url` image attachment, encryption,
emergency retry and expiry, recipient validation, dry-run, confirmation,
file/stdin body sources, and credentials-stdin. Exactly one body source is
required. Emergency priority requires valid retry/expiry values and explicit
`--yes` when non-interactive.

Remote attachments are fetched only for a send or an explicitly network-enabled
dry run. The fetcher accepts public HTTPS without URL credentials, denies
non-public IPv4 and IPv6 destinations before dialing and after every redirect,
pins resolved addresses against DNS rebinding, limits redirects and each
network phase, bounds streamed bytes regardless of `Content-Length`, and
accepts only sniffed PNG, JPEG, or GIF images. It never forwards Pushover
credentials or the source URL as attachment metadata, and errors expose only a
redacted origin.

End-to-end encryption is fail-closed across the complete batch. Every selected
recipient needs a valid 64-hex-character key; otherwise nothing is sent. There
is no plaintext fallback or mixed encrypted/plaintext batch. The CLI encrypts
the message, title, URL, and URL title, and rejects attachments plus HTML or
monospace formatting. The compatible Pushover format shares one symmetric key
between the sender and receiving devices and reuses it for AES and HMAC. It
provides no replay or reordering protection and compression leaks approximate
plaintext length.

Native stores, environment credentials, credentials-stdin, and the age backend
all trust the local OS-user boundary; a same-user process or compromised account
can use the secrets. When an unattended age identity or passphrase is stored on
disk, the backend chiefly protects against off-machine disclosure such as
backups, sync, or stolen media, not local same-user compromise. See
[SECURITY.md](SECURITY.md) for the complete trust model.

## Output, exits, and completions

Human output is the default. `--json` emits schema version 1 JSON with a typed
result or redacted error; `--plain` emits documented headerless TSV where a
tabular contract exists. `--json` and `--plain` cannot be combined. See
[REFERENCE.md](REFERENCE.md) for exact fields and process exits. Exit 6 means a
definite network failure with no delivery, while exit 9 means delivery is
ambiguous; mixed fan-out results use exit 7.

Generate deterministic Bash, Zsh, or Fish completions without reading config,
profiles, aliases, a secret store, or the network:

```sh
pushover completion bash > completions/pushover.bash
pushover completion zsh > completions/_pushover
pushover completion fish > completions/pushover.fish
```

Completion intentionally offers static command and flag words only.

## Troubleshooting and side effects

If stale `PUSHOVER_TOKEN` or `PUSHOVER_USER_KEY` variables are present, unset
both or provide both; a partial pair fails before network access. Use
`pushover --json auth status` to inspect configuration without exposing secret
values. `pushover info` reports paths and build metadata, not credentials.

Ordinary tests and builds never contact Pushover, open a native secret store, or
send a notification. The build-tagged live smoke test is opt-in only. It
requires dedicated `PUSHOVER_LIVE_TOKEN` and `PUSHOVER_LIVE_USER_KEY` values to
already be present in the environment. The only executing live command is:

```sh
PUSHOVER_LIVE_CONFIRM=YES go test -tags=live ./internal/live -run TestLiveSend -v
```

That command creates one external notification, consumes one message from the
dedicated application's quota, and sends exactly one lowest-priority (`-2`)
message titled `[pushover-cli live test] <UTC-RFC3339 timestamp>`. It never
retries. Never run it as part of ordinary development or CI. Compile the live
package without sending with `go test -run '^$' -tags=live ./internal/live`.

## Security reporting

Please use the private GitHub security advisory workflow for suspected
vulnerabilities. Do not publish credentials, recipient keys, message bodies, or
live endpoint details in an issue.

## License

MIT. See [LICENSE](LICENSE).

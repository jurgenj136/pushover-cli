# pushover-cli

`pushover-cli` is an unofficial, outbound-only command-line client for the
[Pushover Message API](https://pushover.net/api). It is built for people,
scripts, and explicitly authorised AI agents. `v0.1.3` is the supported
pre-1.0 release.

## Features

- Guided, transactional setup with profiles and recipient aliases
- Secure credential storage in the native keychain or an age-encrypted file
- Batch sends with devices, priorities, sounds, TTL, URLs, and formatting
- Local and public-HTTPS image attachments
- Optional fail-closed end-to-end encryption
- Human-readable, JSON, and plain TSV output
- Deterministic Bash, Zsh, and Fish completion
- Portable Agent Skill with an explicit-send policy

## Installation

### Homebrew

```sh
brew install jurgenj136/tap/pushover
```

### Go

```sh
go install github.com/jurgenj136/pushover-cli/cmd/pushover@v0.1.3
```

### Agent Skill

```sh
npx --yes skills@1.5.9 add jurgenj136/pushover-cli --skill pushover-cli --yes
```

The Agent Skill does not install the native `pushover` binary. Install the CLI
separately with Homebrew or Go.

### Build from source

```sh
git clone https://github.com/jurgenj136/pushover-cli.git
cd pushover-cli
go install ./cmd/pushover
```

Verified archives, checksums, SBOMs, and provenance attestations are available
from the [v0.1.3 release](https://github.com/jurgenj136/pushover-cli/releases/tag/v0.1.3).

## Quick start

Create a profile and recipient alias. Secrets are prompted for securely and are
not placed in command history:

```sh
pushover setup
```

Preview the complete send plan without contacting Pushover:

```sh
pushover --json send --to <recipient-alias> --dry-run "message"
```

When the plan is correct, make a separate explicit send:

```sh
pushover --json send --to <recipient-alias> "message"
```

## Common examples

```sh
# Title and sound
pushover send --to ops --title "Deploy complete" --sound magic "Production is healthy"

# High priority
pushover send --to ops --priority 1 "Database latency is elevated"

# Local image attachment
pushover send --to ops --attachment ./graph.png "Current latency graph"

# Encrypt message fields for an alias configured with an E2EE key
pushover send --to private --encrypted "Confidential update"
```

Run `pushover send --help` for every send option and conflict.

## Output and completion

Human-readable output is the default. `--json` emits schema version 1 JSON for
scripts and agents; `--plain` emits headerless TSV where a tabular contract
exists.

```sh
pushover --json recipients list
pushover --plain sounds
```

Generate shell completion without reading configuration, secrets, or network
state:

```sh
pushover completion bash > completions/pushover.bash
pushover completion zsh > completions/_pushover
pushover completion fish > completions/pushover.fish
```

## Safety

Use credentials for your own Pushover account and application. Keep tokens and
recipient keys out of argv, shell history, logs, and source control; use guided
setup, the environment, or a protected credentials file as documented in the
[security model](SECURITY.md).

Sends are non-idempotent. The CLI validates the complete batch before the first
request, attempts each target at most once, and never automatically retries a
send. A response interruption that may have reached Pushover is reported as
ambiguous.

Remote attachments must use public HTTPS and are fetched with redirect, address,
type, size, and timeout controls. End-to-end encryption is fail-closed and has
important compatibility and trust limitations. Read the
[security model](SECURITY.md) before using either feature.

## Documentation

- [Command reference](REFERENCE.md) — commands, flags, schemas, and exit codes
- [Security model](SECURITY.md) — credentials, attachments, encryption, and trust boundaries
- [v0.1.3 release](https://github.com/jurgenj136/pushover-cli/releases/tag/v0.1.3) — archives, checksums, SBOMs, and attestations
- [License](LICENSE) — MIT

This project is not affiliated with or supported by Pushover.

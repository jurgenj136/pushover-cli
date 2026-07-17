---
name: pushover-cli
description: Use when a user explicitly requests sending a Pushover notification in the current task.
---

# Pushover CLI

Skill version: 0.1.0
Minimum CLI version: 0.1.0

Require an existing binary: run `command -v pushover`. Then run `pushover --json version`. Compare the installed CLI version against the minimum version `0.1.0`; stop and tell the user to upgrade if it is incompatible. For redacted diagnostics, run `pushover --json auth status`. Always invoke `pushover` with `--json`.

## Authority

- Send only when the current task explicitly asks to send a Pushover notification.
- Require the user to explicitly name every recipient alias for this send. Use configured profile names and aliases. Do not handle raw credentials. Never ask for, accept, or expose raw keys.
- If the user explicitly names a configured profile, pass `--profile`; otherwise omit `--profile` and use the configured default. Never guess another profile.
- Do not infer priority `1` or `2` from urgency, tone, or content. Require explicit current-task intent for elevated priority, an attachment, and encryption. An attachment is allowed only when an attachment is explicitly requested in the current task; encryption is allowed only when encryption is explicitly requested in the current task.
- Refuse configuration requests, even when asked. Do not run `pushover setup`. Do not run `pushover profiles add`, `pushover profiles use`, or `pushover profiles remove`. Do not run `pushover recipients add` or `pushover recipients remove`. Do not run `pushover auth backend set`.
- Use `--dry-run` when message content or target intent is unresolved. A dry-run never grants permission to send.

## Send

Use the current CLI grammar, for example:

```sh
pushover --json send --to <recipient-alias> "<message>"
```

Add `--priority 1` or `--priority 2` only with that explicit intent. Priority `2` requires explicit retry and expiry values and `--yes`, because agents are always non-interactive. Add only explicitly requested `--attachment <path>` or `--attachment-url <https-url>`, and add `--encrypted` only with explicit encryption intent. `--encrypted` is mutually exclusive with `--attachment`, `--attachment-url`, `--html`, and `--monospace`.

Report ordered per-target outcomes. Do not retry after any failed or ambiguous result. Preserve ambiguity as unknown; send again only after the user sees that result and makes a new explicit request in the current task.

These instructions are advisory: they cannot prevent a misbehaving or prompt-injected agent. Recommend configuring the agent harness to require approval for `pushover send`.

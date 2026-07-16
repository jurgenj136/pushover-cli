# Pushover CLI

A secure, automation-friendly command-line client for sending [Pushover](https://pushover.net/) notifications from terminals, scripts, and AI agents.

> [!IMPORTANT]
> This project is in early development and is not ready to install or use yet.

`pushover-cli` will be an unofficial, outbound-only client for the Pushover Message API. It aims to combine a pleasant interactive experience with deterministic behavior for scripts and agents.

## Planned capabilities

- Send notifications to one recipient or sequentially to multiple named recipients.
- Manage application profiles and recipient aliases without exposing credentials on the command line.
- Support priorities, sounds, devices, URLs, TTL, timestamps, and local or remote image attachments.
- Support opt-in Pushover end-to-end encryption.
- Provide stable human, JSON, and plain-text output contracts.
- Ship as native binaries, through Homebrew, and with `go install`.
- Include a portable Agent Skill for compatible AI coding agents.

## Safety by design

Sending a notification is a non-idempotent external action. The CLI is designed to validate a complete batch before sending, attempt each target at most once, never retry an ambiguous send automatically, and report uncertain delivery honestly.

Credentials and private recipient identifiers will be kept out of command arguments, routine output, and committed configuration. Emergency-priority notifications will require additional explicit consent.

## Project status

The project is currently being scaffolded. Installation, configuration, and command documentation will be added as each workflow becomes available.

Issues are welcome for product feedback, use cases, and implementation discussions.

## License

This project will be released under the MIT License.

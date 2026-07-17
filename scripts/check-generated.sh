#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
contract=internal/releasetest/testdata/release-contract.json

jq -e '.versions.skills == "1.5.9" and (.archives | length == 6)' "$contract" >/dev/null
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT

for shell in bash zsh fish; do
  go run ./cmd/pushover completion "$shell" > "$temp/$shell"
done

cmp "$temp/bash" completions/pushover.bash
cmp "$temp/zsh" completions/_pushover
cmp "$temp/fish" completions/pushover.fish

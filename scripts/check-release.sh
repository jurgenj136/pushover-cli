#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
contract=internal/releasetest/testdata/release-contract.json

jq -e '
  .versions.golangci_lint == "v2.12.2" and
  .versions.goreleaser == "v2.17.0" and
  .versions.syft == "v1.46.0" and
  .checksum == "dist/checksums.txt" and
  (.archives | length == 6) and
  (.provenance_subjects | length == 7)
' "$contract" >/dev/null

grep -Fq 'name_template: checksums.txt' .goreleaser.yaml
grep -Fq 'artifacts: binary' .goreleaser.yaml
grep -Fq 'pull_request:' .goreleaser.yaml
go test ./internal/releasetest

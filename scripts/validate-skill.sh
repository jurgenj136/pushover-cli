#!/bin/sh
# Validate the portable discovery contract without interpreting skill content.

set -eu

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd)
repo_dir=$(CDPATH='' cd "$script_dir/.." && pwd)
skill_path="$repo_dir/.claude/skills/pushover-cli/SKILL.md"
skill_dir="$repo_dir/.claude/skills/pushover-cli"
testdata_dir="$repo_dir/internal/skilltest/testdata"

frontmatter_error() {
	printf '%s\n' "ERROR skill-frontmatter: $1" >&2
	return 1
}

version_error() {
	printf '%s\n' "ERROR skill-version: $1" >&2
	return 1
}

validate_skill() {
	path=$1
	if [ ! -f "$path" ]; then
		frontmatter_error "skill file is missing"
		return 1
	fi

	if ! LC_ALL=C awk '
		NR == 1 {
			if ($0 != "---") exit 1
			next
		}
		NR == 2 {
			if ($0 != "name: pushover-cli") exit 1
			next
		}
		NR == 3 {
			prefix = "description: "
			if (index($0, prefix) != 1) exit 1
			value = substr($0, length(prefix) + 1)
			if (value !~ /^Use when [A-Za-z0-9][A-Za-z0-9 .,-]*[A-Za-z0-9.]$/) exit 1
			next
		}
		NR == 4 {
			if ($0 != "---") exit 1
			valid = 1
			exit
		}
		END {
			if (!valid) exit 1
		}
	' "$path"; then
		frontmatter_error "expected strict name and description plain scalars"
		return 1
	fi

	body=$(awk '
		$0 == "---" { delimiters++; next }
		delimiters >= 2 { print }
	' "$path")
	if [ "$(printf '%s\n' "$body" | grep -Fxc 'Skill version: 0.1.0')" -ne 1 ]; then
		version_error "Skill version must be 0.1.0"
		return 1
	fi
	if [ "$(printf '%s\n' "$body" | grep -Fxc 'Minimum CLI version: 0.1.0')" -ne 1 ]; then
		version_error "Minimum CLI version must be 0.1.0"
		return 1
	fi
}

validate_fixtures() {
	validate_skill "$testdata_dir/valid-skill.md"
	if validate_skill "$testdata_dir/invalid-frontmatter.md" >/dev/null 2>&1; then
		frontmatter_error "invalid-frontmatter fixture was accepted"
		return 1
	fi
	if validate_skill "$testdata_dir/version-drift.md" >/dev/null 2>&1; then
		version_error "version-drift fixture was accepted"
		return 1
	fi
}

skip_unavailable() {
	printf '%s\n' "SKIP skills-cli-unavailable: $1"
	if [ -n "${CI:-}" ]; then
		return 1
	fi
	return 0
}

if [ "${1:-}" = "--validate-only" ]; then
	if [ "$#" -ne 2 ]; then
		frontmatter_error "--validate-only requires one skill file"
		exit 1
	fi
	validate_skill "$2"
	exit 0
fi
if [ "$#" -ne 0 ]; then
	frontmatter_error "unknown argument"
	exit 1
fi

validate_fixtures
validate_skill "$skill_path"

if ! command -v npx >/dev/null 2>&1; then
	skip_unavailable "npx not found"
	exit $?
fi

if discovery_output=$(
	cd "$skill_dir"
	DISABLE_TELEMETRY=1 DO_NOT_TRACK=1 NO_UPDATE_NOTIFIER=1 \
		npx --yes skills@1.5.9 add . --list
); then
	:
else
	status=$?
	if [ -n "$discovery_output" ]; then
		printf '%s\n' "$discovery_output" >&2
	fi
	exit "$status"
fi
case $discovery_output in
	*pushover-cli*) ;;
	*)
		printf '%s\n' "ERROR skill-discovery: skills CLI did not identify pushover-cli" >&2
		exit 1
		;;
esac

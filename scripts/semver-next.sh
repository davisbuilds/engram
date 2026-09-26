#!/usr/bin/env bash
# semver-next.sh — preview a semantic-version bump without creating a tag.
#
# Usage:
#   scripts/semver-next.sh <patch|minor|major>
#
# Reads the latest vX.Y.Z tag (or v0.0.0 if none) and prints the requested bump.
# This is a diagnostic preview, not Release Please's commit-derived decision.
# Release Please is the only release tag writer.
set -euo pipefail

level="${1:-}"
case "$level" in
	patch | minor | major) ;;
	*)
		echo "usage: $0 <patch|minor|major>" >&2
		exit 2
		;;
esac
if [ "$#" -ne 1 ]; then
	echo "usage: $0 <patch|minor|major>" >&2
	exit 2
fi

if ! git rev-parse --git-dir >/dev/null 2>&1; then
	echo "error: not a git repository" >&2
	exit 1
fi

# Latest vX.Y.Z tag by semver order; empty when the repo has no version tags yet.
latest="$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)"
if [ -z "$latest" ]; then
	major=0 minor=0 patch=0
else
	read -r major minor patch <<EOF
$(printf '%s' "${latest#v}" | tr '.' ' ')
EOF
fi

case "$level" in
	patch) patch=$((patch + 1)) ;;
	minor)
		minor=$((minor + 1))
		patch=0
		;;
	major)
		major=$((major + 1))
		minor=0
		patch=0
		;;
esac
next="v${major}.${minor}.${patch}"

printf '%s\n' "$next"

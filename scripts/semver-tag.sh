#!/usr/bin/env bash
# semver-tag.sh — compute and create the next semantic-version git tag.
#
# Usage:
#   scripts/semver-tag.sh <patch|minor|major> [--print]
#
# Reads the latest vX.Y.Z tag (or v0.0.0 if none), bumps the requested field,
# and creates an annotated tag. With --print it only prints the next version and
# creates nothing. Tag creation is local; pushing is a separate, explicit step
# (`git push origin <tag>`). Refuses to tag a dirty working tree.
set -euo pipefail

level="${1:-}"
mode="${2:-create}"
case "$level" in
	patch | minor | major) ;;
	*)
		echo "usage: $0 <patch|minor|major> [--print]" >&2
		exit 2
		;;
esac
[ "$mode" = "--print" ] && mode="print"

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

if [ "$mode" = "print" ]; then
	printf '%s\n' "$next"
	exit 0
fi

if [ -n "$(git status --porcelain)" ]; then
	echo "error: working tree is dirty; commit or stash before tagging" >&2
	exit 1
fi
if git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
	echo "error: tag $next already exists" >&2
	exit 1
fi

git tag -a "$next" -m "$next"
echo "created tag $next (was ${latest:-<none>})"
echo "push it explicitly with: git push origin $next"

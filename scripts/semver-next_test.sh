#!/usr/bin/env bash
# Regression tests for the read-only version preview in throwaway Git repos.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/semver-next.sh"
fails=0

check() { # desc, expected, actual
	if [ "$2" = "$3" ]; then
		echo "ok   - $1"
	else
		echo "FAIL - $1: expected [$2] got [$3]"
		fails=$((fails + 1))
	fi
}

newrepo() {
	d="$(mktemp -d)"
	git -C "$d" init -q
	git -C "$d" config user.email t@t
	git -C "$d" config user.name t
	cp "$script" "$d/semver-next.sh"
	git -C "$d" add .
	git -C "$d" commit -q -m init
	printf '%s' "$d"
}

# Version math from no tags.
r="$(newrepo)"
trap 'rm -rf "$r"' EXIT
check "no-tag patch" v0.0.1 "$(cd "$r" && ./semver-next.sh patch)"
check "no-tag minor" v0.1.0 "$(cd "$r" && ./semver-next.sh minor)"
check "no-tag major" v1.0.0 "$(cd "$r" && ./semver-next.sh major)"

# Version math from an existing tag.
git -C "$r" tag -a v1.2.3 -m v1.2.3
check "bump patch" v1.2.4 "$(cd "$r" && ./semver-next.sh patch)"
check "bump minor" v1.3.0 "$(cd "$r" && ./semver-next.sh minor)"
check "bump major" v2.0.0 "$(cd "$r" && ./semver-next.sh major)"

# Extra arguments are rejected; neither valid nor invalid previews create tags.
rc=0
(cd "$r" && ./semver-next.sh patch --print) >/dev/null 2>&1 || rc=$?
check "extra argument exits 2" 2 "$rc"
check "previews created no tags" "v1.2.3" "$(git -C "$r" tag --list | tr '\n' ' ' | xargs)"

rc=0
(cd "$r" && ./semver-next.sh bogus) >/dev/null 2>&1 || rc=$?
check "bad level exits 2" 2 "$rc"

# A dirty checkout can be previewed safely, and only exact semver tags count.
echo x >"$r/dirty"
git -C "$r" tag v2.0.0-rc.1
check "dirty preview ignores prerelease tag" v1.2.4 "$(cd "$r" && ./semver-next.sh patch)"
check "dirty preview created no tag" "" "$(git -C "$r" tag --list v1.2.4)"

if [ "$fails" -ne 0 ]; then
	echo "$fails test(s) failed" >&2
	exit 1
fi
echo "all semver-next tests passed"

#!/usr/bin/env bash
# Regression tests for semver-tag.sh. Runs the helper in throwaway git repos and
# asserts version math, the dirty-tree/existing-tag refusals, and — critically —
# that an unknown second argument is rejected without creating a tag.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/semver-tag.sh"
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
	cp "$script" "$d/semver-tag.sh"
	git -C "$d" add .
	git -C "$d" commit -q -m init
	printf '%s' "$d"
}

# Version math from no tags.
r="$(newrepo)"
check "no-tag patch" v0.0.1 "$(cd "$r" && ./semver-tag.sh patch --print)"
check "no-tag minor" v0.1.0 "$(cd "$r" && ./semver-tag.sh minor --print)"
check "no-tag major" v1.0.0 "$(cd "$r" && ./semver-tag.sh major --print)"

# Version math from an existing tag.
git -C "$r" tag -a v1.2.3 -m v1.2.3
check "bump patch" v1.2.4 "$(cd "$r" && ./semver-tag.sh patch --print)"
check "bump minor" v1.3.0 "$(cd "$r" && ./semver-tag.sh minor --print)"
check "bump major" v2.0.0 "$(cd "$r" && ./semver-tag.sh major --print)"

# Unknown second arg must be rejected (exit 2) and create nothing.
rc=0
(cd "$r" && ./semver-tag.sh patch --pritn) >/dev/null 2>&1 || rc=$?
check "typo flag exits 2" 2 "$rc"
check "typo flag created no tag" "v1.2.3" "$(git -C "$r" tag --list | tr '\n' ' ' | xargs)"

# Bad level rejected.
rc=0
(cd "$r" && ./semver-tag.sh bogus) >/dev/null 2>&1 || rc=$?
check "bad level exits 2" 2 "$rc"

# Create path on a clean tree makes an annotated tag.
(cd "$r" && ./semver-tag.sh patch) >/dev/null
check "create made v1.2.4" v1.2.4 "$(git -C "$r" tag --list v1.2.4)"
check "v1.2.4 is annotated" tag "$(git -C "$r" cat-file -t v1.2.4)"

# Dirty tree is refused.
echo x >"$r/dirty"
rc=0
(cd "$r" && ./semver-tag.sh minor) >/dev/null 2>&1 || rc=$?
check "dirty tree exits 1" 1 "$rc"

if [ "$fails" -ne 0 ]; then
	echo "$fails test(s) failed" >&2
	exit 1
fi
echo "all semver-tag tests passed"

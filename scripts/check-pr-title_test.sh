#!/usr/bin/env bash
# Behavioral controls for the squash-title guard, including hostile shell text.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
for title in 'fix(import): preserve scope' 'feat!: change CLI' 'refactor(scope)!: change policy' 'chore(main): release 0.2.0'; do
	PR_TITLE="$title" "$here/check-pr-title.sh"
done
for title in 'Add a feature' 'fix:' 'fix: ' 'fix:   ' 'fix((scope): invalid' $'fix: valid\nextra' $'fix:\tinvalid'; do
	if PR_TITLE="$title" "$here/check-pr-title.sh" >/dev/null 2>&1; then
		echo "FAIL - accepted invalid title: $title" >&2
		exit 1
	fi
done
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
# The substitution is intentionally literal hostile input, not shell code.
# shellcheck disable=SC2016
PR_TITLE='fix: $(touch '"$scratch"'/injected)' "$here/check-pr-title.sh"
if [ -e "$scratch/injected" ]; then
	echo 'FAIL - title executed shell text' >&2
	exit 1
fi
echo 'all PR title tests passed'

#!/usr/bin/env bash
# Validate the title that becomes the Conventional Commit on squash merge.
set -euo pipefail

pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^()[:cntrl:]]+\))?!?: [^[:space:][:cntrl:]][^[:cntrl:]]*$'
if [[ ! "${PR_TITLE:-}" =~ $pattern ]]; then
	echo '::error::Use a Conventional Commit PR title, for example fix(import): preserve scope' >&2
	exit 1
fi

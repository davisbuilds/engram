<!--
Write for a maintainer who decides from this description rather than the diff.
Scale it to the change: a small fix needs a title plus Why, Evidence, and Flags.
Delete sections that would be empty; keep Flags for any behavior change.
-->

## Why

<!-- The problem or motivation in behavior terms. Summarize it here; link the
in-repo issue, spec, or plan for depth. -->

## What changes

<!-- The approach and its key decision, one level above the diff. For a large
change: how it worked before, how it works now, and what stays compatible. -->

## Evidence

<!-- What ran and what it showed: a test that failed before and passes now,
commands and output, screenshots, numbers with their method. What was not
verified, and why. -->

## Flags

<!-- Write "None" only after checking. -->

- Changed or removed tests:
- Changed contracts (CLI, API, schema, config, file formats):
- Consistency sweep (parallel sites checked):
- Weakened guards (suppressions, permissions, dependencies, CI):
- Risk and rollback:

## Decisions and open questions

<!-- Judgment calls made on the maintainer's behalf, and the alternative
rejected when a real one existed. Questions that need an answer before merge. -->

## Guide for review

<!-- Reading order: where the substantive change is, what is mechanical, and
which tests are new or modified. -->

## Also in this PR / Out of scope

<!-- Declared extras. Follow-ups left out, and where they are tracked. -->

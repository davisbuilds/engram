# Contributing

## Welcome and scope

Bug reports, focused fixes, documentation improvements, and supported proposals
are welcome. Discuss substantial harness integrations, new dependencies, or public
CLI/schema changes before major implementation.

This is a solo-maintained project; contributions do not imply a support or
response-time commitment.

## Understanding and agent use

Agent-assisted work is welcome. Submitters should understand the change's purpose,
important behavior, tradeoffs, and verification limits. Explain what you checked
and what remains uncertain; no prompt transcript or manual rewrite is required.

Read the architecture invariants in [AGENTS](AGENTS.md): canonical data owns truth,
renderers are pure, sync owns filesystem changes, and only marked files may be
rewritten. [CLI contracts](docs/cli.md) own JSON and exit-code compatibility;
[headless usage](docs/headless.md) explains agent operation. Keep machine identity
and personal paths in configuration, out of tracked source and commit messages.

## Choosing work

[Backlog](docs/project/BACKLOG.md) records unresolved work. Backlog entries can be
delegated directly to agents or become focused PRs. Use an issue when persistent
discussion, investigation, or coordination helps; there is no mandatory graduation
step. An entry or issue alone is not a feature commitment. When an issue owns the
details, keep only a useful linked summary in the backlog.

## Delivering a change

Work on a focused branch from `main` (or an appropriate parent for stacked work).
Keep commits coherent. Describe the problem and resulting behavior in the PR,
with relevant verification and limitations. Merge after applicable checks pass
and review conversations are resolved.

Start with [README](README.md) for setup and [AGENTS](AGENTS.md#build--test) for
build, race tests, lint, formatting, and script checks. Follow the repo's behavioral
TDD and standard-library-first dependency policy.

The [Git policy](docs/project/GIT_HISTORY_POLICY.md) uses squash merges. Use a
Conventional Commit PR title: it becomes the retained commit title. Mark breaking
changes with `!` or a `BREAKING CHANGE:` footer in the PR body and explain migration.
[Release operations](docs/releases.md) owns version categories, retained-history
validation, and release recovery. Review consumer meaning and migration notes in
the generated release PR; do not maintain a parallel manual release log.

For branch cleanup, follow the [preservation-aware Git guidance](docs/project/GIT_HISTORY_POLICY.md#recommended-ongoing-hygiene).

Update the owning reference when its claims change and reconcile affected backlog
entries. Git and PRs hold routine delivery history.

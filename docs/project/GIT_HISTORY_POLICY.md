# Git History and Branch Hygiene

Last updated: September 26, 2026

## Repository Merge Settings

Observed September 26, 2026 on GitHub repository `davisbuilds/engram` (public).
Recheck current settings with:

```bash
gh api repos/davisbuilds/engram --jq '{allow_squash_merge, allow_merge_commit, allow_rebase_merge, delete_branch_on_merge, squash_merge_commit_title, squash_merge_commit_message}'
```

- `allow_squash_merge`: `true`
- `allow_merge_commit`: `false`
- `allow_rebase_merge`: `false`
- `delete_branch_on_merge`: `true`
- `squash_merge_commit_title`: `PR_TITLE`
- `squash_merge_commit_message`: `PR_BODY`

Result:

- PR branches can contain multiple commits.
- `main` receives one squashed commit per merged PR, titled from the PR title.
- Merged remote branches are auto-deleted.

## Merge Strategy

Squash-merge only. All other merge strategies are disabled at the repository level.

## Release History

Keep squash-only merges. PR titles follow Conventional Commits because they
become `main` commit titles. `.github/workflows/pr-title.yml` checks that syntax;
the maintainer still classifies each change correctly and reviews breaking-change
notes. Release Please owns version selection, changelog updates, release tags,
and GitHub Releases; see [release operations](../releases.md).

The release workflow runs only after successful push CI on current `main`.
Release PRs must pass the normal required `build-test-lint` check before merge.
The GitHub App token lets their pull-request events trigger normal CI.

## CI Gates

Workflow: `.github/workflows/ci.yml`

Quality gates before merge:

- `go build ./...`
- `go test -race ./...`
- `golangci-lint run ./...`
- `golangci-lint fmt --diff` (gofumpt formatting)
- `shellcheck scripts/*.sh` (when touching shell)

## Branch Protection

`main` is protected. The `build-test-lint` CI check is a required status check, so
a PR cannot merge until CI passes.

- `required_status_checks`: `build-test-lint` (`strict: false` — a PR need not be
  rebased onto the latest `main`, only pass its own CI run).
- `enforce_admins`: `false` — the repository owner can bypass protection for an
  emergency direct push; normal work still goes through a green PR.
- `required_pull_request_reviews`: none — this is a solo repository; review
  discipline is provided by the Codex PR review pass rather than a required
  second approver.

Consider enabling `strict` (require branches up to date before merge) or a
required review if the contributor set grows.

## Recommended Ongoing Hygiene

1. Create short-lived feature branches from `main`.
2. Open PRs early; keep them focused.
3. Merge only with **Squash and merge** after CI passes.
4. Before deleting local branches, inspect `git worktree list` and preserve any
   attached worktree or ongoing work. Confirm a branch's changes were merged or
   are equivalent to the squash commit on `main`; squash merges do not preserve
   branch ancestry. Delete only explicitly named, disposable branches.

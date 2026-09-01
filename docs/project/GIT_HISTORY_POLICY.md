# Git History and Branch Hygiene

Last updated: September 1, 2026

## Repository Merge Settings

Configured on GitHub repository `davisbuilds/engram` (public):

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
4. Periodically prune local branches:

```bash
git fetch --prune
git branch --merged main | grep -v ' main$' | xargs -n 1 git branch -d
```

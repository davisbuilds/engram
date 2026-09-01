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

`main` branch protection is not currently enabled. This is a public repository, so
branch protection is available and not blocked by any private-tier API limitation;
enabling a required `build-test-lint` status check on `main` is the recommended
next step. Until then, enforce checks and review discipline by convention.

## Recommended Ongoing Hygiene

1. Create short-lived feature branches from `main`.
2. Open PRs early; keep them focused.
3. Merge only with **Squash and merge** after CI passes.
4. Periodically prune local branches:

```bash
git fetch --prune
git branch --merged main | grep -v ' main$' | xargs -n 1 git branch -d
```

# Release operations

Release Please is the single release authority. It maintains a release PR with
`CHANGELOG.md` and `.release-please-manifest.json`; merging that PR after review
and green CI creates its `vX.Y.Z` tag and GitHub Release. Ordinary feature/fix
merges only update the release PR. This first wave publishes release notes and
tags, without distributing binaries or packages.

## Version and history policy

The manifest currently identifies the published `v0.2.0` release. CI uses that
actual tag's commit as the unreleased-history boundary. If the manifest advances
before its next tag exists, the conservative fallback is `bootstrap-sha`, now
set to the real `v0.2.0` commit (`a29b7a2cff20ea900ddd3a35a78612b77eb8ccb4`).
The original bootstrap predates already released non-Conventional commits;
refreshing it avoids retroactively rejecting that legitimate published history.
No tags or historical changelogs are rewritten.

PRs are squash merged with their title and body as the commit message. Use
Conventional Commit titles and inspect the final squash message before merging:

| Merged change | Before 1.0 | From 1.0 onward |
| --- | --- | --- |
| `fix:`, `perf:`, or `revert:` | Patch | Patch |
| `feat:` | Minor | Minor |
| `!` or `BREAKING CHANGE:` footer | Minor | Major |
| Ordinary `docs`, `test`, `chore`, `build`, `ci`, `style`, `refactor` | No release | No release |

Breaking changes need migration notes. Release PRs are reviewed and merged under
repository policy; this rollout does not configure automatic merging. Do not
create competing tags manually. An intentional 1.0 promotion requires a
separately reviewed version-policy decision. Use `fix(deps):` for a shipped
runtime/security dependency correction that needs a release, and `chore(deps):`
for ordinary maintenance that does not. Reverts appear in a Reverts section.

`go.mod`'s `go` directive is the language/toolchain floor, not an application
version. The Go Release Please strategy updates the changelog and manifest only.
`make build` and `make install` retain `git describe --tags --always --dirty`, so
an exact tagged build reports its tag, while intervening commits and local edits
retain their provenance. `make next-version` (optionally `LEVEL=minor|major`) is
a read-only arithmetic preview, not a prediction of merged-commit release intent.

## CI and authentication

`.github/workflows/release-please.yml` listens for completed `CI` runs, accepts
only successful pushes to this repository's `main`, and compares the tested SHA
with current `main` before making release changes. Older CI runs skip if main
has advanced. It executes no code or checkout supplied by the triggering event.
Release writes are serialized without cancelling a run in progress.

The required `build-test-lint` job keeps PR validation focused on the final
squash title; individual WIP branch commits are not release commits. Main pushes
must have nonzero, available, distinct SHAs with `before` an ancestor of `head`.
CI also validates all non-merge subjects from the actual manifest-matching tag
(or configured bootstrap fallback) through head, so a later valid push cannot
forget an earlier unclassified main commit whose CI failed. Missing configuration
or unavailable/nonancestor release boundaries fail closed. A tagged head may
have an empty unreleased range; the actual push range must still be nonempty.

A GitHub App installation token is restricted to this repository, with Contents,
Issues, and Pull requests write permissions; the default workflow token remains
read-only. The App action revokes its short-lived token when the job finishes.
The App's release PR events trigger normal pull-request CI, avoiding the built-in
`GITHUB_TOKEN` event restrictions/approval requirements. Release PRs still require
`build-test-lint`, just like every other PR. Renaming `CI` requires updating the
release workflow's `workflow_run.workflows` trigger.

## Activation and recovery

For a new installation or recovery, verify the release App on this repository has
Contents, Issues, and Pull requests read/write permissions, set repository
variable `RELEASE_APP_CLIENT_ID`, and set repository secret
`RELEASE_APP_PRIVATE_KEY`. Never commit the key. The workflow fails if these are
missing; it does not fall back to a broader personal token. No App setup, remote
settings, release tags, or GitHub Releases are created by a local implementation.

After a releasable main change, verify successful CI creates or updates a release
PR and that the App PR starts normal CI. Review its version, changelog, and PR
body for consumer behavior and migration steps since the actual release tag.
After a release PR merge, verify the tagged commit, manifest, changelog, and
GitHub Release agree. The GitHub App must not bypass branch protection, approve
its own PRs, or merge releases.

For a failed run, fix the reported configuration/permission/CI problem and rerun
the failed release workflow after confirming its tested SHA is still current.
If main advanced, wait for that commit's own CI. Check existing release PRs,
tags, and Releases before recovery; do not force tags or publish a manual version
to work around automation. A successful local test does not prove App provisioning
or GitHub event delivery; those are verified during activation.

Run `make test-scripts` for title controls and real Git release-history regression
controls, including a failed main commit followed by a later valid push. Workflow
changes also require `uvx zizmor@1.30.0 --offline .github/workflows/`.

If an unclassified commit is already on main, stop release work and review the
complete unreleased range and compatibility intent. Preserve published history;
rewriting unpublished main requires an explicit owner decision. Otherwise keep
automation blocked pending an owner-approved recovery release with complete
version/migration notes. Pause this writer before any separately authorized manual
recovery publication. Never fabricate a historical tag or silently skip the failed
commit; resume with the manifest matching the actual reviewed release tag.

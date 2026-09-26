# Release operations

Release Please is the single release authority. It maintains a release PR with
`CHANGELOG.md` and `.release-please-manifest.json`; merging that PR after review
and green CI creates its `vX.Y.Z` tag and GitHub Release. Ordinary feature/fix
merges only update the release PR. This first wave publishes release notes and
tags, without distributing binaries or packages.

## Version and history policy

The existing `v0.1.0` tag is the baseline. The manifest starts at `0.1.0`, and
`bootstrap-sha` identifies that tag's commit if Release Please cannot discover a
prior release. Historical changelogs are not reconstructed. Later releases use
their own discovered tag/release boundary rather than repeatedly using bootstrap.

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

A GitHub App installation token is restricted to this repository, with Contents,
Issues, and Pull requests write permissions; the default workflow token remains
read-only. The App action revokes its short-lived token when the job finishes.
The App's release PR events trigger normal pull-request CI, avoiding the built-in
`GITHUB_TOKEN` event restrictions/approval requirements. Release PRs still require
`build-test-lint`, just like every other PR. Renaming `CI` requires updating the
release workflow's `workflow_run.workflows` trigger.

## Activation and recovery

Before merging the automation, install the release App on this repository with
Contents, Issues, and Pull requests read/write permissions, set repository
variable `RELEASE_APP_CLIENT_ID`, and set repository secret
`RELEASE_APP_PRIVATE_KEY`. Never commit the key. The workflow fails if these are
missing; it does not fall back to a broader personal token. No App setup, remote
settings, release tags, or GitHub Releases are created by a local implementation.

After merge, verify the first successful main CI run creates/updates a release
PR and that the App PR starts normal CI. Review its version and changelog against
commits since `v0.1.0` before merging. Then verify the tagged commit, manifest,
changelog, and GitHub Release agree. The GitHub App must not bypass branch
protection, approve its own PRs, or merge releases.

For a failed run, fix the reported configuration/permission/CI problem and rerun
the failed release workflow after confirming its tested SHA is still current.
If main advanced, wait for that commit's own CI. Check existing release PRs,
tags, and Releases before recovery; do not force tags or publish a manual version
to work around automation. A successful local test does not prove App provisioning
or GitHub event delivery; those are verified during activation.

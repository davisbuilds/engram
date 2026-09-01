# Contributing

This repository uses a squash-merge workflow to keep `main` history clean and readable.

## Workflow

1. Sync local `main`.
2. Create a feature branch from `main`.
3. Make focused changes and commit normally.
4. Push branch and open a pull request.
5. Merge with **Squash and merge** after CI passes.
6. Let GitHub auto-delete the merged remote branch.
7. Prune merged local branches periodically.

## Branch Naming

Use descriptive prefixes:

- `feat/<name>`
- `fix/<name>`
- `chore/<name>`
- `docs/<name>`

## Commit Guidance

- Keep commits logical and atomic while working on the branch.
- Use clear, imperative commit messages.
- It is fine to have multiple commits in one PR; squash merge will combine them on `main`.

## Public-Repository Hygiene

This is a public repository, and host identity plus harness home directories are
**configuration, not code**. Keep local machine state out of tracked files and
commit messages: no absolute personal paths, real host names, private-repository
references, or personal identifiers. Scope and host labels are declared in config
and resolved at runtime, never compiled in.

## Architecture Invariants

engram is agent-first — the CLI is deterministic plumbing; judgment lives in an
agent. Before changing behavior, read the load-bearing invariants in
[`AGENTS.md`](AGENTS.md): canonical is the single source of truth, renderers are
pure while `internal/sync` owns the filesystem, and marker discipline is the
identity contract (engram only ever rewrites files carrying its own marker;
hand-authored files are inviolable). Preserve the stable JSON output envelope and
documented exit codes, or bump `schemaVersion` and update [`docs/cli.md`](docs/cli.md).

## Pull Request Expectations

- Keep PR scope tight (one objective per PR).
- Include a short summary and test evidence.
- Follow TDD for behavior changes: a red test that fails for the reason you are
  about to fix, then green.
- Zero external dependencies beyond `gopkg.in/yaml.v3` — prefer the standard library.
- Ensure CI passes before merge:
  - `go build ./...`
  - `go test -race ./...`
  - `golangci-lint run ./...`
  - `golangci-lint fmt --diff` (gofumpt formatting; run `gofumpt -w .` to fix)
  - `shellcheck scripts/*.sh` (when touching shell)

## Local Branch Cleanup

Run periodically:

```bash
git fetch --prune
git branch --merged main | grep -v ' main$' | xargs -n 1 git branch -d
```

## Documentation Hygiene

- Do not hardcode volatile counts in docs.
- Prefer executable source-of-truth references (for example, `go test ./...`,
  `.github/workflows/ci.yml`).
- Keep `docs/` current when behavior changes, and log deferred work in
  [`docs/project/BACKLOG.md`](docs/project/BACKLOG.md) rather than leaving it implicit.

## Related Docs

- Git history and branch hygiene config: [`docs/project/GIT_HISTORY_POLICY.md`](docs/project/GIT_HISTORY_POLICY.md)
- Agent implementation guidance and architecture invariants: [`AGENTS.md`](AGENTS.md)
- CLI interface contract (commands, envelope, exit codes, scope): [`docs/cli.md`](docs/cli.md)
- How a headless agent drives engram: [`docs/headless.md`](docs/headless.md)
- Project onboarding: [`README.md`](README.md)

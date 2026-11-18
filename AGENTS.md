# AGENTS.md

engram is a cross-harness agent-memory bridge: one canonical Markdown store,
rendered deterministically into each agent harness (Claude Code and Codex) and
reverse-synced back. Written in Go, agent-first — the CLI is plumbing, an agent
is the intelligence. See `README.md` for the user-facing overview.

## Documentation Map

- `docs/project/VISION.md` — continuity, fewer repeated mistakes, intended audience,
  knowledge boundaries, and questions guiding future work.

- `docs/cli.md` — the CLI interface contract: command tree, response envelope,
  exit codes, scope model, configuration, example invocations.
- `docs/releases.md` — release PRs, version policy, build provenance, and activation.
- `docs/headless.md` — how a headless agent drives engram: `next_steps` leads and
  the `curate` proposer/applier loop.
- `docs/operating.md` — configuration, isolated onboarding, adoption and scope,
  versioning the store in Git, and multiple machines (one writer, render-only consumers).
- `docs/project/BACKLOG.md` — future-only friction points and deferred follow-ups noted
  during implementation.
- `CONTRIBUTING.md` — public contribution scope, review expectations, and release intent.

## Architecture (load-bearing invariants)

- **Canonical is the single source of truth.** One Markdown file per memory (YAML
  frontmatter + body) under the canonical root. Every harness render is derived
  from canonical; canonical is never derived from a render.
- **Renderers are pure; `internal/sync` owns the filesystem.** Two invariants
  hold: apply is idempotent (a second apply on unchanged canonical is a no-op),
  and a file without an engram ownership marker is never modified or deleted.
- **Marker discipline is the identity contract.** engram only ever rewrites or
  removes files carrying its own marker; hand-authored files are inviolable, and
  the reverse-sync importers loop-guard on the same signal (for Codex, also the
  label its consolidator writes on a folded note) so engram never re-imports its
  own rendered output. The one exception is `migrate --apply`,
  run explicitly after a dry-run: it adopts a hand-authored original only when
  canonical provably supersedes it (identical content, deterministic match), and
  `migrate --shared` additionally requires the memory's shared render to be
  loadable; a diverged or ambiguous original is never touched.
- **CLI is plumbing, agent is intelligence.** Deterministic transforms live in
  engram; judgment lives in an agent. `curate` is the sole command that invokes a
  headless agent — and even there the agent only *proposes* operations; engram
  validates each against the corpus and schema and is the *only* mutator, failing
  closed (applies nothing) if any proposed operation is invalid, and rolling the
  batch back if a write fails midway.
- **All canonical mutation serializes** under one exclusive apply lock
  (`internal/lock`, an advisory `flock` the kernel releases automatically on
  process exit or crash) — `remember`, `share`, `import --apply`,
  `curate --apply`, `forget --apply`, `detach --apply`, `reconcile --apply` and
  `migrate --shared --apply` all take it.
- **Renders come from one locked read of canonical.** A render apply
  (`sync --apply`, `migrate --apply`) holds the canonical lock *shared* from its
  read of canonical through its last render write; a writer that also renders
  (`reconcile`, `import --shared`, `forget`, `migrate --shared`) holds its
  exclusive lock through its renders and renders from a read made under it. So
  a removal lands before the read or is refused, and is never undone by a
  render made from an older read. The canonical lock is taken before any render
  directory's; every lock fails fast (`locked`, nothing written), never waits;
  dry runs take none. A reader never creates a missing canonical root (it must
  stay "missing", which holds `STALE` removals, not become "empty"). `curate`
  reads its corpus under the shared lock and releases it before the agent runs;
  a missing root, which leaves nothing locked, is captured empty, never read.
- **Claude renders split in two.** Shared memories (global, no cwd narrowing)
  render once into `<claude home>/engram/memory/`, loaded everywhere through an
  engram-owned `<claude home>/rules/engram-memory.md`; project slugs hold only
  the rest. Reconcile never renders a memory back onto its own source (Codex
  as a whole; a Claude memory's own slug).
- **Scope model:** tiers `global` / `project:<repo>`, plus `applies_to` narrowing
  axes (cwd globs, agents, hosts). The host axis fails closed for an unmapped
  machine, and host labels are config-declared, never compiled in.

## Source layout

Each `internal/` package's doc comment states its responsibility (`go doc
./internal/<pkg>`); the binary entry point is `cmd/engram`.

## Local gate (matches `.github/workflows/ci.yml`)

```bash
go mod tidy -diff && go mod verify
go build ./...
go test -race ./...
golangci-lint run ./... && golangci-lint fmt --diff   # CI pins golangci-lint v2.14.0
make test-scripts
```

`gofumpt -w .` fixes formatting (`.golangci.yml` configures gofumpt as the
formatter). CI also checks the PR title (`scripts/check-pr-title.sh`), and on
`main` pushes validates the complete unreleased history.
`.github/workflows/security.yml` runs `govulncheck` on PRs, `main` pushes, and
weekly (`go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` locally).

## Guardrails

- **Zero external dependencies** beyond `gopkg.in/yaml.v3`. Prefer the standard
  library. Match the house style: small packages with one responsibility each,
  deterministic output, atomic temp-then-rename writes.
- **Agent-first output contract.** Every command emits the stable JSON envelope
  under `--json` (auto when stdout is not a TTY); exit codes are a documented
  contract (`0` ok, `1` runtime error, `2` usage/disabled-harness, `3`
  conflicts); read commands emit `next_steps` leads. Don't change the envelope
  shape or exit-code semantics without bumping `schemaVersion` and updating
  `docs/cli.md`.
- **This is a public repository — keep machine state out.** No absolute personal
  paths, real host names, or private repository references in tracked files or
  commit messages. Host identity and harness home directories are configuration,
  not code.
- **TDD for behavior:** red → green. The red step must fail for the reason you're
  about to fix. Skip it only for code with no behavior to assert (plain data
  holders, thin glue), and cover that after.

## Git and releases

- Squash-merge only ([policy](docs/project/GIT_HISTORY_POLICY.md)); the PR title
  and body become the `main` commit, so PR titles are Conventional Commits.
  `build-test-lint` is the required check; branches need not be up to date.
- **Release Please owns releases** ([release operations](docs/releases.md)).
  Ordinary merges only update the open release PR; merging that release PR
  creates the `vX.Y.Z` tag and GitHub Release. Do not create competing release
  tags. `go.mod` declares the Go language version, not the engram application
  version. Preserve Git-derived build provenance.

## Working Agreement

- **Clarify before building** when a request is vague, contradictory, or skips a
  key decision; clear, well-scoped requests need no interview.
- **Fix small issues now; backlog the rest.** Fix simple, quick, or blocking
  issues in this repo as part of the current change and call them out. Record in
  `docs/project/BACKLOG.md` only what can't be finished now or needs the
  maintainer's decision; don't open a branch or PR just for a backlog entry.
  Keep the backlog future-only; agents may work from entries directly, and
  issues help when persistent discussion or coordination is needed.
- **Keep docs current** when a change makes a documented contract, boundary,
  procedure, or direction stale.
- **Planning and continuity.** Reuse clear task context. Save planning docs when
  decisions, acceptance, or coordination need persistence. Leave a brief handoff
  only when needed to resume unfinished work. Commit shared docs and keep status
  current. Before archiving, preserve lasting decisions and open work in
  maintained docs and repair links. Keep private working docs in the ignored
  `docs/design/`, `docs/specs/`, or `docs/plans/` directories and share private
  context through restricted channels.
- **Commit and push.** Commit coherent chunks, staging by explicit path. Push
  your task branch and open a PR once the work is complete and the local gate
  passes; don't push directly to `main`.
- **Merge** only under the standing merge authority in the maintainer's
  workspace instructions, or when the maintainer asks. Without those
  instructions in context, leave the PR for the maintainer.
- **Re-ground after compaction** in source, this file, the owning docs, and
  recent commits. A summary and your own earlier proposals are not user approval.

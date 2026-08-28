# AGENTS.md

engram is a cross-harness agent-memory bridge: one canonical Markdown store,
rendered deterministically into each agent harness (Claude Code and Codex) and
reverse-synced back. Written in Go, agent-first — the CLI is plumbing, an agent
is the intelligence. See `README.md` for the user-facing overview.

## Documentation Map

- `docs/cli.md` — the CLI interface contract: command tree, response envelope,
  exit codes, scope model, configuration, example invocations.
- `docs/headless.md` — how a headless agent drives engram: `next_steps` leads and
  the `curate` proposer/applier loop.
- `docs/BACKLOG.md` — future-only friction points and deferred follow-ups noted
  during implementation.

## Architecture (load-bearing invariants)

- **Canonical is the single source of truth.** One Markdown file per memory (YAML
  frontmatter + body) under the canonical root. Every harness render is derived
  from canonical; canonical is never derived from a render.
- **Renderers are pure; `internal/sync` owns the filesystem.** Two invariants
  hold: apply is idempotent (a second apply on unchanged canonical is a no-op),
  and a file without an engram ownership marker is never modified or deleted.
- **Marker discipline is the identity contract.** engram only ever rewrites or
  removes files carrying its own marker; hand-authored files are inviolable, and
  the reverse-sync importers loop-guard on the same signal so engram never
  re-imports its own rendered output.
- **CLI is plumbing, agent is intelligence.** Deterministic transforms live in
  engram; judgment lives in an agent. `curate` is the sole command that invokes a
  headless agent — and even there the agent only *proposes* operations; engram
  validates each against the corpus and schema and is the *only* mutator, failing
  closed (applies nothing) if any proposed operation is invalid.
- **All canonical mutation serializes** under one exclusive apply lock
  (`internal/lock`, with stale reclaim) — `remember`, `share`, `import --apply`,
  and `curate --apply` all take it.
- **Scope model:** tiers `global` / `project:<repo>`, plus `applies_to` narrowing
  axes (cwd globs, agents, hosts). The host axis fails closed for an unmapped
  machine, and host labels are config-declared, never compiled in.

## Build & Test

- `make build` (or `go build ./...`); the binary entry point is `cmd/engram`.
- `go test ./...` — full suite; CI additionally runs `go test -race ./...`.
- `gofumpt -w .` to format; `golangci-lint run ./...` to lint (config in
  `.golangci.yml`, golangci-lint v2 with gofumpt as the configured formatter).
- CI (`.github/workflows/ci.yml`) runs build + race tests + lint + a gofumpt
  format gate on every pull request and push to `main`.

## Conventions

- **Zero external dependencies** beyond `gopkg.in/yaml.v3`. Prefer the standard
  library. Match the house style: small packages with one responsibility each,
  deterministic output, atomic temp-then-rename writes.
- **TDD for behavior:** red → green. The red step must fail for the reason you're
  about to fix. Skip it only for code with no behavior to assert (plain data
  holders, thin glue), and cover that after.
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
- Keep `docs/` current when behavior changes, and log deferred work in
  `docs/BACKLOG.md` rather than leaving it implicit.

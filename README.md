# engram

**A cross-harness memory bridge for AI coding agents.**

Different agent CLIs each keep their own long-term memory, and none can see the
others'. Switch harness mid-task and you lose signal: the same lesson gets
re-learned, the same preference re-stated. `engram` treats a single git-tracked
directory as the **canonical** source of truth for agent memories and renders
each one into every harness's native memory location, scoped by tier, working
directory, agent, and host — so a lesson learned in one place is available in
the others on the next sync.

> **Status: working, pre-1.0.** The full pipeline is implemented — a canonical
> store, Claude Code + Codex renderers, idempotent forward `sync`, reverse-sync
> `import`, `review`, and the headless `curate` proposer/applier loop. The
> interface contract (`docs/cli.md`) is stable but may still change before 1.0.

## Design in one breath

- **One canonical form, N harness renders.** A memory is authored once and
  rendered into each subscribed harness. Every rendered file carries a
  provenance marker; anything *without* a marker is hand-authored and off-limits.
- **The CLI is plumbing; the agent is the intelligence.** engram does
  deterministic transforms and marker discipline and emits structured JSON.
  Judgment — what is worth remembering, what to merge, what to promote — is left
  to a headless agent that consumes that JSON.
- **Agent-first CLI.** Structured output by default off a TTY, a single stable
  response envelope, documented exit codes to branch on, dry-run-by-default
  writes, and a self-describing `schema` command. See [`docs/cli.md`](docs/cli.md).
- **Scoped, explicit promotion.** Memories are `global` or `project`-scoped and
  filtered by cwd globs, agent, and host. Widening a memory's reach is an
  explicit, reviewable action — nothing leaks up implicitly.

## Build

Requires Go 1.26+; the only dependency is `gopkg.in/yaml.v3`.

```bash
make build      # -> bin/engram (version-stamped from git)
make install    # -> $PREFIX/bin/engram (default ~/.local/bin; override PREFIX)
./bin/engram help
./bin/engram schema --json
```

Cut a release tag (bumps the latest `vX.Y.Z`, creates an annotated tag locally;
push it explicitly):

```bash
make next-version           # print the next patch version, create nothing
make tag-patch              # v0.1.0 -> v0.1.1   (also tag-minor / tag-major)
git push origin v0.1.1      # publish the tag when ready
```

`curate` additionally shells out to a headless agent CLI (`claude` and/or
`codex`); every other command is self-contained.

## Layout

```
cmd/engram          entrypoint
internal/schema     the canonical memory type + validation
internal/store      atomic per-memory read/write of canonical files
internal/discover   parse the canonical set (with per-file parse errors)
internal/render     pure canonical → harness renderers (Claude, Codex)
internal/sync       the filesystem owner: idempotent apply, marker discipline
internal/importer   reverse-sync a harness's native memory into canonical
internal/review     read-only health findings (near-duplicate names)
internal/curate     the headless proposer/applier loop (agent proposes, engram applies)
internal/agentexec  builds headless agent argv + extracts structured output
internal/lock       the advisory flock all canonical mutation serializes under
internal/scope      the scope/applies_to filter model
internal/config     configuration + per-harness readiness
internal/cli        command dispatch + the JSON response envelope
docs/cli.md         the CLI interface contract
docs/headless.md    how a headless agent drives engram
```

## License

MIT — see [LICENSE](LICENSE).

# engram

**A cross-harness memory bridge for AI coding agents.**

Different agent CLIs each keep their own long-term memory, and none can see the
others'. Switch harness mid-task and you lose signal: the same lesson gets
re-learned, the same preference re-stated. `engram` treats a single git-tracked
directory as the **canonical** source of truth for agent memories and renders
each one into every harness's native memory location, scoped by tier, working
directory, agent, and host — so a lesson learned in one place is available in
the others on the next sync.

> **Status: early scaffold.** The command surface and interface contract are
> defined (`docs/cli.md`); most commands are stubbed as the implementation lands
> in phases.

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

Requires Go 1.26+. No external dependencies.

```bash
make build      # -> bin/engram
./bin/engram help
./bin/engram schema --json
```

## Layout

```
cmd/engram        entrypoint
internal/cli      command dispatch + the response envelope
internal/version  build version
docs/cli.md       the CLI interface contract
```

## License

MIT — see [LICENSE](LICENSE).

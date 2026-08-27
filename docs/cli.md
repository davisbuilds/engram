# engram CLI Design

`engram` is a cross-harness agent-memory bridge. Its operator is a **frontier
coding agent**, not a human at a TTY — so the surface is designed for machine
consumption first, with human-readable output as a courtesy fallback.

This document is the **interface contract** (syntax + behavior). It is
mechanism-free about implementation. The behavioral contract it serves lives in
the (local) design spec.

## Design principles (agent-first)

1. **One stable response envelope.** Every command, under `--json`, returns the
   same top-level object shape. An agent parses one schema, not N.
2. **Structured output by default for non-humans.** Output mode auto-selects:
   JSON when stdout is not a TTY (an agent, a pipe, a hook), human text when it
   is. `--json` / `--plain` force the choice.
3. **Documented exit codes are the fast path.** An agent branches on the exit
   code before parsing a byte. Codes are a stable contract.
4. **Dry-run by default; writes are explicit.** State-changing commands preview
   (compute + report `Action`s) and write nothing until `--apply`. This gives
   the agent a preview→apply loop and makes every write intentional.
5. **Never block on a prompt.** No command waits on interactive input. Safety
   comes from the explicit `--apply` gate and from separate write commands, not
   from confirmation prompts an agent cannot answer.
6. **Self-describing.** `engram schema` emits the machine-readable schemas
   (response envelope + canonical memory); `engram help --json` lists commands.
   An agent discovers the surface without scraping prose.
7. **Leads, not actions, for judgment.** Read commands that find something worth
   acting on (`review`, `audit`) emit `next_steps[]` — the exact follow-up
   invocation — rather than acting. The CLI is plumbing; the agent decides.
8. **Idempotent + stateless.** Every invocation stands alone; re-running a
   command with unchanged inputs is safe and (for `sync --apply`) a no-op.

## Response envelope

Every `--json` result:

```json
{
  "schema_version": 1,
  "command": "sync",
  "ok": true,
  "data": { "...": "command-specific payload" },
  "warnings": ["human-readable non-fatal notes"],
  "error": null,
  "next_steps": [
    { "reason": "why", "command": "engram share foo --to global" }
  ]
}
```

- `ok` — boolean success, redundant with the exit code but present so a captured
  payload is self-contained.
- `data` — command-specific; documented per command.
- `error` — `null` on success, else `{ "code": "stable_slug", "message": "…" }`.
  `code` is a stable branch key; `message` is for humans.
- `next_steps` — agent-consumable leads; may be empty/absent.

Primary data (and all JSON) goes to **stdout**; diagnostics, warnings, and human
notes go to **stderr**.

## Exit codes

| Code | Meaning | Agent action |
| ---- | ------- | ------------ |
| `0` | success, including "no actions needed" | proceed |
| `1` | runtime/unexpected error (I/O, parse, internal) | inspect `error.code`, retry or surface |
| `2` | usage/validation error, **or** a write to a disabled harness (strict toggle) | fix the invocation/config |
| `3` | `CONFLICT` actions present and unresolved | resolve the named conflicts, then re-run |

Code `3` is distinct so an agent detects "I must resolve conflicts" from the exit
status alone. `sync --apply` applies all non-conflicting actions and exits `3`
when any `CONFLICT` remains — partial success is observable, not silent.

## Command tree

```
engram [global flags] <command> [args]

  Authoring & propagation (write canonical or render):
    remember     Author a canonical memory (flags, or --from-json - on stdin).
    share        Promote a memory to a wider scope tier (writes canonical).
    sync         Render canonical → harnesses. Dry-run; --apply to write.
    import       Reverse-sync a harness's native memory into canonical
                 (explicit, one-shot; dry-run, --apply to write).
    curate       Run a headless agent over the corpus; it proposes
                 add/merge/remove/rescope, engram validates and applies
                 (dry-run; --apply to write). The one command that runs an agent.

  Introspection (read-only, --json everywhere):
    discover     Parse and list every canonical memory, with parse errors.
    list         List memories relevant to a given cwd / agent / host.
    audit        Report pending render Actions for a harness without writing.
    diff         Cross-state difference for each render target.
    show         Dump a harness's engram-rendered memories.
    review       Health report: near-dupe names, promotion candidates,
                 staleness — emitted as next_steps leads.

  Wiring & meta:
    hook         Print harness lifecycle wiring (SessionStart/Stop sync).
    config       Show or validate the resolved configuration.
    schema       Emit engram's JSON schemas (self-describing).
    version      Print the engram version.
    help         Show help (help --json lists commands machine-readably).
```

Verb-first throughout (matches the authoring vocabulary: *remember*, *share*,
*sync*). No abbreviations, no catch-all command.

## Global flags

| Flag | Purpose |
| ---- | ------- |
| `--json` | force structured output (auto when stdout is not a TTY) |
| `--plain` | force stable line-based human output |
| `-q, --quiet` | suppress non-essential stdout (for hooks) |
| `-v, --verbose` / `--debug` | more diagnostics to stderr |
| `--no-color` | disable color (also honors `NO_COLOR`, `TERM=dumb`) |
| `--config <path>` | config file (else `$ENGRAM_CONFIG`, else XDG default) |
| `--cwd <path>` | operate as if invoked from this directory — sets the scope/slug target |
| `--agent <claude\|codex>` | caller harness for scope filtering (else inferred) |
| `--host <label>` | override the host label (else `hostname -s` mapped via config) |
| `-h, --help` | help; ignores other args |
| `--version` | print version |

`--cwd`, `--agent`, and `--host` are the load-bearing agent affordances: a hook
or headless agent runs `engram` on behalf of *another* session whose directory,
harness, and machine differ from engram's own process — these make the target
explicit rather than ambient.

## Command semantics (selected)

- **`remember`** — writes one canonical memory. Accepts fields as flags
  (`--name`, `--description`, `--type`, `--scope`, `--applies-cwd`,
  `--applies-agent`, `--applies-host`, …) **or** a complete `CanonicalMemory`
  as JSON on stdin via `--from-json -` (the agent path: construct once, pipe in).
  Refuses to overwrite a differing canonical of the same name → canonical-side
  `CONFLICT` (exit `3`), never silent data loss.
- **`sync`** — computes render `Action`s (`CREATE` / `UPDATE` / `STALE` /
  `CONFLICT`) for the relevant memories against the current cwd/agent/host and,
  with `--apply`, executes them under an exclusive lock (idempotent; a second
  concurrent `--apply` blocks or exits non-zero). Without `--apply`: reports and
  writes nothing.
- **`audit`** — `sync`'s read-only projection: the `Action` list as data, always
  zero side effects.
- **`import <harness>`** — reverse-sync, explicit and one-shot. Dry-run lists the
  canonical memories it *would* create; `--apply` writes them. Marker/loop-guarded
  so engram's own output never round-trips. `import` against a disabled harness
  exits `2`. **Scope is derived, not defaulted:** a memory's scope resolves to
  `project:<repo>` when its source cwd (Claude: the import cwd; Codex: the Task
  Group's `applies_to: cwd=`) is a real git repository, else `global`. Only the
  repo's base name enters the scope — the full path never does. A workspace
  container (a non-repo parent of many projects) correctly stays `global`.
- **`show <harness>`** — permissive on a disabled harness (proceeds, stderr note);
  contrast `import` (strict). Read vs write, mapped to filesystem semantics.
- **`review`** — never mutates; every finding is a `next_step` the agent may run.
- **`curate`** — the proposer/applier loop, and the **only** command that invokes
  an agent. engram gathers the canonical corpus + `review` findings
  (deterministic), hands them to a headless agent as facts, and the agent returns
  *proposed operations only* (`add` / `update` / `merge` / `remove` / `rescope`
  with reasons) — it never touches a file. engram validates every proposed
  operation against the corpus and the schema; a dry-run reports the plan, and
  `--apply` executes it through the same `store` write-path, holding the shared
  exclusive canonical-root lock so the multi-file batch is atomic against a
  concurrent apply. **Fail closed**: if any operation in the batch is invalid,
  `--apply` applies nothing (exit `3`).
  Model/effort are `--model` / `--effort` (flags win over the per-harness config
  default: claude → `claude-sonnet-5`/`high`, codex → `gpt-5.6-terra`/`high`);
  `--harness` picks which agent runs (default `claude-code`). The trust boundary
  is that a model *proposes* and engram is the sole *mutator*.
- **`hook print`** — emits the JSON snippet wiring `engram sync --apply --quiet`
  to Claude Code SessionStart/Stop. Codex capture is agent-wrapped (documented).

## Configuration

- Resolution precedence (high→low): flags > `ENGRAM_*` env > project config >
  user config. User config default: `$XDG_CONFIG_HOME/engram/config.yaml`
  (fallback `~/.config/engram/config.yaml`).
- Config declares: canonical roots per tier; tier subscriptions; each harness's
  home dir + enable toggle; and **host labels** — a mapping from `hostname -s`
  values to the host identifiers used in `applies_to.hosts`. Host names are
  therefore never compiled into engram; a fresh install knows nothing about any
  specific machine until its config says so.
- engram never edits another program's config silently; `hook print` emits a
  snippet for the user/agent to place, it does not mutate `settings.json`.

## Example invocations

```bash
# Preview what would render for the current session, as an agent sees it:
engram sync --json

# Apply; branch on the exit code, resolve conflicts if any:
engram sync --apply --json || case $? in 3) engram audit --json ;; esac

# Author a memory from an agent-constructed object:
printf '%s' "$memory_json" | engram remember --from-json - --json

# What does Claude Code currently hold from engram, machine-readably:
engram show claude-code --json

# Health leads the agent can act on:
engram review --json | jq '.next_steps[]'

# Discover the surface without scraping help text:
engram schema --json
engram help --json

# Wire session-boundary sync for Claude Code:
engram hook print --json
```

## Non-goals (interface)

- No interactive TUI, no prompts, no pager-gated flows.
- No hidden global state or daemon; every call is stateless.
- No secrets via flags or env; engram handles none.

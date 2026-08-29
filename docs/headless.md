# Headless agent workflows

engram is plumbing; a headless agent is the intelligence. Two integration
shapes realize that split:

1. **Leads (default).** Read commands emit `--json` — a stable envelope with a
   `next_steps` array. An outer agent consumes the leads and calls engram's write
   primitives. engram supplies the facts; the agent decides and acts.
2. **`curate` (self-contained).** The one command that runs an agent itself.
   engram gathers the corpus deterministically, invokes a headless agent to
   *propose* operations, then validates and applies them. The trust boundary is
   preserved by construction: the model only proposes, engram is the sole mutator
   (see "Curate" below).

Both keep the same invariant — a model never mutates canonical directly; every
change goes through engram's validated, idempotent write-path.

## The `--` separator (required for `claude -p`)

`claude`'s `--allowedTools` / `--disallowedTools` are variadic. Without a `--`
before the positional prompt, the parser eats the prompt as another tool rule.
Always place `--` immediately before the prompt:

```
claude -p --allowedTools Read Edit -- "your prompt here"
```

engram encodes this in `internal/agentexec`, so any `claude -p` lead it emits is
already guarded. Do the same in your own scripts.

## Capture at session end

```
memory='{"name":"rg-replace-flag-gotcha","description":"never use rg -r to search","type":"lesson","scope":"global","body":"..."}'
printf '%s' "$memory" | engram remember --from-json - --json
```

Follow the `agent-memory-capture` skill for what is worth remembering.

## Review and act on leads

```
engram review --json | jq -c '.next_steps[]'
```

Each near-duplicate finding's `next_steps[].command` is a runnable, `--`-guarded
`claude -p ... -- "..."` pass that asks the agent to compare the two memories and
merge them if warranted. The `agent-memory-review` skill covers the judgment;
broader scope/promotion calls are left to `curate`.

`migrate` emits a lead too: when it finds a hand-authored file that `diverged`
from canonical, it cannot adopt it deterministically (adopting would overwrite
hand-authored edits), so it reports the file and hands a `curate` `next_step` to
the agent to reconcile. Adopt/ambiguous/skip classifications are surfaced in
`data` for inspection; only divergence needs a judgment call.

## Migrate: adopt what canonical already supersedes

When engram is pointed at a harness that already holds the hand-authored memory
it was imported from, a plain `sync` would duplicate normalized-name lessons and
conflict on same-named ones. `migrate` converts those originals to engram-owned
in place — deterministically (provenance/slug-equality) and only when the body is
identical, so nothing hand-authored is lost.

```
engram migrate claude-code --json          # dry-run: classify adopt/diverged/ambiguous/skip
engram migrate claude-code --apply --json  # adopt the body-identical, provably-superseded originals
engram sync --apply --json                 # now duplicate-free and conflict-free
```

## Curate: engram runs the agent, engram applies

`curate` is the proposer/applier loop. Unlike every other command, it invokes a
headless agent — but the agent only returns *proposed operations*; engram
validates and applies them.

```
engram curate --json                 # dry-run: agent proposes, engram shows the validated plan
engram curate --apply --json         # commit the plan (fail-closed if any op is invalid)
engram curate --harness codex --json # run the codex agent instead of claude
engram curate --model claude-opus-5 --effort high --json   # override the model/effort
```

Flow, and where each layer's authority begins and ends:

1. **engram (deterministic):** gathers every canonical memory + `review`
   findings into a corpus and builds the prompt.
2. **agent (judgment):** returns JSON `operations` — `add` / `update` / `merge` /
   `remove` / `rescope`, each with a `reason`. It is handed the corpus as text
   and needs no tools; it never touches the filesystem.
3. **engram (deterministic):** validates every operation against the corpus and
   the schema. Dry-run emits the per-op verdicts as `data.operations`; `--apply`
   executes the batch through `store` under the canonical write-path.

**Fail closed.** If any proposed operation is invalid (unknown op, target that
does not exist, a memory that fails the schema, a rename smuggled through
`update`), `--apply` applies *nothing* and exits `3`, listing the offenders as
`next_steps`. engram never partially applies a proposal it could not fully
validate.

The default models are `claude-sonnet-5`/`high` and `gpt-5.6-terra`/`high`;
set per-harness defaults under `curate.models.<harness>` in config, or override
per run with `--model` / `--effort`.

## Session-boundary sync (Claude Code hooks)

```
engram hook print
```

prints the `settings.json` fragment; merge it into `~/.claude/settings.json`
(it wires `engram sync --apply --quiet` to SessionStart and Stop).

Codex has no lifecycle hooks: run `engram sync --apply` at session end there, or
wrap it in an agent capture flow.

## Codex exec

```
codex exec "summarize the pending engram actions: $(engram audit --json)"
```

## Readiness

Before relying on a harness, confirm it will actually read what engram writes:

```
engram config --json    # per-harness ready flag + warnings
```

A harness whose native memory feature is off (e.g. Codex `memories = false`) is
reported `ready: false`; `engram sync` still writes but warns that the output
will not be consumed.

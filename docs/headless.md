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
`claude -p ... -- '...'` pass that asks the agent to compare the two memories and
merge them if warranted. Every argument is POSIX single-quoted, so a shell reads it
back as exactly the intended argv and never expands anything in the prompt. The `agent-memory-review` skill covers the judgment;
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
engram curate --apply --json         # ask the agent for a new proposal and commit it unreviewed (fail-closed if any op is invalid)
engram curate --harness codex --json # run the codex agent instead of claude
engram curate --model claude-opus-5 --effort high --json   # override the model/effort
```

### Reviewed plans

A plain `--apply` runs the agent again, and the agent is not deterministic, so
the batch it commits can differ from the one a dry run showed (a plain dry run
keeps no plan, which is why its lead points here). To commit exactly what you
review, make the dry run with `--save-plan`, review the plan it saves, and apply
that file:

```
engram curate --save-plan curate-plan.json --json   # agent proposes; engram validates and saves the plan
engram curate --plan curate-plan.json --json        # optional: re-check it against canonical now (no agent)
engram curate --plan curate-plan.json --apply --json  # apply exactly the saved plan (no agent)
```

Review the saved plan's operations (`data.operations` in the first envelope, or
the file itself) between the two steps. Each memory is shown as apply will
write it: an `update`, or a `merge` reusing a source's name, carries the stored
provenance engram keeps, not whatever the agent wrote. `data.plan.sha256` is
the digest of the plan file's bytes in each step, so a log can match the bytes
applied to the bytes saved or reviewed. It is not a signature and records no
approval: it identifies a file, whoever wrote or edited it.

The plan is bound to canonical as engram read it for the agent: the root, and
the bytes of every file under it. That read holds the canonical lock shared, so
no engram writer changes the store between the binding and the corpus the agent
is shown; the lock is released before the agent runs, which it does with no
lock held. A missing root has no lock to hold, so it is bound and shown as the
empty store it was, not read again. Any change before apply (a `remember`, an
`import`, a `forget`, a hand edit, even one that changes no parsed field) makes
the plan stale: `--apply` refuses it with `stale_plan` (exit `3`), lists the
changed paths in `data.drift`, and writes nothing. Run `--save-plan` again and
review the new plan. The check runs again under the canonical lock immediately
before the batch writes, so a write landing between the re-check and the apply
is caught too. A plan edited by hand is revalidated like any other: it is
refused if it is malformed (`invalid_plan`, exit `2`) or if an operation no
longer validates (`invalid_proposal`, exit `3`), and otherwise applied as
edited. `--save-plan` never overwrites a file and
refuses a path inside the canonical root. See `docs/cli.md` for every code.

Flow, and where each layer's authority begins and ends:

1. **engram (deterministic):** gathers every canonical memory + `review`
   findings into a corpus and builds the prompt. The prompt reaches the agent on
   stdin, not as an argument, so the store's size is not capped by the OS
   argument limit. Under `--apply` or `--save-plan` this read holds the
   canonical lock shared, so a writer holding the lock refuses the run
   (`locked`, exit `1`) before any agent runs; the lock is released before
   step 2. A missing root is neither created nor read: the corpus is empty.
2. **agent (judgment):** returns JSON `operations` — `add` / `update` / `merge` /
   `remove` / `rescope`, each with a `reason`. It is handed the corpus as text
   and needs no tools; it never touches the filesystem.
3. **engram (deterministic):** validates every operation against the corpus and
   the schema. Dry-run emits the per-op verdicts as `data.operations` (and
   `--save-plan` keeps them as a plan file); `--apply` executes the batch
   through `store` under the canonical write-path.

**Fail closed.** If any proposed operation is invalid (unknown op, target that
does not exist, a memory that fails the schema, a rename smuggled through
`update`), `--apply` applies *nothing* and exits `3`, listing the offenders as
`next_steps`. engram never partially applies a proposal it could not fully
validate. A write that fails partway through a valid batch rolls back the batch,
leaving canonical as it was; only a rollback that fails too leaves it partly
applied, reported as `apply_partial`. A store whose `.forgotten` tombstone
directory is a symlink is refused (`linked_history`, exit `1`) before any agent
runs or anything is written, since rollback would restore the link but not
the tombstones written behind it.

**Curated edits stay curated.** An `update` or `merge` keeps the stored
memory's provenance, including import's merge base, so a later import reports
the curated memory as `canonical_ahead` rather than as a conflict with its
unchanged native.

**Removals stay removed.** A `remove`, and each source a `merge` replaces, is
tombstoned, so a later `import` or `reconcile` does not re-create it from a
native source that still exists; it reports the candidate as `forgotten`.

**Orphans are leads, not actions.** `reconcile` (and `import codex`,
`import claude-code --all`) lists each imported memory whose native source is
gone under `orphaned`, with two `next_steps`: `engram forget <name> --apply`
(naming the successor when one Task Group cites the same session) or
`engram detach <name> --apply` to keep it. Choosing between them is judgment;
engram retires nothing on its own.

The default models are `claude-sonnet-5`/`high` and `gpt-5.6-terra`/`high`;
set per-harness defaults under `curate.models.<harness>` in config, or override
per run with `--model` / `--effort`. Each agent run times out after `curate.timeout` (default `20m`,
`0` for none), overridable per run with `--timeout`.

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

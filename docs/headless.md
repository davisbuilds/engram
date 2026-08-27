# Headless agent workflows

engram is plumbing; a headless agent is the intelligence. Read commands emit
`--json` (a stable envelope with a `next_steps` array), and the agent consumes
that and calls engram's write primitives. engram never invokes an agent itself.

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

Each `next_steps[].command` is runnable: an `engram share ...` for a promotion
candidate, or a `--`-guarded `claude -p ... -- "..."` comparison pass for a
near-duplicate. The `agent-memory-review` skill covers the judgment.

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

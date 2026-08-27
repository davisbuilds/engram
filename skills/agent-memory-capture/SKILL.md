---
name: agent-memory-capture
description: At session end, decide what durable lesson (if any) to file into engram's canonical memory via `engram remember`.
---

# Agent Memory Capture

Use at the end of a work session to capture durable, cross-harness lessons into
engram's canonical store. Zero to two memories per session is normal; three is a
red flag that you are capturing procedure, not durable lessons.

## When to capture

- A non-obvious lesson that will apply again (a gotcha, a convention, a decision).
- A user preference stated as a general rule, not a one-off instruction.
- A durable project fact not derivable from the code or git history.

## When NOT to capture

- Anything the repo already records (code structure, past fixes, git history).
- One-off task state or context that only matters to this conversation.

## How to capture

1. Draft the memory: a kebab-case `name`, a one-line `description` (the hook a
   future agent will search for), a `type` (user | feedback | project |
   reference | lesson | preference), and a `scope` (`global` or `project:<repo>`).
2. Write the body: the rule/fact, then a `**Why:**` line, then a
   `**How to apply:**` line.
3. File it, preferring the JSON primitive so the CLI does no parsing:

   ```
   printf '%s' "$memory_json" | engram remember --from-json - --json
   ```

   or with flags:

   ```
   engram remember --name <name> --type lesson --scope global \
     --description "<hook>" --body "<body>" --json
   ```

4. If engram reports `outcome: conflict` (exit 3), a differing canonical memory
   of that name already exists — read it, decide, and only re-run with `--force`
   if you intend to overwrite it.

Canonical is authoritative; engram renders it into every subscribed harness on
the next `engram sync`.

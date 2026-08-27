---
name: agent-memory-review
description: Triage the leads from `engram review` — merge near-duplicates and promote over-narrow memories using engram's own write primitives.
---

# Agent Memory Review

Run `engram review --json` and act on each finding in `next_steps`. engram does
the detection; you make the judgment. Nothing changes until you act.

## Handling each finding kind

### near-duplicate

Two memory names share most of their tokens. Read both canonical memories:

- If they are the same lesson, merge: rewrite one to subsume both and delete the
  other (edit the canonical files, then `engram sync --apply`).
- If they are genuinely distinct, leave them, optionally renaming for clarity.

The lead carries a ready-to-run `claude -p ... -- "<prompt>"` invocation (the
`--` separator is already in place) for a headless comparison pass if you want one.

### promotion-candidate

A `project:<repo>` memory sets no cwd constraint, so it may belong wider. If the
lesson is broadly useful, run the suggested `engram share <name> --to global`. If
it is genuinely project-specific, add a cwd constraint instead of promoting.

## Discipline

- Prefer the fewest, most durable memories. Merging beats accumulating.
- Never overwrite a hand-edited canonical file blindly; read before you write.
- Re-run `engram audit --json` afterward; it should report no pending conflicts.

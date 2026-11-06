---
name: agent-memory-review
description: Review engram memories for semantic overlap, stale claims and scope mistakes, then propose evidence-backed changes through validated curation plans.
---

# Agent Memory Review

Preserve useful knowledge and its conditions. Fewer files, shorter text, similar
names, age and repeated delivery do not establish that a memory should change.
Leaving a memory unchanged is a valid outcome.

## Inspect the evidence

Confirm the active configuration and canonical writer before mutation. Use
`engram help --json` and `engram schema --json` for this executable's commands
and proposal schema; carry the same configuration through inspection and apply.

Start with `engram review --json` and read the named canonical memories in full.
Its near-duplicate findings compare name tokens only: they neither establish
semantic duplication nor cover every stale or overlapping memory. Prefer
submitting your own inspected proposal below. The emitted `curate --json` lead
optionally invokes a no-tools proposer over the whole corpus in dry-run mode;
one pass covers all findings. A lead does not authorize file edits or deletion.

For broader cleanup, `engram status --stats --all-projects --json` supplies
inventory, source paths and reference checks. Inspect completeness, diagnostics
and checked reference forms before relying on absence. Read relevant source
material and current project references; stored size, timestamps, matching
provenance and missing paths are leads, not proof of replacement or staleness.
A path may belong to another configured machine or repository context.

Compare imported content with `engram diff --native <harness> --name <name>
--json`; use `--source-name <candidate>` for a different native identity. Inspect
bodies, provenance, merge-base evidence, ambiguity and incomplete scans. Shared
source history alone does not show that a successor preserves the old lesson.

## Decide what the evidence supports

- **Keep or update:** retain distinct lessons, exceptions, decision rationale,
  references and unresolved uncertainty. Remove or correct a stale claim only
  when current evidence supports the change; uncertainty can remain explicit.
- **Merge:** require semantic overlap and preserve every still-useful condition
  and lesson from the sources. Check resulting scope, narrowing filters,
  references and provenance; avoid combining memories whose different audiences
  or contexts would be lost. Do not invent source lineage.
- **Retire:** establish that the retained knowledge is obsolete or covered by an
  inspected successor. Native absence, common session provenance or an old date
  alone is insufficient.
- **Rescope:** inspect the actual audience and applicability. A project memory
  needs no cwd glob; its absence does not justify promotion or adding a filter.
  Preserve host, agent and cwd restrictions unless evidence supports changing
  them. Use an `update` when changing these filters; `rescope` changes the tier.

For each proposed change, record a reason tied to the inspected evidence and
what knowledge remains. Keep uncertain cases unchanged and report the gap.

## Apply through engram

Write a strict JSON proposal containing an `operations` array, using the schema's
`update`, `merge`, `remove` or `rescope` contract. Include complete memory fields
where required, preserving unchanged optional fields and restrictions. Then
validate and save a plan outside canonical:

```bash
engram curate --proposal proposal.json --save-plan reviewed-plan.json --json
# Inspect the saved operations against current canonical, then:
engram curate --plan reviewed-plan.json --apply --json
```

Saving binds canonical at submission, not the earlier evidence you consulted.
Review the resolved operations and preserved provenance before apply. If the
store drifts, regenerate and review the plan; never bypass a stale-plan refusal.
`curate --save-plan <file>` can instead request a headless proposal for review;
plain `curate --apply` requests a fresh, unreviewed batch.

Do not manually rewrite or delete canonical files or Engram-owned renders.
Curation preserves stored provenance on updates and same-name merges, and
records tombstones for removed identities so native imports cannot revive them.
Unmarked native memories belong to their harness: canonical retirement does not
authorize deleting them. Use the owning native workflow for native changes.

## Verify the affected surfaces

Inspect canonical changes, preserved content, provenance and tombstones, then
preview delivery in the affected contexts. For named content corrections or
missing renders, use `sync --name NAME --harness codex --json` (or
`--harness claude-code`), repeating `--name` as needed; carry configuration and cwd through
preview and apply. Confirm `selected_sync` in this executable's capabilities.
Selected sync creates or updates those renders and their required wiring without
imports or stale removals. It does not clean up old copies after a scope move or
retirement. Native-origin canonical entries can still render to their origin;
this does not adopt or edit the native original.

For broader delivery or cleanup, ordinary `sync` renders all relevant memories;
`reconcile` suppresses source echoes but also imports native content. Inspect the
full action list before choosing either. A small canonical update does not
authorize unrelated imports, new copies or conflict resolution. After applying,
repeat the selected preview for a narrow check, or `engram audit --json` for the
full context. Inspect warnings and conflicts separately.

Check `status` and relevant native comparisons separately for stale index links
or native echoes. A clean render audit does not prove native cleanup or semantic
coverage; tombstones prevent re-import but do not remove native text. Report
remaining native work, incomplete coverage and unknown runtime consumption.

---
date: 2026-10-30
author: Codex
topic: memory-observability
stage: spec
status: in-progress
source: maintainer discussion
risk_profile: routine
readiness: ready
---

# Memory Observability Spec

## Problem

Operators must join native memory, canonical entries and managed renders by hand
to understand coverage. Shared session citations can be mistaken for semantic
replacement, and successful rendering can be mistaken for actual consumption.
These ambiguities increase curation effort and can encourage premature retirement.

## Contract

This spec records six improvements. The first delivery implements safer orphan
leads and unified read-only inventory, verified by `go test ./internal/cli` and
`go test ./...`. Remaining capabilities are follow-ups, not prerequisites for
this delivery. No command in this delivery mutates memories or invokes a model.

| Capability | Observable outcome | Delivery |
| --- | --- | --- |
| Safer orphan leads | Shared-session candidates carry their evidence basis and unassessed semantic coverage; leads request inspection without recommending retirement or detachment. | First slice (implemented) |
| Unified inventory | `status [--all-projects] --json` joins canonical names, native entries and owned render files, with explicit scan boundaries, counts, diagnostics and reasons for canonical absence. | First slice (implemented) |
| Content comparison | A focused native/canonical comparison exposes bodies, source paths, merge-base availability and field differences. | Follow-up |
| Delivery versus consumption | Configuration readiness, stored renders, load eligibility and observed consumption remain distinct; unavailable consumption evidence is unknown. | Follow-up |
| Capability discovery and supplied proposals | Installed command flags/capabilities are discoverable; an outer agent can submit operations for deterministic validation and plan binding without another model invocation. Reviewed saved plans already exist in v0.5.0. | Follow-up |
| Volume and freshness | Per-scope bytes/words, duplicate render volume, source timestamps and unresolved references support curation without turning age or size into automatic retirement. | Follow-up |

## Success Criteria

- Orphan rows retain the compatibility `successors` list, adding candidate
  evidence (`shared_session`, `unassessed`) and the source/session identifiers
  used for the match. No lead asserts that shared provenance proves replacement.
- The first orphan lead is a read-only inspection command. Existing explicit
  `forget` and `detach` commands remain available, with unchanged behavior.
- Inventory reports canonical count, accepted native entries, owned render
  files, distinct normalized native/render names, their overlap and union.
  Files, Task Groups and normalized identities are labeled as different units.
- Default inventory scans the current Claude project plus shared memory and
  Codex's consolidated source and extension notes. `--all-projects` scans all
  existing Claude project slugs, plus the current target and shared memory.
  It does not discover or render arbitrary repositories or contact other hosts.
- Per-name records include native source identifiers and managed render paths;
  names absent from both receive an explanation. Reasons distinguish disabled
  rendering, source suppression, scope exclusion, a missing expected render and
  incomplete evidence. All-project scope evaluation is limited to the current
  context and resolvable scanned project slugs, which are included in output.
- Missing paths are reported as missing, distinct from present empty stores.
  Parse/read failures produce diagnostics, `complete: false`, and exit 1 with
  partial counts explicitly marked incomplete. Unknown files are not counted as
  successfully parsed memories. Nonempty unrecognized Codex sources are flagged.
- A fixture with overlapping native/render names counts the physical entries
  separately and the normalized union once. A different project slug appears
  only with `--all-projects`. A malformed source cannot yield a clean zero.
- Inventory never creates a directory, lock, config or render, including when
  `--apply` is supplied. It reports observed files, not session consumption or
  semantic equivalence. Existing response-envelope schema and commands remain
  compatible; additions are command-specific data and additive orphan fields.

## Evaluation

Use synthetic temporary harness homes and canonical stores. Exercise both
import and reconcile orphan leads, matching sessions with different lessons,
no matches and multiple matches. Test normalized-name overlap, repeated renders,
multiple slugs, ownership markers, scope/host filters, source suppression,
disabled harnesses, absent/empty/malformed sources and read failures. Snapshot
fixture files before/after inventory to establish that inspection writes nothing.
Run race tests, lint, formatting and build checks before PR review. A local
read-only inventory is an operational cross-check, not a consumption test.

## Scope

Included now: safer leads, candidate-match evidence, unified inventory, public
CLI documentation and regression coverage. Follow-ups retain the boundaries in
the capability table. No model-based deduplication, automatic deletion, new
memory ownership, installation upgrades, scheduling or remote transport.

## Assumptions And Constraints

Native identity uses existing importer normalization. One normalized name can
have multiple physical sources; counts do not prove identical content. A shared
session may contain unrelated topics. Scope exclusions are assessed against
reported contexts, never inferred from a count difference. Inventory is a
best-effort filesystem observation, not a cross-harness atomic snapshot.

## Open Questions

None for the first slice. Exact interfaces for follow-up comparison, consumption
evidence, supplied proposals and size/freshness reports remain open; those choices
cannot change this slice's read-only inventory and evidence-labeling contract.

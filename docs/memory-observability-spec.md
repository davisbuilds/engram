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
this delivery. The second delivery adds focused content comparison. The third
adds static delivery evidence with explicit consumption limits. All three slices
are read-only and do not invoke a model.

| Capability | Observable outcome | Delivery |
| --- | --- | --- |
| Safer orphan leads | Shared-session candidates carry their evidence basis and unassessed semantic coverage; leads request inspection without recommending retirement or detachment. | First slice (implemented) |
| Unified inventory | `status [--all-projects] --json` joins canonical names, native entries and owned render files, with explicit scan boundaries, counts, diagnostics and reasons for canonical absence. | First slice (implemented) |
| Content comparison | `diff --native <harness> --name <name>` exposes normalized bodies, source paths, merge-base evidence and field differences. | Second slice (implemented) |
| Delivery versus consumption | Configuration readiness, stored renders, load eligibility and observed consumption remain distinct; unavailable consumption evidence is unknown. | Third slice (implemented) |
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

### Second Slice: Focused Content Comparison

- `diff --native <claude-code|codex> --name <canonical-name>` shows the selected
  canonical memory and matching normalized native entries. Bare `diff` retains
  its existing render-status shape. Bodies are emitted only in explicit named
  comparison mode. Verify with `go test ./internal/cli -run TestNativeDiff`.
- `--source-name <native-name>` permits comparing an orphan with a differently
  named candidate; it never borrows a merge base across names. Claude `--shared`
  explicitly selects a managed shared render, using its render stamp as the base.
- Both sources expose full normalized memory fields, physical source identifiers,
  and stable full-field/content-field difference lists. Normalization follows the
  importers, including Codex echo stripping. Derived scope differences stay visible.
- A recorded base exposes its hash and whether each side matches; no historical
  base text is invented. A missing base, different lineage or ambiguous source
  explicitly prevents binding. Relations describe literal native-authored fields,
  never semantic coverage or an import/apply prediction.
- Missing native/canonical inputs are distinct from parse/read failures. Exit 0
  covers ordinary differences and observed absence; exit 1 includes partial
  evidence and diagnostics; exit 2 rejects invalid argument combinations/names;
  exit 3 shows every ambiguous match and selects none.
- Plain output shows both bodies and metadata, including partial evidence on
  failure. JSON uses the existing response envelope. `--apply` still writes
  nothing. Disabled harnesses may be inspected, with their enabled state shown.
- Orphan leads select a focused comparison for each provenance candidate, without
  recommending retirement. With no candidate they inspect retained canonical
  content and explicitly report native absence in the scanned source.

### Third Slice: Delivery Evidence

- `status` adds `delivery` per harness, with the existing readiness report,
  artifact evidence for each scanned Claude project index, shared index/rules
  import, and Codex extension instructions/notes directory. Existing config and
  readiness fields are preserved; reports add the explicit
  `readiness_basis: configuration_heuristic` label.
- Artifacts distinguish present, empty, missing, different wiring and read
  error. Wiring agreement compares the exact bytes produced by the existing
  writers; differing custom files are not declared invalid. Missing/empty
  prerequisites are explicit; other eligibility stays unknown, even with intact
  files. No effective runtime config, permissions, harness version, index limits
  or per-memory references are inferred. Verify with
  `go test ./internal/cli -run TestDelivery`.
- Consumption is always unknown with reason `no_session_consumption_evidence`.
  Stored counts, file timestamps and readiness never imply loaded or used.
  No harness session or model is invoked. Disabled harnesses and old shared
  routes remain inspectable; all-project routes follow inventory boundaries.
- JSON and plain output show the same evidence without artifact bodies. Read
  errors return partial diagnostics, `complete: false` and exit 1; missing
  artifacts alone remain exit 0. Inspection writes nothing, even with `--apply`.

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

Included now: safer leads, candidate-match evidence, unified inventory, focused
native/canonical comparison, static delivery evidence, public CLI documentation
and regression coverage.
Follow-ups retain the boundaries in the capability table. No model-based deduplication, automatic deletion, new
memory ownership, installation upgrades, scheduling or remote transport.

## Assumptions And Constraints

Native identity uses existing importer normalization. One normalized name can
have multiple physical sources; counts do not prove identical content. A shared
session may contain unrelated topics. Scope exclusions are assessed against
reported contexts, never inferred from a count difference. Inventory is a
best-effort filesystem observation, not a cross-harness atomic snapshot.

## Open Questions

None for the first three slices. Exact interfaces for observed session consumption,
supplied proposals and size/freshness reports remain open; those choices cannot
change these read-only inspection and evidence-labeling contracts.

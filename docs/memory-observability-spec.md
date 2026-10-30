---
date: 2026-10-30
author: Codex
topic: memory-observability
stage: spec
status: complete
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

This spec records six capabilities implemented across five slices. The first
implements safer orphan leads and unified inventory, the second focused content
comparison, and the third static delivery evidence with explicit consumption
limits. The fourth adds discovery and supplied proposal validation/saved plans;
application uses the existing explicit saved-plan workflow. The fifth adds
optional stored-volume, freshness and reference evidence. Inspection modes are
read-only and invoke no model. Verification uses synthetic fixtures, the full
race suite, lint and compiled CLI probes.

| Capability | Observable outcome | Delivery |
| --- | --- | --- |
| Safer orphan leads | Shared-session candidates carry their evidence basis and unassessed semantic coverage; leads request inspection without recommending retirement or detachment. | First slice (implemented) |
| Unified inventory | `status [--all-projects] --json` joins canonical names, native entries and owned render files, with explicit scan boundaries, counts, diagnostics and reasons for canonical absence. | First slice (implemented) |
| Content comparison | `diff --native <harness> --name <name>` exposes normalized bodies, source paths, merge-base evidence and field differences. | Second slice (implemented) |
| Delivery versus consumption | Configuration readiness, stored renders, load eligibility and observed consumption remain distinct; unavailable consumption evidence is unknown. | Third slice (implemented) |
| Capability discovery and supplied proposals | Installed command flags/capabilities are discoverable; an outer agent can submit operations for deterministic validation and plan binding without another model invocation. | Fourth slice (implemented) |
| Volume and freshness | Per-scope bytes/words, duplicate render volume, source timestamps and unresolved references support curation without turning age or size into automatic retirement. | Fifth slice (implemented) |

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

### Fourth Slice: Discovery And Supplied Proposals

- `help --json` and `schema` expose the same command argument metadata, global
  flags and versioned capability identifiers. Existing fields and envelope
  schema 2 remain compatible. Accepted flag spellings/arity derive from dispatch
  declarations, including the actual `remember` FlagSet; metadata does not claim
  to encode every semantic combination. Discovery reads no config or corpus.
- `curate --proposal <file|->` strictly parses an operations object and validates
  it against current canonical, with no model invocation. Unknown/duplicate keys,
  trailing data, missing/null operations and malformed corpora are refused.
  Invalid batches cannot save plans or mutate canonical.
- With `--save-plan`, use existing resolved operations, whole-store binding,
  path guards and owner-only plan writing. Apply only with the separate reviewed
  `--plan ... --apply` path, retaining locked stale checks and rollback. Raw
  proposals cannot combine with apply, plan or agent flags. No model identity is
  fabricated. Report the submitted input's exact byte hash.
- A saved proposal binds its validation snapshot forward, never claims evidence
  of what its author previously read. Plain validation writes no lock or files;
  saving writes a plan and may create a shared-read lock file, without changing
  memories. Verify with `go test ./internal/cli -run 'TestDiscovery|TestSuppliedProposal'`.

### Fifth Slice: Stored Volume And Freshness

- `status --stats [--all-projects]` adds exact stored file bytes and
  Unicode-whitespace word counts, with no bodies or token estimates. Count raw
  readable files (including malformed sources) separately from validated entries.
  Report per-source and per-location volume, and declared canonical scope totals
  with separate parsed body sizes. Keep mixed-scope Codex storage intact and
  unresolved Claude contexts unknown. Verify with `go test ./internal/cli -run TestStats`.
- Filesystem modification timestamps and optional declared provenance dates are
  labeled separately; neither means last use. Report repeated-name render volume
  separately from full-file SHA-256 duplicate copies. No age, size or duplicate
  count automatically recommends retirement or establishes semantic replacement.
- Reference probes cover canonical related names, simple local Claude index
  bullet links and Codex rollout-summary basename citations, skipping code and
  unsupported path forms. Report detector names, unique checked counts and
  missing/not-file/error/unknown states. Incomplete canonical cannot prove a
  related name missing. No recursive traversal, network access or rollout reads.
- Statistics are opt-in, read-only even with `--apply`, and best-effort across
  separate filesystem reads. Measurement/probe failures add diagnostics,
  `complete: false`, partial output and exit 1; missing targets alone remain
  exit 0. JSON/plain show the same evidence. Discovery advertises the flag and
  capability. Test known-positive controls, exact bytes/words, scope/location
  boundaries, divergent same-name copies, duplicate volume, source dates, missing
  versus empty stores, incomplete corpora, probe errors and non-mutation.

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
native/canonical comparison, static delivery evidence, discovery, supplied
proposals with reviewed plans, volume/freshness and bounded reference evidence,
public CLI documentation and regression coverage. No model-based deduplication, automatic deletion, new
memory ownership, installation upgrades, scheduling or remote transport.

## Assumptions And Constraints

Native identity uses existing importer normalization. One normalized name can
have multiple physical sources; counts do not prove identical content. A shared
session may contain unrelated topics. Scope exclusions are assessed against
reported contexts, never inferred from a count difference. Inventory is a
best-effort filesystem observation, not a cross-harness atomic snapshot.

## Open Questions

None for this spec. Actual session-consumption evidence, tokenizer-specific
estimates and broader reference grammars are separate possible follow-ups; the
current detectors and evidence limits are explicit in the CLI contract.

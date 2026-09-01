# engram Backlog

Known gaps and deferred work, noted as they were spotted mid-build. Future work
only; shipped items live in the git history.

## Correctness / robustness

- **Codex import loop-guard: content-hash fallback.** The guard currently skips a
  Task Group when its text carries an engram signal (`extension=engram` marker or
  an `extensions/engram/` citation). The spec (SC-06 / EV-NEG-02) also wants a
  content-hash fallback against current canonical, proven against a *real*
  consolidated engram Task Group. That fixture requires observing what Codex's
  consolidator actually preserves when it folds an extension note; capture one
  and add the fallback + differential test before relying on import in anger.
- **Codex curate has no verified failure-event handling.** `ExtractCodexText` now
  parses the `codex exec --json` JSONL stream and returns the final
  `agent_message`; an exit-0 run with no assistant message is reported as "no
  agent message". A turn that fails *with* exit 0 (if that can happen) would land
  in that same generic error rather than surfacing Codex's own failure text,
  because the error-event shape has not been observed and captured. Capture a real
  failing run and map its event to a specific error message.

## Import quality

- **Slug truncation is ugly-but-lossless; the real fix is a shorter signal.**
  Task Group titles slugify to kebab and hard-cap at 60 chars. A cut that lands
  mid-token (`...connector-availab`) reads badly but is deliberate: the slug is
  an identity, and the trailing partial token is what keeps two long titles that
  share their leading tokens distinct. Trimming back to the last token boundary
  reads cleaner but can collapse both to the same slug — on import the second
  then loses to a `store.Save` name conflict and never reaches canonical (tried
  and reverted; see `Slugify`). The right fix is to derive the name from a
  shorter signal than the full title, not to trim the title. Surfaced by a
  read-only import dry-run against the real Codex MEMORY.md (36 Task Groups).

## Modelling

- **Scope derivation depends on the live filesystem** (mitigated). Import derives
  a memory's scope by resolving a path to a real git repo (single Claude import:
  the cwd; `import --all`: each project slug reconstructed against the filesystem;
  Codex: the Task Group's `applies_to: cwd=`). A repo → `project:<base>`, anything
  else → `global`. The **re-scoping** hazard is now closed: a *provisional* import
  (`import --all`, Codex, reconcile), whose paths may not resolve on this machine,
  never overwrites an existing memory's scope — it preserves the stored scope and
  surfaces a warning; only a *live single import* may revise scope, and only when
  scope is the sole difference (see `internal/cli/decideImportScope`). Residual,
  deferred: a memory imported *for the first time* from a machine lacking its repo
  is still seeded `global` (no prior scope to preserve). A content-hash /
  path-cache / "unresolved vs genuinely-global" signal would let that first import
  distinguish "not a repo here" from "not a repo anywhere" and flag it.
- **`remember` provenance timestamps.** `remember` sets `provenance.origin` but
  not `created`/`modified`, to keep render output deterministic and idempotent.
  A "preserve created, bump modified on change" policy would restore timestamps
  without breaking idempotency.

## Surface not yet built

- **`migrate` for Codex.** `migrate` adopts hand-authored Claude files (per-file
  per-slug memory) that canonical supersedes, so a re-sync into the source slug
  neither duplicates nor conflicts. Codex keeps one consolidated `MEMORY.md`
  folded by its own consolidator, a different duplication shape: engram writes
  marked notes and Codex folds them, so the "hand-authored original vs engram
  render" collision does not arise the same way. Whether Codex needs a migrate
  analogue at all — and if so, what "adopt" means against a consolidated file —
  is unresolved. Decide once real consolidated Codex fixtures exist (ties to the
  content-hash loop-guard item above).
- Skills are project-scoped only (in-repo symlinks). Global install via the
  workspace `~/.agents/skills` + skill-standardizer flow is a later promotion.
- `review` heuristics are deliberately simple (name-token Jaccard for near-dupes,
  unconstrained project scope for promotion). Semantic/LLM near-dupe detection is
  Stage 2 and belongs in an agent flow, not the CLI.

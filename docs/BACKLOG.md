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

- **Codex slug truncation cuts mid-word.** Task Group titles slugify to kebab and
  are capped at 60 chars, which can truncate mid-token
  (`...connector-availab`). Valid kebab, but ugly. Prefer trimming at a token
  boundary, or derive the name from a shorter signal than the full title.
  Surfaced by a read-only import dry-run against the real Codex MEMORY.md
  (36 Task Groups parsed cleanly).

## Modelling

- **Scope derivation depends on the live filesystem.** Import derives a memory's
  scope by resolving a path to a real git repo (single Claude import: the cwd;
  `import --all`: each project slug reconstructed against the filesystem; Codex:
  the Task Group's `applies_to: cwd=`). A repo → `project:<base>`, anything else →
  `global`. That makes import non-reproducible: a project since deleted (or on
  another machine) falls back to `global` — e.g. an orphaned Claude slug whose
  project dir is gone. Safe (never a wrong project), but worth a content-hash or
  path-cache alternative if reproducibility matters later.
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
- **Self-documenting `MEMORY.md` header.** When engram manages entries in a
  Claude `MEMORY.md`, an agent in another session sees `<!-- engram … -->` markers
  with no context. engram could write a one-line managed-by-engram header comment
  at the top of an index it touches, pointing at how the markers work — so any
  agent reading the index gets the pointer inline without an external doc.
- **On-demand `reconcile` convenience.** The steady-state cross-harness flow is
  `import` (each harness) → `review`/`curate` → `sync` (each harness). A single
  `engram reconcile` wrapper could chain the deterministic steps (import both,
  sync both) and print the review/curate leads in between, so the operator runs
  one command instead of five. Keep curate's judgment step explicit, not implicit.
- Skills are project-scoped only (in-repo symlinks). Global install via the
  workspace `~/.agents/skills` + skill-standardizer flow is a later promotion.
- `review` heuristics are deliberately simple (name-token Jaccard for near-dupes,
  unconstrained project scope for promotion). Semantic/LLM near-dupe detection is
  Stage 2 and belongs in an agent flow, not the CLI.

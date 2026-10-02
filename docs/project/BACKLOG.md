# engram Backlog

Future-only gaps and opportunities worth revisiting. Capture recurring friction,
meaningful risk or cost, unresolved decisions, or concrete revisit triggers.
Fix simple, quick, or blocking issues inline when within the active task's scope.

## Conventions

- **Entry:** state **What** and **Why or evidence**. Add **Next** (a useful first
  action) or **Revisit when** (a concrete gate) where helpful; no fixed template
  is required.
- **Evidence:** date and source volatile claims. Support causal or performance
  claims with measurements, or label them **hypothesis, unmeasured**.
- **Delegation:** agents can execute entries directly. Recording a candidate does
  not expand the active task or select a roadmap priority. Use an issue when
  persistent discussion or coordination helps; no mandatory graduation step.
- **Ownership:** keep cross-repository work with the capability-owning repository.
  If an issue owns the details, retain only a useful linked summary here; avoid
  parallel checklists. Keep private evidence out of public entries and issues.
- **Closure:** reconcile affected entries as work lands. Remove resolved concerns,
  retain unresolved remainders, and preserve durable rationale in its owning
  reference. Roadmap records selected direction; Git and PRs hold routine shipped
  history. Revisit the broader list during prioritization or when stale entries
  impede work.

## Correctness / robustness

- **Codex import loop-guard: the group-level signal check.** Import skips a Task
  Group whose text carries `extension=engram` or an `extensions/engram/` citation,
  and strips the `Curated Engram update (DATE):` bullets the consolidator writes
  when it folds a note. In real consolidated output (2026-10) no group carried
  either signal: the consolidator paraphrased notes into labeled bullets, so a
  content-hash fallback (spec SC-06 / EV-NEG-02) would not have matched them. The
  group check still has a false positive: a genuine group that merely mentions
  the `extensions/engram/` path (a Codex session that worked on engram itself) is
  skipped whole. Revisit when: a real consolidation shows a whole-group echo, or
  the false positive drops a genuine group; then narrow the check to lines.
- **Codex curate has no verified failure-event handling.** `ExtractCodexText` now
  parses the `codex exec --json` JSONL stream and returns the final
  `agent_message`; an exit-0 run with no assistant message is reported as "no
  agent message". A turn that fails *with* exit 0 (if that can happen) would land
  in that same generic error rather than surfacing Codex's own failure text,
  because the error-event shape has not been observed and captured. Capture a real
  failing run and map its event to a specific error message.

- **Curate: the corpus goes through argv.** `ClaudeArgvOpts`/`CodexArgvOpts` pass
  the whole prompt (corpus JSON + contract) as one argument, so a store past the
  OS `ARG_MAX` (about 1 MB on macOS) fails at exec with an opaque `agent_run`
  error. Pass the prompt on stdin; verify how `claude -p` and `codex exec` read
  stdin first.
- **Curate: a batch can half-apply.** `curate.Apply` validates the whole batch
  first but has no rollback if a write fails midway (e.g. a directory occupying a
  target path), leaving canonical in neither the before nor the after state.
  Stage writes and commit them in a final rename phase, or report a partial apply
  as its own flagged outcome.
- **Curate: a timed-out agent's helpers can linger as zombies.** The timeout
  SIGKILLs the agent's whole process group at once, so its helpers are reparented
  to init when the leader dies. An init that does not reap (a container's bare
  PID 1, without `--init`) keeps them as zombies, one batch per timed-out run. On
  Linux, marking engram a child subreaper (`PR_SET_CHILD_SUBREAPER`) and reaping
  after the kill would keep them; macOS has no equivalent and launchd reaps.
- **Render applies read canonical without a lock.** `sync --apply` (and
  `reconcile --apply`'s propagation, after it releases the canonical lock)
  renders from a canonical snapshot taken without the lock, so a removal that
  lands after the snapshot (`forget`, `curate remove`) can have its render
  re-created by that sync, until the next sync removes it as `STALE`. A shared
  canonical lock (`LOCK_SH`) held from snapshot through render, against the
  writers' exclusive one, would order them; reconcile would hold its exclusive
  lock through propagation.
- **Unknown frontmatter keys are dropped on re-save.** `schema.Parse` (and
  `remember --from-json`) silently ignore unmapped keys, so one Parse→Render round
  trip (share, a forced remember) strips a hand-added field, and a typo'd
  optional key vanishes instead of erroring. Decide between strict decoding
  (existing files with extra keys would then be withheld) and preserving unknown
  keys through render.
- **`reconcile --apply` ignores the scope force and the Save outcome.** It calls
  `store.Save(..., false)` and checks only the error, discarding the `force` that
  `decideImportScope` computes and any `Conflict` outcome. Inert today (reconcile
  only runs provisional imports), but a trap for an authoritative import path.
- **`scopeIsSoleDiff` is stricter than `store.Plan`.** It requires a full-render
  match including provenance, so an authoritative scope-only rename is refused
  when the stored memory carries one more provenance field than the candidate.
  Compare with provenance neutralized, as `Plan` does.
- **The importer does not flag within-batch name collisions.** Two natives that
  normalize to one name both land in `Result.Memories`; the dry-run now predicts
  the second as a conflict, but the importer should report the collision itself
  (in `Dropped` or its own bucket) with both sources named.

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
- **`curate --apply` re-invokes the agent rather than applying a reviewed plan.**
  The proposer is non-deterministic, so the plan committed by `--apply` can differ
  from the one the operator inspected in the dry-run. A `--plan <file>` (emit the
  validated plan from the dry-run, apply exactly that) would close the
  preview→apply gap the other write commands already guarantee.

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

## CLI contract

- **`--cwd` and symlinks.** `--cwd` is tilde-expanded, made absolute and cleaned,
  and the Claude slug follows the main repository's root (measured 2026-10-02:
  Claude Code keeps auto memory in the repository's slug for a session in a
  subdirectory or a linked worktree; only transcripts go under the cwd's slug).
  Symlinks are still left as given, and the default cwd comes from `os.Getwd`,
  which can return the logical `$PWD` (`/tmp/x`) rather than the physical path
  (`/private/tmp/x`). Unanswered: which form Claude Code slugs. Check a real slug
  created from a symlinked directory before choosing to resolve.
- **Codex relevance ignores the repository root.** Codex targets judge tier and
  in-view by the literal cwd, so from a linked worktree a `project:<repo>`
  memory is neither rendered nor swept. Harmless (Codex keeps one store), but it
  differs from the Claude target, which follows the repository root.

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
  The substring check also has a real false positive: any genuine Task Group that
  merely mentions the `extensions/engram/` path (a Codex session that worked on
  engram itself) is skipped as an echo, indistinguishable in `skipped` from a real
  one. The content-hash fallback is what would let the path check be narrowed.
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
- **Claude-origin memory never reaches other Claude slugs.** Claude loads memory
  per project slug, and `reconcile` propagates only across harnesses, so a
  `global` memory authored natively in one slug is never rendered into another
  (plain `sync` does, but it also echoes every memory back into its own harness).
  A global lesson written in one project stays invisible to the rest unless it
  is authored again there. A reconcile policy that excludes only the *source
  slug*, not the whole harness, would spread it.
- **`remember` provenance timestamps.** `remember` sets `provenance.origin` but
  not `created`/`modified`, to keep render output deterministic and idempotent.
  A "preserve created, bump modified on change" policy would restore timestamps
  without breaking idempotency.
- **A curated edit of an imported memory conflicts with its stale native.** A
  `curate` `merge`/`update` (or any canonical edit) of an imported memory becomes
  a standing import `conflict` against the unchanged native, and a native edited
  after import conflicts the same way until `--refresh`. Recording the
  last-imported native hash would give import a merge base: a native-only edit
  fast-forwards, a canonical-only edit is not re-reported, and only a true
  two-sided edit conflicts. (Removal is handled: `curate` `remove`/`merge`
  tombstone what they take out.)
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

- **`--cwd` and symlinks.** `--cwd` is now tilde-expanded, made absolute and
  cleaned, but symlinks are left as given, and the default cwd comes from
  `os.Getwd`, which can return the logical `$PWD` (`/tmp/x`) rather than the
  physical path (`/private/tmp/x`). Unanswered: which form Claude Code slugs. Check
  a real slug created from a symlinked directory before choosing to resolve.

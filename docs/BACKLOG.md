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
- **Stale apply lock.** `sync` uses an exclusive `.engram.lock` file removed on
  completion. A crash mid-apply leaves the lock behind and blocks the next apply.
  Add staleness detection (age/PID) or a `--force-unlock` escape hatch.

## Modelling

- **Imported scope defaults to `global`.** Both importers set `scope: global`;
  Claude memories are per-project and could map to `project:<repo>` from the cwd
  slug, and Codex groups carry `applies_to: cwd=...`. For now the user re-scopes
  with `engram share`. Consider deriving project scope on import.
- **`remember` provenance timestamps.** `remember` sets `provenance.origin` but
  not `created`/`modified`, to keep render output deterministic and idempotent.
  A "preserve created, bump modified on change" policy would restore timestamps
  without breaking idempotency.

## Surface not yet built

- Headless-agent path: `review` (near-dupe / promotion leads), the
  `claude -p` / `codex exec` exec builder (with the `--` separator guard), the
  `agent-memory-capture` / `agent-memory-review` skills, and `docs/headless.md`.
- `import` currently reads only the current cwd's Claude project memory dir;
  an `--all` mode could sweep every project slug.

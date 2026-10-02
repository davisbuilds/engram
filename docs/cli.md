# engram CLI Design

`engram` is a cross-harness agent-memory bridge. Its operator is a **frontier
coding agent**, not a human at a TTY — so the surface is designed for machine
consumption first, with human-readable output as a courtesy fallback.

This document is the **interface contract** (syntax + behavior). It is
mechanism-free about implementation. The behavioral contract it serves lives in
the (local) design spec.

## Design principles (agent-first)

1. **One stable response envelope.** Every command, under `--json`, returns the
   same top-level object shape. An agent parses one schema, not N.
2. **Structured output by default for non-humans.** Output mode auto-selects:
   JSON when stdout is not a TTY (an agent, a pipe, a hook), human text when it
   is. `--json` / `--plain` force the choice.
3. **Documented exit codes are the fast path.** An agent branches on the exit
   code before parsing a byte. Codes are a stable contract.
4. **Dry-run by default; writes are explicit.** State-changing commands preview
   (compute + report `Action`s) and write nothing until `--apply`. This gives
   the agent a preview→apply loop and makes every write intentional.
5. **Never block on a prompt.** No command waits on interactive input. Safety
   comes from the explicit `--apply` gate and from separate write commands, not
   from confirmation prompts an agent cannot answer.
6. **Self-describing.** `engram schema` emits the machine-readable schemas
   (response envelope + canonical memory); `engram help --json` lists commands.
   An agent discovers the surface without scraping prose.
7. **Leads, not actions, for judgment.** Read commands that find something worth
   acting on (`review`, `audit`) emit `next_steps[]` — the exact follow-up
   invocation — rather than acting. The CLI is plumbing; the agent decides.
8. **Idempotent + stateless.** Every invocation stands alone; re-running a
   command with unchanged inputs is safe and (for `sync --apply`) a no-op.

## Response envelope

Every `--json` result:

```json
{
  "schema_version": 2,
  "command": "sync",
  "ok": true,
  "data": { "...": "command-specific payload" },
  "warnings": ["human-readable non-fatal notes"],
  "error": null,
  "next_steps": [
    { "reason": "why", "command": "engram share foo --to global" }
  ]
}
```

- `ok` — boolean success, redundant with the exit code but present so a captured
  payload is self-contained.
- `data` — command-specific; documented per command.
- `error` — `null` on success, else `{ "code": "stable_slug", "message": "…" }`.
  `code` is a stable branch key; `message` is for humans. A command that works
  on several harnesses (`sync`, `audit`, `diff`, `reconcile`) sets `error` to
  `harness_failed` (exit `1`) when any harness fails, naming each one in
  `message`; the failed harness's entry in `data` still carries its own `error`.
- Lists in `data` are always JSON arrays, `[]` when empty, never `null`.
- `next_steps` — agent-consumable leads; may be empty/absent.
- `schema_version` changes only when the envelope's shape does. Version 2 made
  empty lists `[]` (they were `null`, e.g. a sync result's `applied` and
  `conflicts`) and added the top-level `harness_failed` error (a harness failure
  used to appear only inside `data`).

Primary data (and all JSON) goes to **stdout**; diagnostics, warnings, and human
notes go to **stderr**.

## Exit codes

| Code | Meaning | Agent action |
| ---- | ------- | ------------ |
| `0` | success, including "no actions needed" | proceed |
| `1` | runtime/unexpected error (I/O, parse, internal) | inspect `error.code`, retry or surface |
| `2` | usage/validation error, **or** a write to a disabled harness (strict toggle) | fix the invocation/config |
| `3` | `CONFLICT` actions present and unresolved | resolve the named conflicts, then re-run |

Code `3` is distinct so an agent detects "I must resolve conflicts" from the exit
status alone. `sync --apply` applies all non-conflicting actions and exits `3`
when any `CONFLICT` remains — partial success is observable, not silent.

## Command tree

```
engram [global flags] <command> [args]

  Authoring & propagation (write canonical or render):
    remember     Author a canonical memory (flags, or --from-json - on stdin).
    share        Change where a memory applies: its scope tier (--to) and/or
                 its cwd globs (--applies-cwd, --any-cwd). Writes canonical.
    sync         Render canonical → harnesses. Dry-run; --apply to write.
    import       Reverse-sync a harness's native memory into canonical
                 (explicit, one-shot; dry-run, --apply to write; --all to
                 sweep every Claude project slug, not just the cwd's;
                 --refresh <name> to overwrite a conflicting canonical;
                 --keep <name> to keep canonical and settle the conflict;
                 claude-code --shared for edits made to Claude's shared
                 renders).
    migrate      Adopt hand-authored native memory canonical supersedes,
                 converting it to engram-owned in place so a later sync
                 neither duplicates nor conflicts (dry-run; --apply to write;
                 Claude Code only). The one command allowed to rewrite an
                 unmarked file.
    reconcile    Cross-harness one-shot: import every enabled harness into
                 canonical, surface review leads, and propagate canonical into
                 the harnesses, never back onto a memory's own source
                 (dry-run; --apply to write). The enricher
                 flow in one command; curate stays a separate, explicit step.
    curate       Run a headless agent over the corpus; it proposes
                 add/merge/remove/rescope, engram validates and applies
                 (dry-run; --apply to write). The one command that runs an agent.
    forget       Retire canonical memories: tombstone them so import never
                 re-creates them, and remove their engram-owned renders
                 (dry-run; --apply to write; --restore undoes).
    detach       Stop tracking an imported memory's native source, so a kept
                 orphan is no longer reported (dry-run; --apply to write).

  Introspection (read-only, --json everywhere):
    discover     Parse and list every canonical memory, with parse errors
                 (schema violations and duplicate names included).
    list         List memories relevant to a given cwd / agent / host.
    audit        Report pending render Actions for a harness without writing.
    diff         Cross-state difference for each render target.
    show         Dump a harness's engram-rendered memories.
    review       Health report: flags near-duplicate memory names —
                 emitted as next_steps leads (merge judgment left to the agent).

  Wiring & meta:
    hook         Print harness lifecycle wiring (SessionStart/Stop sync).
    config       Show or validate the resolved configuration.
    schema       Emit engram's JSON schemas (self-describing).
    version      Print the engram version.
    help         Show help (help --json lists commands machine-readably).
```

Verb-first throughout (matches the authoring vocabulary: *remember*, *share*,
*sync*). No abbreviations, no catch-all command.

## Global flags

| Flag | Purpose |
| ---- | ------- |
| `--json` | force structured output (auto when stdout is not a TTY) |
| `--plain` | force stable line-based human output |
| `-q, --quiet` | suppress non-essential stdout (for hooks) |
| `--config <path>` | config file (else `$ENGRAM_CONFIG`, else XDG default) |
| `--cwd <path>` | operate as if invoked from this directory — sets the scope/slug target; `~` is expanded, a relative path is resolved against the process cwd, and the path is cleaned, so `dir/` and `dir` map to one slug. As in Claude Code, the Claude project slug (and which memories belong in it) follows the main repository's root when the path is inside one, so a subdirectory or a linked worktree targets the repository's slug |
| `--agent <claude\|codex>` | caller harness for scope filtering (else inferred) |
| `--host <label>` | override the host label (else `hostname -s` mapped via config) |
| `-h, --help` | help, anywhere in argv; the command is never run |
| `--version` | print version |

Arguments are strict. An unknown flag (including a misspelled or extended one,
such as `--cdw` or `--harnessX`), a value flag given no value or followed by
another flag, and a positional a command does not take are all usage errors
(exit `2`, `error.code = "usage"`) reported before the command runs, so a typo
never runs the command as if the argument were absent.

`--cwd`, `--agent`, and `--host` are the load-bearing agent affordances: a hook
or headless agent runs `engram` on behalf of *another* session whose directory,
harness, and machine differ from engram's own process — these make the target
explicit rather than ambient.

## Command semantics (selected)

- **`remember`** — writes one canonical memory. Accepts fields as flags
  (`--name`, `--description`, `--type`, `--scope`, `--applies-cwd`,
  `--applies-agent`, `--applies-host`, …) **or** a complete `CanonicalMemory`
  as JSON on stdin via `--from-json -` (the agent path: construct once, pipe in).
  Refuses to overwrite a differing canonical of the same name → canonical-side
  `CONFLICT` (exit `3`), never silent data loss; `--force` overwrites
  intentionally (to refresh a canonical memory from an edited native, prefer
  `import --refresh`). A difference confined to `provenance`
  (where a memory came from, not what it says) is never a conflict: empty stored
  provenance fields are backfilled from the incoming memory (only when the two
  agree on origin, so `origin` and `source` never come from different harnesses), a
  populated field keeps its stored value (except import's merge base,
  `import_hash`, which moves forward on agreement), and incoming memory can never
  strip provenance. Every
  canonical writer
  (`remember`, `share`, `import --apply`, `curate --apply`, `forget --apply`,
  `detach --apply`) takes the shared
  exclusive canonical-root lock, so no two writers interleave; a held lock is a
  retryable error (exit `1`, `error.code = "locked"`), not a silent second write.
  An `applies_to.cwd` glob must be `**` or an absolute, clean path, since it is
  matched against an absolute, cleaned cwd. `remember` and `share` spell input
  that way: a leading `~` is expanded to the home directory (as for the global
  `--cwd`) and an absolute glob is cleaned (`/work/` → `/work`). Any other
  relative glob, or an unclean one in a canonical file, fails validation.
- **`share <name> [--to <scope>] [--applies-cwd <glob>]... [--any-cwd]`** —
  changes where one memory applies, keeping everything else, including its
  import provenance and merge base (unlike `remember --force`, which drops the
  merge base). `--to` sets the scope tier, wider or narrower. `--applies-cwd`
  (repeatable) replaces `applies_to.cwd`, and `--any-cwd` clears it; the two are
  mutually exclusive, and at least one change is required. `data` reports
  `from_scope`/`to_scope` and `from_applies_cwd`/`to_applies_cwd`.
- **`sync`** — computes render `Action`s (`CREATE` / `UPDATE` / `STALE` /
  `CONFLICT`) for the relevant memories against the current cwd/agent/host and,
  with `--apply`, executes them under an exclusive lock (idempotent; a second
  concurrent `--apply` blocks or exits non-zero). Without `--apply`: reports and
  writes nothing. When it writes entries into a Claude `MEMORY.md`, it keeps a
  one-line self-documenting header at the top explaining what the per-line `<!--
  engram name=… -->` markers mean and where those memories are authored, so an
  agent reading the index in a later session has that context inline. The header
  is added lazily (only when an entry is written) and removed once no engram
  entries remain, and it never triggers a re-sync on its own.
  `STALE` removal is held back (with a warning) whenever the canonical view may
  be incomplete: the canonical root is missing, or any canonical file fails to
  parse or validate, or two files claim one name (every copy of that name is
  withheld). A render whose canonical merely failed to load is kept until it
  loads again. Codex keeps one notes directory for every cwd, so a Codex note is
  `STALE` only when it is in view from the current cwd: the scope tier its marker
  records is visible here and, while its canonical memory exists, that memory's
  current scope tier and `applies_to` axes (cwd globs, agents, hosts) admit this
  session. The current tier matters because a marker records the tier at render
  time: a memory narrowed from `global` to `project:<repo>` keeps a `global`
  note until that project's run updates it, and runs elsewhere leave it alone
  rather than remove it. A note another project's run rendered is
  left for that project, so the note of a retired project memory is removed by
  the next run from within that project (`forget` removes it at once). A target path that already exists as a hand-authored file (on a
  case-insensitive filesystem, including a case variant of the name) is a
  `CONFLICT`, never an overwrite. Only a `MEMORY.md` line that *ends* with the
  `<!-- engram name=… -->` marker is engram's; a line quoting it mid-text is not.
- **`audit`** — `sync`'s read-only projection: the `Action` list as data, always
  zero side effects.
- **`import <harness>`** — reverse-sync, explicit and one-shot. Dry-run lists every
  candidate memory with the `outcome` `--apply` would produce for it (`created` /
  `updated` / `unchanged` / `canonical_ahead` / `conflict` / `forgotten`);
  `--apply` writes them.
  `forgotten` means a tombstone holds the name (see `forget`): the memory was
  deliberately retired, so it is not written, even with `--force` or `--refresh`,
  and it is not a conflict. Each `conflict`
  result also carries `differs`, a comma-separated list of the fields that differ
  (`description`, `type`, `scope`, `applies_to`, `related`, `provenance`, `body`),
  so the cause is visible without diffing files. A `conflict` carrying `withheld`
  instead means a canonical file claims the name but discovery withheld it (it
  fails to parse or validate, or another file shares the name): that memory is
  never written, even with `--force` or `--refresh`, until the file is fixed, and
  the rest of the batch proceeds. `reconcile` reports the same rows.
  **Import knows which side moved.** Each candidate records
  `provenance.import_hash`, a hash of what the native authors (description, type,
  body), and canonical keeps it as the *merge base* whenever the two agree. When
  they differ: a native edited since, with canonical untouched, **fast-forwards**
  — `updated`, with `differs` naming the fields taken; canonical keeps its own
  scope, `applies_to` and `related`. A canonical edited since (a curated merge,
  a hand edit) with the native untouched is `canonical_ahead`: nothing is written
  and it is not a conflict. Only an edit on both sides, or a memory with no base
  yet, is a `conflict`. The base belongs to one native source (the same origin
  and source file, recorded with its project as `provenance.import_source`): a same-named native from another file or harness, or a name
  two natives in one import both claim, gets no merge-base treatment and
  conflicts as before. A memory imported before merge bases existed gets one
  the first time canonical and native agree (a one-time `updated` with no
  `differs`, as is any provenance-only backfill). **`--refresh <name>`**
  (repeatable, or comma-separated) is the explicit, per-name way to take a
  native's content into canonical over a `conflict` or `canonical_ahead`: the
  named memory is overwritten (reported as `updated`, with `differs` recording
  what was replaced), everything else behaves as without the flag, and a name
  matching no candidate exits `2` (`unknown_refresh`) before any write.
  **`--keep <name>`** is its opposite, for a conflict decided in canonical's
  favor: canonical's content stays, and the native's current hash becomes the
  base, so the memory reads as `canonical_ahead` until the native moves again
  (then it conflicts again, as it should). Across two sources it declines with
  a warning and the conflict stands. An unknown name exits `2`
  (`unknown_keep`), and one name cannot be given to both flags. Marker/loop-guarded
  so engram's own output never round-trips. `import` against a disabled harness
  exits `2`. **Scope is derived, not defaulted:** a memory's scope resolves to
  `project:<repo>` when its source cwd (Claude: the import cwd; Codex: the Task
  Group's `applies_to: cwd=`) is a real git repository, else `global`. Only the
  repo's base name enters the scope — the full path never does. A workspace
  container (a non-repo parent of many projects) correctly stays `global`.
  **A re-import never silently re-scopes an existing memory.** Because that repo
  probe reads the live filesystem, an `import --all` or Codex import run on a
  machine that lacks the repo would derive `global` for a memory that is really
  `project:<repo>`. Such a *provisional* import preserves the memory's stored
  scope rather than widening it, and emits a warning naming both scopes when it
  brings a change to the memory's content; one that changes nothing keeps the
  scope silently, since a scope set by `share` would otherwise warn on every
  run. Only a
  *live single* `import <harness>` (whose cwd is the real session directory) may
  revise scope — an intentional project rename honored — and only when scope is
  the sole change; if the body also diverges it is reported as a `3` conflict for
  a human or `curate` to resolve, never force-overwritten.
  **Native names are normalized to kebab-case** on the way in (a free-text or
  snake_case native name becomes a valid canonical name, matching what the Codex
  path already does). **Nothing is lost silently:** every candidate source lands
  in exactly one of `data.memories`, `data.skipped` (loop guard), or
  `data.dropped` (with a reason) — a file with no frontmatter is recovered from
  its filename and first heading, and one whose frontmatter will not parse is
  reported in `data.dropped` and warned about, never dropped unreported.
  **`--all` (Claude) sweeps every project slug**, not just the cwd's: each slug is
  reconstructed to its real path (resolving the lossy `-`-for-`/` encoding against
  the filesystem) so scope is derived per project; an orphaned slug whose project
  is gone falls back to `global`. Codex keeps one consolidated source, so `--all`
  is accepted there but changes nothing.
  **Orphans are reported, never retired.** `import codex` and
  `import claude-code --all` (the scans that see the whole native source) list
  under `data.orphaned` each canonical memory imported from that harness whose
  source is gone: a Codex name matching no current Task Group, or a Claude
  `provenance.source` file present in no slug. Each row carries `successors`:
  current Task Groups citing a session (`thread_id=`) the orphan cites. Every
  orphan gets two `next_steps`, `forget` it (naming a sole successor) or `detach`
  it. An empty or missing source skips detection with a warning, since it would
  make every memory look orphaned. A single-slug Claude import omits the field.
- **`migrate <harness>`** — the steady-state bridge for a harness that already
  holds hand-authored memory (the slug engram imported *from*). Plain `sync`
  refuses to touch an unmarked file, so a canonical name normalized away from its
  original filename would render as a duplicate, and a same-named original blocks
  updates as a `CONFLICT` forever. `migrate` resolves both by **adopting** the
  hand-authored file — converting it to engram-owned in place — but only when it
  can *prove* canonical supersedes it. Matching is deterministic: recorded import
  `provenance.source`, or slug-equality of the native name (no content similarity —
  that is `curate`'s job). Adoption is gated on **body-identity**: a file is
  adopted only when its body is byte-identical to canonical, so taking ownership
  loses nothing; a file whose body diverged (edited since import, or canonical
  changed) is reported as `diverged` and left byte-for-byte untouched, and a
  non-one-to-one match is reported as `ambiguous` and left alone. Adoption is
  **non-lossy**: it sets only engram's managed frontmatter fields (name,
  description, metadata.type, metadata.origin) and preserves every other key the
  harness wrote (e.g. Claude Code's `node_type` / `originSessionId`) — the same
  preservation `sync` applies on every render, so a migrated file stays idempotent
  and its harness-native metadata survives. `migrate` is the
  **only** command permitted to modify or delete an unmarked file, and only under
  `--apply`; dry-run classifies every candidate (`adopt` / `diverged` /
  `ambiguous` / `skip`) and writes nothing. It emits a `curate` `next_step` for
  diverged files, whose reconciliation is a judgment call engram does not make
  deterministically. Claude Code only — Codex keeps one consolidated `MEMORY.md`
  folded by its own consolidator, a separate follow-up.
- **Claude's shared memory.** Claude Code loads memory per project slug, so a
  memory every project should see is not copied into each slug. A memory that is
  `global`, has no `applies_to.cwd`, and is admitted for `claude` on this host is
  rendered **once** into `<claude home>/engram/memory/` (an ordinary engram-owned
  memory dir with its own `MEMORY.md`), and an engram-owned rules file,
  `<claude home>/rules/engram-memory.md`, `@`-imports that index by absolute path,
  so Claude Code loads it in every session. Each project slug holds only the
  rest (project-tier and cwd-narrowed memories); an engram render of a shared
  memory left in a slug by an earlier sync is removed as `STALE`. The rules file
  is written after the index it imports, rewritten when it drifts, removed once
  nothing is shared (unless `STALE` removal is held), and a hand-authored file at
  its path is a `CONFLICT`, never overwritten. Shared renders appear in `sync`,
  `audit`, `diff` and `reconcile` output as their own target,
  `"harness": "claude-code:shared"`, whose rules-file action is named `_rules`.
  Claude reads a shared memory's file outside the session's working directory,
  which Claude Code gates by permission; allow it once in user settings with
  `Read(<claude home>/engram/**)` (engram never edits Claude Code settings).
  **Edits to a shared render are imported, never overwritten.** Each shared
  render records the hash of the content engram wrote (`metadata.engram_base`,
  the same `description`/`type`/`body` hash as import's merge base). A render
  whose content no longer matches its stamp was edited in place: `sync` holds it
  as a `CONFLICT` (never rewriting or removing it), and `reconcile` imports it
  as its own source, `data.import[]` entry `"harness": "claude-code:shared"`:
  `updated` when canonical has not moved since the stamp (the edit becomes
  canonical and the render is restamped), `unchanged` when canonical already
  says it, `conflict` when canonical moved too, and `invalid` when the edit is
  not a valid memory. A render without a stamp (written before stamping) has no
  known base and is simply updated. **`import claude-code --shared`** settles the
  held ones: a dry-run lists them; `--apply` takes every fast-forwardable edit,
  `--refresh <name>` takes a conflicting edit anyway, and `--keep <name>` keeps
  canonical and discards the edit; it then re-syncs the shared dir, so taken
  edits are restamped and discarded ones overwritten (or removed, for a memory
  no longer shared). A `--refresh` or `--keep` name with no edited render is a
  usage error (`not_edited`). Only `description`, `type` and `body` come from an
  edit; scope, `applies_to` and provenance stay canonical's.
- **`reconcile`** — the on-demand cross-harness convenience: it runs the enricher
  flow — `import` every enabled harness into canonical, `review` for leads, then
  propagate canonical back into the harnesses — in one invocation. Dry-run by
  default; `--apply` writes (all imports save under one canonical lock, then each
  harness is synced). Two policies make it safe and useful: it **never runs
  curate** (near-duplicate/overlap findings are emitted as `next_steps`, judgment
  left explicit), and propagation **never echoes a memory onto its source** — a
  memory imported from Codex is not rendered back into Codex (one store), and a
  memory imported from a Claude project slug is not rendered back into *that
  slug* (its `provenance.import_source`; a Claude-imported memory with none is
  kept out of every slug). It still reaches Claude's shared memory and other
  projects' slugs, so a lesson written in one Claude project reaches the rest
  without conflicting on or overwriting native memory. (Plain `sync` still renders everything, for a fresh
  machine where a harness has no native originals.) Each `data.import[]` entry
  carries the same `forgotten` rows and `orphaned` list as `import`. Exit follows the same codes as
  `sync`: `3` if any propagation `CONFLICT` remains.
- **`forget <name>...`** — retires canonical memories deterministically. Every
  name must exist, or the batch stops before any write (exit `2`,
  `unknown_memory`). Under `--apply`, each memory is tombstoned, then its file
  removed, under the canonical lock; then every **engram-owned** render of the
  name is removed from each Claude project slug and Claude's shared memory dir
  (file and marked index line) and from the shared Codex notes directory, each
  under its own lock; when that leaves nothing shared, the engram-owned rules
  file importing the shared index goes too. A tombstone is
  `<canonical_root>/.forgotten/<name>.yaml`, holding the name, origin, source,
  `forgotten_at`, `--reason`, `--successor`, and the forgotten file's full text;
  discovery never reads that directory. Forgetting a name again (another harness
  reused it) keeps the earlier record as `<name>.<n>.yaml`, so every forgotten
  origin stays blocked. Hand-authored files are never touched:
  each row's `natives` lists where the memory's source still lives (a Claude file,
  which gets an `rm` lead because Claude keeps loading it, or a Codex Task Group,
  which the tombstone keeps out of canonical). `--restore <name>...` writes
  forgotten memories back byte for byte and removes their tombstones; a name held
  by a canonical memory again is refused (exit `3`, `name_taken`), and an unknown
  one exits `2`. Dry-run by default.
- **`detach <name>...`** — keeps an orphan: prefixes an imported memory's
  `provenance.origin` with `detached:`, so orphan detection passes it by. A
  detached memory is an ordinary canonical memory to `reconcile`, which then
  renders it into every harness, its former source included. A memory already
  detached, or not imported, is `unchanged`. An unknown name exits `2`.
- **`show <harness>`** — permissive on a disabled harness (proceeds, stderr note);
  contrast `import` (strict). Read vs write, mapped to filesystem semantics.
  `show claude-code` lists the cwd's project slug and Claude's shared memory dir
  together; each item's `target` is `project` or `shared`.
- **`review`** — never mutates; every finding is a `next_step` the agent may run.
- **`curate`** — the proposer/applier loop, and the **only** command that invokes
  an agent. engram gathers the canonical corpus + `review` findings
  (deterministic), hands them to a headless agent as facts, and the agent returns
  *proposed operations only* (`add` / `update` / `merge` / `remove` / `rescope`
  with reasons) — it never touches a file. engram validates every proposed
  operation against the corpus and the schema; a dry-run reports the plan, and
  `--apply` executes it through the same `store` write-path, holding the shared
  exclusive canonical-root lock so the multi-file batch is atomic against a
  concurrent apply. **Fail closed**: if any operation in the batch is invalid,
  `--apply` applies nothing (exit `3`). An `update`, or a `merge` that reuses a
  source's name, keeps the stored memory's provenance whatever the agent gives,
  so the next import sees canonical as ahead of its native, not in conflict. A
  `remove`, and each source a `merge`
  replaces, is tombstoned (as `forget` does, with the operation's reason), so
  a later import does not bring it back.
  Model/effort are `--model` / `--effort` (flags win over the per-harness config
  default: claude → `claude-sonnet-5`/`high`, codex → `gpt-5.6-terra`/`high`);
  `--harness` picks which agent runs (default `claude-code`). An agent run is
  bounded by `--timeout <duration>` (else `curate.timeout` in config, else
  `20m`; `0` means no deadline): on expiry engram kills the agent and every
  process it started and reports `agent_run` (exit `1`); an invalid duration is
  `invalid_timeout` (exit `2`). The trust boundary is that a model *proposes*
  and engram is the sole *mutator*.
- **`hook print`** — emits the JSON snippet wiring `engram sync --apply --quiet`
  to Claude Code SessionStart/Stop. Codex capture is agent-wrapped (documented).

## Configuration

- Resolution precedence (high→low): flags > `ENGRAM_*` env > project config >
  user config. User config default: `$XDG_CONFIG_HOME/engram/config.yaml`
  (fallback `~/.config/engram/config.yaml`).
- Config declares: canonical roots per tier; tier subscriptions; each harness's
  home dir + enable toggle; and **host labels** — a mapping from `hostname -s`
  values to the host identifiers used in `applies_to.hosts`. Host names are
  therefore never compiled into engram; a fresh install knows nothing about any
  specific machine until its config says so.
- With no `harnesses:` section, every harness is enabled at its default home
  (`~/.claude`, `~/.codex`). Once a config has a `harnesses:` section, a harness
  it does not list is **disabled** (a listed harness with no `home` still gets
  the default home); an empty `harnesses:` key disables every harness. A config naming one harness — a scratch area, say — thus
  never writes the others at the user's real homes; commands skip the unlisted
  harness with a warning saying it was not listed, and `config` reports it.
- engram never edits another program's config silently; `hook print` emits a
  snippet for the user/agent to place, it does not mutate `settings.json`.

## Example invocations

```bash
# Preview what would render for the current session, as an agent sees it:
engram sync --json

# Apply; branch on the exit code, resolve conflicts if any:
engram sync --apply --json || case $? in 3) engram audit --json ;; esac

# Author a memory from an agent-constructed object:
printf '%s' "$memory_json" | engram remember --from-json - --json

# What does Claude Code currently hold from engram, machine-readably:
engram show claude-code --json

# Health leads the agent can act on:
engram review --json | jq '.next_steps[]'

# Discover the surface without scraping help text:
engram schema --json
engram help --json

# Wire session-boundary sync for Claude Code:
engram hook print --json
```

## Non-goals (interface)

- No interactive TUI, no prompts, no pager-gated flows.
- No hidden global state or daemon; every call is stateless.
- No secrets via flags or env; engram handles none.

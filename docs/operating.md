# Operating engram

Start with the [README](../README.md) for installation and a first memory. This
guide covers configuration, trying Engram in isolation, adopting existing memory,
and keeping the canonical store safe. The full command contract is in
[`cli.md`](cli.md).

## Configuration and readiness

Run `./bin/engram config --json` from the built checkout before a first write.
It reports the canonical root and each harness's home/readiness. The default
canonical root is `~/.engram/canonical`; both harnesses are enabled at
`~/.claude` and `~/.codex`. Those defaults do not follow `CLAUDE_HOME` or
`CODEX_HOME`: configure custom homes explicitly.

Engram reads `$XDG_CONFIG_HOME/engram/config.yaml`, falling back to
`~/.config/engram/config.yaml`. To use a separate config, pass
`--config /absolute/path/to/config.yaml` on **every invocation**. A missing
config file silently selects defaults, so confirm the resolved paths before
writing. There is no config-generation or initialization command.

For a custom store and **only Claude Code**, create a YAML file like this,
replacing the example paths with your own absolute paths:

```yaml
canonical_root: /absolute/path/to/private-memory-store
harnesses:
  claude-code:
    home: /absolute/path/to/.claude
```

For **only Codex**, replace the `claude-code` entry with `codex`. To enable both,
list both entries. Once a `harnesses:` section is present, unlisted harnesses
are disabled. Use absolute paths in this YAML: `~` and environment variables are
not expanded there. Keep this machine-specific file out of the public checkout.

Readiness has limits:

- **Claude Code:** `ready` means the configured home exists and Engram is
  enabled. Shared global memories render once into `engram/memory/`; an
  Engram-owned `rules/engram-memory.md` imports their index. If Claude asks for
  permission to read those files, allow `Read(~/.claude/engram/**)` (adjust for
  a custom home). Merge that rule into the existing `permissions.allow` list in
  Claude's settings; do not replace the settings file. Alternatively,
  `shared_index: false` under `harnesses.claude-code` renders relevant memories
  into each project's memory directory when you sync from that project.
- **Codex:** Engram inspects the configured home's `config.toml` for
  `memories = true` or `false`. `false` reports `ready: false`; an unknown setting
  can still report `ready: true` **with a warning**. This lightweight check does
  not verify your Codex version's support for extension notes or that its
  consolidator has run. Sync can succeed while the notes remain unused. Check
  the installed harness's memory support and verify a note is actually consumed.

`config` can exit `0` even with `ok: false`; inspect the payload and warnings.
Engram never changes the harness's settings for you.

`status --json` adds a separate `delivery` report for each harness. Readiness is
explicitly labeled `configuration_heuristic`; stored memories retain their own
counts and source paths. Load paths show project/shared indexes, the shared rules
import and Codex extension instructions/notes. Missing or empty prerequisites
are explicit; intact or custom wiring still leaves runtime eligibility unknown.
Read failures produce partial evidence and exit 1. Consumption is always unknown
because Engram does not inspect a session's actual context. File presence,
timestamps and a successful sync cannot establish that an agent used a memory.

## Try it without touching your agents

After `make build`, run this from the Engram checkout in a POSIX-compatible
shell. It uses disposable directories for **both** harness homes and canonical;
no real agent installation or provider access is required:

```bash
engram_demo=$(mktemp -d)
mkdir -p "$engram_demo/claude" "$engram_demo/codex"
cat > "$engram_demo/config.yaml" <<EOF
canonical_root: $engram_demo/canonical
harnesses:
  claude-code:
    home: $engram_demo/claude
  codex:
    home: $engram_demo/codex
EOF
./bin/engram --config "$engram_demo/config.yaml" config --json
./bin/engram --config "$engram_demo/config.yaml" remember \
  --name explain-check-results --type preference --scope global \
  --description "Explain what was checked and what the result means" \
  --body "When reporting a completed change, name the check you ran, its result, and any remaining uncertainty." --json
./bin/engram --config "$engram_demo/config.yaml" sync --json
```

The unknown Codex-memory warning is expected: these are empty scratch homes,
not running agents. Inspect the proposed actions, then:

```bash
./bin/engram --config "$engram_demo/config.yaml" sync --apply --json
./bin/engram --config "$engram_demo/config.yaml" show claude-code --json
./bin/engram --config "$engram_demo/config.yaml" show codex --json
./bin/engram --config "$engram_demo/config.yaml" sync --json
```

Both `show` results should list `explain-check-results`. The final sync should
have empty action lists: applying unchanged inputs again has nothing to do.
This verifies rendering, not harness consumption. You can inspect all files
inside the temporary directory. When finished, remove only that directory:

```bash
rm -r -- "$engram_demo"
unset engram_demo
```

## Adopt existing memories

Choose the store and harnesses first, and back up existing native memory before
adoption. Use the same config on the preview and apply. The examples below
assume the default config and the built checkout:

```bash
./bin/engram reconcile --json
./bin/engram reconcile --apply --json
```

Reconcile reads **all Claude project slugs** and Codex's consolidated memory,
imports candidates into canonical, surfaces review leads, and renders to the
other eligible targets. It does not run a model, schedule future syncs, or render
an imported memory onto its own native source. Review names, scopes, warnings,
and conflicts before applying; imports on a machine missing a source repository
can initially derive a global scope. Narrow that scope if necessary.

To preview one source instead, use `import claude-code` for the current project,
`import claude-code --all` for every project, or `import codex`. Add `--apply`
only after inspecting the candidates. Import alone does not generally propagate
to the other harness; reconcile does.

If native and canonical diverge, read both before deciding: `import <harness>
--keep <name> --apply` settles in canonical's favor; `--refresh <name> --apply`
deliberately takes the native content. Both are per-name decisions. Plain
`sync` renders everything eligible, including onto native source locations, so
prefer reconcile on the writer when adopting existing memory.

For Claude originals you deliberately want Engram to own, preview `migrate
claude-code` and read [migration semantics](headless.md#migrate-adopt-what-canonical-already-supersedes).
This is the explicit exception to leaving unmarked files alone; do not use it
as a generic conflict fix.

## Scope a memory

With `engram` installed on your PATH, run this from the root of a Git repository
named `sample-app` to retain a lesson only for that project and its working
directory tree:

```bash
engram --cwd "$PWD" remember --name integration-test-server \
  --type lesson --scope project:sample-app --applies-cwd "$PWD/**" \
  --description "Integration tests need the local test server" \
  --body "Start the project's local test server before running its integration suite." --json
```

Use the actual repository basename in `project:<repo>` and your actual lesson.
If you haven't installed the command, use the absolute path to `bin/engram`
instead. `--cwd` selects the session target; it does not infer the authored
memory's scope or directory filter.
The explicit directory glob prevents another repository with the same basename
from receiving the lesson. Linked worktrees have different paths; add their
globs explicitly when wanted. Codex's project-tier matching uses the literal
working directory, while Claude follows the main repository's root.

`remember` and `share` write immediately, without an `--apply` step. To widen a
lesson intentionally, `share <name> --to global --any-cwd` changes its tier and
clears its directory filter; agent and host filters remain. Host-specific memory
requires a `hosts:` mapping in config; an unmapped machine receives none of it.

## Where memories live

Paths below are relative to the configured roots. The source files remain owned
by their harness; canonical is Engram's authority for its marked renders.

| Location | Purpose |
| --- | --- |
| `<canonical_root>/<name>.md` | One Markdown memory with YAML frontmatter |
| `<claude home>/projects/<slug>/memory/` | Native Claude imports and project-specific Engram renders |
| `<claude home>/engram/memory/` | Shared global renders and their `MEMORY.md` index |
| `<claude home>/rules/engram-memory.md` | Engram-owned rule importing the shared index |
| `<codex home>/memories/MEMORY.md` | Codex consolidated memory read by import |
| `<codex home>/memories/extensions/engram/` | Engram's `INSTRUCTIONS.md` and marked `notes/` for Codex consolidation |

Correct canonical rather than editing Codex's generated notes. Claude shared
renders also support an explicit reverse import (`import claude-code --shared`)
on the writer; [the CLI reference](cli.md) covers its conflict rules.
Forgetting a memory removes canonical and marked renders, not native originals
or text Codex has already folded into its own consolidated memory.

## Updates and automation

For a clean checkout on `main`, run `git pull --ff-only` and `make build`, then
check `./bin/engram version`. If you installed to your PATH, rerun `make install`
with the same `PREFIX`. There is no Engram server to restart.

Nothing runs on a schedule by default. [Headless workflows](headless.md) covers
optional capture skills, Claude hook snippets, and agent-proposed curation.
Read the printed hook commands before installing them: the snippet runs plain
`sync --apply`, not reconcile, and a non-default config must be carried into
the hook explicitly. Keep curation separate; even its dry run sends the corpus
to the chosen agent.

## Versioning the canonical store

The canonical store is plain Markdown, meant to live in Git, but engram does not
create a repository or commit for you. Two kinds of file in the canonical root
are runtime state, not memory, and should not be versioned:

```gitignore
# engram runtime files
.engram.lock
.engram-*.tmp
```

`.engram.lock` is the advisory apply lock every mutating command takes
(exclusively to change canonical, shared to render from it), and
`.engram-*.tmp` are the temporaries behind engram's atomic writes. Do version
`.forgotten/`: its tombstones are what keep a retired memory from being imported
again, so a checkout without them resurrects it.

Commit after each canonical change, including `remember`, `share`, and applying
imports or curation (and before one, if you edited canonical by hand, so
the two changes stay separate). The history is then the store's backup and undo:
revert a commit, and the next `sync --apply` renders the reverted state. A copy
of canonical taken before a risky change is unnecessary once every change is a
commit.

## More than one machine

engram does not coordinate machines. Its lock serializes commands on one
machine, and canonical has no merge logic for two writers changing it at once.
Keep one machine as the writer and let the others consume:

- **The writer** runs everything that changes canonical: `import`, `reconcile`,
  `remember`, `share`, `forget`, `detach`, `migrate`, `curate`. It commits each
  change and pushes it to a remote the other machines can reach.
- **A consumer** pulls fast-forward only and runs `sync --apply` for each working
  directory whose memories it needs. `sync` renders without importing, so the
  consumer never writes canonical. engram does not check the checkout, so keep
  it free of local changes yourself.

On a consumer, a memory an agent writes natively stays on that machine, and an
edit made in place to a Claude shared render is held (`sync` reports it as a
`CONFLICT`) but never imported. Make edits on the writer. Allow
`Read(<claude home>/engram/**)` on every machine that renders the shared index;
grant `Edit` only on the writer, where an in-place edit is imported.

A memory meant for some machines only uses `applies_to.hosts`, with each machine
mapped to a host label under `hosts:` in its config (see `cli.md`). The host axis
fails closed, so a machine with no label renders no host-narrowed memory.

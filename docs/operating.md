# Operating engram

How to keep the canonical store safe and how to run engram on more than one
machine. The command contract lives in [`cli.md`](cli.md); this page covers the
choices around it that engram leaves to you.

## Versioning the canonical store

The canonical store is plain Markdown, meant to live in Git, but engram does not
create a repository or commit for you. Two kinds of file in the canonical root
are runtime state, not memory, and should not be versioned:

```gitignore
# engram runtime files
.engram.lock
.engram-*.tmp
```

`.engram.lock` is the advisory apply lock every mutating command takes, and
`.engram-*.tmp` are the temporaries behind engram's atomic writes. Do version
`.forgotten/`: its tombstones are what keep a retired memory from being imported
again, so a checkout without them resurrects it.

Commit after each `--apply` (and before one, if you edited canonical by hand, so
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

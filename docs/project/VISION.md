# Vision

Engram helps agents carry useful learning across sessions and harnesses, so their
operator spends less time restoring context and correcting mistakes they have
already encountered.

Continuity and fewer repeated mistakes are the immediate outcomes. A portable
memory store and reliable synchronization serve those outcomes; a larger corpus
or a successful sync is not, by itself, evidence that an agent works better.

## Who It Serves

Engram is developed first for its maintainer's everyday agent-assisted work.
Practical use should expose which lessons help, which become stale, and how much
attention memory maintenance requires. Other developers should be able to adopt
it without inheriting a particular workspace layout, personal policies, or machine
setup.

A future role in a broader agent infrastructure offering is plausible. Earn that
role through useful integrations and a clear standalone purpose. A platform,
hosted service, or repository consolidation is not required to deliver the current
vision, and none is committed here.

## What Good Continuity Looks Like

Switching harnesses should not require repeating a relevant lesson that was
already learned and deliberately retained. Returning to a project should make
consequential context available in the right scope, with enough provenance to
check it. When circumstances change, correcting or retiring that learning should
be straightforward, without silently overwriting someone else's work.

The goal is useful retained learning, not reconstruction of every conversation or
an exact checkpoint of an unfinished task. A memory may help an agent resume, but
a task handoff owns the active intent, state, and next steps.

## Principles

- **Retain selectively.** Preserve consequential lessons, exceptions, and
  discoveries that would otherwise need relearning. Neither every correction nor
  every completed session warrants a memory. Context and maintenance costs count.
- **Keep scope deliberate.** A lesson from one project, harness, or host does not
  automatically apply elsewhere. Sharing and widening scope should be explicit
  and inspectable.
- **Keep learning correctable.** Preserve useful provenance and uncertainty.
  Repeated mentions are not independent confirmation, and synchronized text is
  not proof of current state. Support correction, conflict handling, and
  retirement as first-class parts of the memory lifecycle.
- **Use judgment where meaning matters.** Agents can assess relevance, reconcile
  lessons, and propose curation. Deterministic code owns the declared mutation,
  ownership, and conflict contracts. More capable models should reduce coaching
  and ceremony without making those contracts implicit.
- **Work with harnesses.** Native memory remains a useful authoring and consumption
  surface. Respect ownership and intentional differences instead of assuming
  every harness needs identical files or that every memory must be adopted into
  a shared store.

## Relationship To Other Knowledge

Choose a home by purpose and ownership, not Markdown format:

- **Standing instructions** carry essential constraints and routing that must be
  available before acting. Concise, broadly relevant safeguards against
  demonstrated silent tool or measurement failures can belong there even when
  they originated as lessons.
- **Guides and capability references** own maintained conventions, procedures,
  and local interfaces. **Skills** supply task-specific capabilities or guidance
  worth loading when needed.
- **Project references** own durable project intent, contracts, and operations;
  source and runtime evidence establish actual behavior.
- **Memory** preserves experience-derived context that would otherwise be lost.
  It can point to these owners without duplicating their procedures or overriding
  current instructions. **Handoffs** preserve ongoing task state.

A recurring lesson can reveal a gap in maintained guidance. When that guidance
takes ownership, retain only useful non-duplicated evidence or a discovery pointer
in memory, or retire the redundant entry. Repetition alone does not justify a new
standing rule, and this is not a mandatory promotion workflow. The best boundary
between these homes remains a design question to refine through real use.

## Current Foundation And Future Decisions

The current implementation provides a canonical Markdown store, scoped harness
renders and imports, reconciliation, and agent-proposed curation. The
[CLI contract](../cli.md), [headless workflows](../headless.md), and
[operating model](../operating.md) own the concrete behavior and limitations.
This vision does not claim automatic relevance selection, universal harness
support, or a measured reduction in mistakes.

Use ordinary work to answer the remaining questions:

- Which lessons prevent repeated explanation or errors, and which add noise?
- How should stale or conflicting lessons be surfaced and corrected with little
  operator effort?
- When should learning stay in memory, become a maintained reference, or be
  forgotten? What evidence should inform that judgment?
- How can an agent discover relevant learning without loading an ever-growing
  instruction layer? Does the intended harness actually expose and use it?

Compare concrete tasks and corrections rather than optimizing memory counts.
Track whether relevant context was available and used, whether known mistakes
recurred, and what restoration or maintenance effort remained. Successful
rendering is necessary for delivery, but does not demonstrate consumption or
benefit. Expand integrations and automation when those observations justify them.

[Backlog](BACKLOG.md) holds implementation candidates and unresolved gaps; it is
not a promise to build every possible memory capability.

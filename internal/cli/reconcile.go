package cli

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/harness"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/review"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
	"github.com/davisbuilds/engram/internal/tombstone"
)

// cmdReconcile is the on-demand cross-harness convenience: it imports each
// enabled harness's native memory into canonical, surfaces review leads, and
// propagates canonical back into the *other* harness(es) — the enricher flow in
// one command instead of five. Dry-run by default; --apply performs the writes.
//
// The deterministic steps run automatically; judgment does not. It never runs
// curate — near-duplicate / overlap findings are emitted as next_steps for the
// operator. Propagation is additive and cross-harness only: a memory imported
// from a harness is not rendered back into that same harness (it already has it,
// natively), so reconcile never fights or overwrites a harness's own memory.
func cmdReconcile(e *env, name string, _ []string) int {
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}

	imports, warns, ierr := s.gatherImports()
	if ierr != nil {
		e.emit(name, false, nil, warns, ierr, nil)
		return exitError
	}
	root := s.cfg.CanonicalRoot
	if e.apply && e.beforeApplyLock != nil {
		e.beforeApplyLock()
	}
	absent := rootAbsent(root)
	saveRoot := ""
	if e.apply {
		release, lerr := canonLock(root)
		if lerr != nil {
			e.emit(name, false, nil, warns, lerr, nil)
			return exitError
		}
		defer release()
		saveRoot = root
	}

	canonical, rerr := readCanon(root, absent && !e.apply)
	if rerr != nil {
		e.emit(name, false, nil, append(warns, canonical.warns...), rerr, nil)
		return exitError
	}
	if absent {
		canonical.holdAbsent(root)
	}
	canonAt := len(warns)
	warns = append(warns, canonical.warns...)
	canonEnd := len(warns)
	tombs, terr := tombstone.Load(root)
	if terr != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "tombstones", Message: terr.Error()}, nil)
		return exitError
	}
	edits, rerr := s.scanSharedEdits()
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}

	merged, importEntries, hadConflict, scopeNotes, serr := mergeImports(saveRoot, canonical.mems, imports, withheldNames(canonical.perrs), tombs)
	warns = append(warns, scopeNotes...)
	if serr != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "save", Message: serr.Error()}, nil)
		return exitError
	}
	exit := exitOK
	if hadConflict {
		exit = exitConflicts
	}
	var orphanNext []NextStep
	for i, imp := range imports {
		orphans, owarn := findOrphans(canonical.mems, imp.harness, imp.result)
		importEntries[i]["orphaned"] = orphans
		if owarn != "" {
			warns = append(warns, owarn)
		}
		orphanNext = append(orphanNext, orphanNextSteps(imp.harness, orphans)...)
	}
	for _, imp := range imports {
		if len(imp.result.Dropped) > 0 {
			warns = append(warns, imp.harness+": some sources could not be imported and were dropped; see data.import[].dropped")
		}
		if len(imp.result.Collisions) > 0 {
			warns = append(warns, imp.harness+": some names are claimed by more than one source; see data.import[].collisions")
		}
	}

	sharedPlans := planSharedEdits(edits, merged, withheldNames(canonical.perrs), tombs, canonical.keepStale, nil)
	if e.apply {
		if serr := applySharedEdits(root, sharedPlans, tombs, canonical.keepStale, nil); serr != nil {
			e.emit(name, false, nil, warns, &RespError{Code: "save", Message: serr.Error()}, nil)
			return exitError
		}
		now, rerr := readCanon(root, false)
		if rerr != nil {
			e.emit(name, false, nil, append(warns, now.warns...), rerr, nil)
			return exitError
		}
		canonical = now
		if absent {
			canonical.holdAbsent(root)
		}
		merged = canonical.mems
		warns = append(append(warns[:canonAt:canonAt], canonical.warns...), warns[canonEnd:]...)
	} else {
		merged = withSharedEdits(merged, sharedPlans)
	}
	if len(sharedPlans) > 0 {
		importEntries = append(importEntries, map[string]any{
			"harness": sync.SharedHarness, "would_import": countOutcome(sharedPlans, editUpdated),
			"results": sharedEditRows(sharedPlans), "orphaned": []map[string]any{},
			"skipped": []string{}, "dropped": []importer.Dropped{}, "collisions": []importer.Collision{},
		})
		for _, p := range sharedPlans {
			if p.held() {
				exit = worseExit(exit, exitConflicts)
			}
		}
	}

	findings := review.Analyze(merged)
	reviewItems := make([]map[string]any, 0, len(findings))
	var next []NextStep
	for _, f := range findings {
		reviewItems = append(reviewItems, map[string]any{"kind": f.Kind, "names": f.Names, "detail": f.Detail})
		next = append(next, e.reviewLead(f))
	}

	next = append(next, orphanNext...)
	next = append(next, sharedEditNextSteps(sharedPlans)...)
	// WHY: sync's generic lead for a held edit defers to the specific ones above.
	planned := map[string]bool{}
	for _, p := range sharedPlans {
		planned[p.edit.Name] = true
	}
	syncLead := func(actions []sync.Action) []NextStep {
		var rest []sync.Action
		for _, a := range actions {
			if !planned[a.Name] || !strings.HasPrefix(a.Note, sync.HeldEditNote) {
				rest = append(rest, a)
			}
		}
		return conflictNextSteps(rest)
	}

	targets, twarns := s.enricherTargets(merged, canonical.keepStale)
	warns = append(warns, twarns...)
	e.at(hookSnapshot)
	syncEntries := make([]map[string]any, 0, len(targets))
	for _, tg := range targets {
		entry := map[string]any{"harness": tg.Harness()}
		if !e.apply {
			actions, err := tg.Plan()
			if err != nil {
				entry["error"] = err.Error()
				exit = worseExit(exit, exitError)
			} else {
				entry["actions"] = orEmpty(actions)
				exit = worseExit(exit, exitForActions(actions))
				next = append(next, syncLead(actions)...)
			}
		} else {
			res, err := tg.Apply()
			if err != nil {
				entry["error"] = err.Error()
				exit = worseExit(exit, exitError)
			} else {
				entry["result"] = res
				if len(res.Conflicts) > 0 {
					exit = worseExit(exit, exitConflicts)
					next = append(next, syncLead(res.Conflicts)...)
				}
			}
		}
		syncEntries = append(syncEntries, entry)
	}

	e.emit(name, exit == exitOK, map[string]any{
		"apply": e.apply, "cwd": s.cwd, "host": s.host,
		"import": importEntries,
		"review": map[string]any{"count": len(findings), "findings": reviewItems},
		"sync":   syncEntries,
	}, warns, harnessFailure(syncEntries), next)
	return exit
}

func mergeImports(saveRoot string, existing []*schema.CanonicalMemory, imports []importGather, withheld map[string]string, tombs tombstone.Set) (merged []*schema.CanonicalMemory, entries []map[string]any, hadConflict bool, notes []string, err error) {
	byName := make(map[string]*schema.CanonicalMemory, len(existing))
	order := make([]string, 0, len(existing))
	add := func(m *schema.CanonicalMemory) {
		if _, ok := byName[m.Name]; !ok {
			order = append(order, m.Name)
		}
		byName[m.Name] = m
	}
	for _, m := range existing {
		add(m)
	}
	for _, imp := range imports {
		outcomes := make([]map[string]string, 0, len(imp.result.Memories))
		for _, m := range imp.result.Memories {
			if tombs.Blocks(m) {
				outcomes = append(outcomes, forgottenRow(m.Name))
				continue
			}
			// WHY: A withheld canonical file still owns its name; store.Save refuses it,
			// so the preview must too, and it must not join the merged set.
			if reason, ok := withheld[m.Name]; ok {
				outcomes = append(outcomes, map[string]string{"name": m.Name, "outcome": string(store.Conflict), "withheld": reason})
				hadConflict = true
				continue
			}
			stored := byName[m.Name]
			scp, force, note := decideImportScope(stored, m, imp.result.ScopeAuthoritative)
			m.Scope = scp
			if note != "" {
				notes = append(notes, note)
			}
			if verr := m.Validate(); verr != nil {
				outcomes = append(outcomes, invalidRow(m.Name, verr))
				hadConflict = true
				continue
			}
			outcome, planned := store.Plan(stored, m)
			if force && (outcome == store.Conflict || outcome == store.CanonicalAhead) {
				outcome, planned = store.Updated, m
			}
			if saveRoot != "" {
				outcome, _, err = store.Save(saveRoot, m, force)
				if err != nil {
					return nil, nil, false, notes, err
				}
			}
			switch outcome {
			case store.Created:
				add(m)
			case store.Updated:
				if planned == nil || planned == stored {
					planned = m
				}
				add(planned)
			case store.Conflict:
				hadConflict = true
			}
			outcomes = append(outcomes, outcomeEntry(m.Name, outcome, stored, m))
		}
		entries = append(entries, map[string]any{
			"harness": imp.harness, "would_import": len(imp.result.Memories),
			"results": outcomes,
			"skipped": orEmpty(imp.result.Skipped), "dropped": orEmpty(imp.result.Dropped),
			"collisions": orEmpty(imp.result.Collisions),
		})
	}
	merged = make([]*schema.CanonicalMemory, 0, len(order))
	for _, n := range order {
		merged = append(merged, byName[n])
	}
	return merged, entries, hadConflict, notes, nil
}

// addRefresh records the comma-separated names of one --refresh value.
func addRefresh(set map[string]bool, csv string) {
	for _, n := range strings.Split(csv, ",") {
		if n = strings.TrimSpace(n); n != "" {
			set[n] = true
		}
	}
}

// unmatchedRefresh returns the --refresh names that match no import candidate,
// sorted for a stable message.
func unmatchedRefresh(refresh map[string]bool, cands []*schema.CanonicalMemory) []string {
	seen := make(map[string]bool, len(cands))
	for _, m := range cands {
		seen[m.Name] = true
	}
	var missing []string
	for n := range refresh {
		if !seen[n] {
			missing = append(missing, n)
		}
	}
	sort.Strings(missing)
	return missing
}

// refreshedEntry is the result row for a conflict the operator chose to overwrite
// with --refresh: the memory is `updated`, and `differs` records what was replaced.
func refreshedEntry(name string, stored, cand *schema.CanonicalMemory) map[string]string {
	return map[string]string{
		"name": name, "outcome": string(store.Updated),
		"differs": strings.Join(store.Diff(stored, cand), ","),
	}
}

// forgottenRow is the result row for an import candidate a tombstone keeps out:
// the memory was deliberately forgotten, so it is neither written nor a conflict.
func forgottenRow(name string) map[string]string {
	return map[string]string{"name": name, "outcome": string(outcomeForgotten)}
}

// outcomeEntry is the per-memory result row shared by every import surface
// (dry-run, apply, reconcile). A conflict, a canonical_ahead and a fast-forward
// also name the differing fields in `differs`, so the cause is visible without
// diffing files by hand.
func outcomeEntry(name string, outcome store.Outcome, stored, cand *schema.CanonicalMemory) map[string]string {
	entry := map[string]string{"name": name, "outcome": string(outcome)}
	if stored == nil {
		return entry
	}
	switch outcome {
	case store.Conflict, store.CanonicalAhead:
		entry["differs"] = strings.Join(store.Diff(stored, cand), ",")
	case store.Updated:
		// WHY: A fast-forward names what it took from the native (description, type,
		// body); a provenance backfill has nothing to name.
		var d []string
		for _, f := range store.Diff(stored, cand) {
			if f == "description" || f == "type" || f == "body" {
				d = append(d, f)
			}
		}
		if len(d) > 0 {
			entry["differs"] = strings.Join(d, ",")
		}
	}
	return entry
}

// importGather is one harness's read-only import result, before any canonical write.
type importGather struct {
	harness string
	result  importer.Result
}

// gatherImports imports every enabled harness's native memory into (in-memory)
// canonical form without writing. A disabled harness is skipped with a warning —
// reconcile operates on all enabled harnesses and does not fail on a disabled one.
func (s *session) gatherImports() ([]importGather, []string, *RespError) {
	var out []importGather
	var warns []string
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		res, err := importer.ImportClaudeAll(h.Home)
		if err != nil {
			return nil, warns, &RespError{Code: "import", Message: err.Error()}
		}
		out = append(out, importGather{harness: config.HarnessClaude, result: res})
	} else {
		warns = append(warns, s.skippedNote(config.HarnessClaude))
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		res, err := importer.ImportCodex(filepath.Join(h.Home, "memories", "MEMORY.md"))
		if err != nil {
			return nil, warns, &RespError{Code: "import", Message: err.Error()}
		}
		out = append(out, importGather{harness: config.HarnessCodex, result: res})
	} else {
		warns = append(warns, s.skippedNote(config.HarnessCodex))
	}
	return out, warns, nil
}

// enricherTargets builds a render target per enabled harness whose desired set
// leaves out the memories whose native originals that target already holds, so
// reconcile propagates lessons without echoing one back onto its source. Codex
// keeps one store, so every Codex-imported memory is left out of Codex. Claude
// keeps one store per project slug, so a Claude-imported memory is left out of
// only the slug it came from: it still reaches the shared target and other
// projects' slugs. (Plain `sync` renders everything; the origin filter is a
// reconcile-specific enricher policy, right for the same-machine case
// reconcile serves.)
func (s *session) enricherTargets(mems []*schema.CanonicalMemory, keepStale bool) ([]sync.Target, []string) {
	var targets []sync.Target
	var warns []string
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		agent := s.agentFor("claude")
		dir := claudeMemoryDir(h.Home, s.claudeRoot)
		rel, shared := s.claudeSplit(h, mems, agent, keepStale)
		rel = excludeSourceSlug(rel, filepath.Base(filepath.Dir(dir)))
		targets = append(targets, sync.ClaudeTarget{MemoryDir: dir, Desired: rel, KeepStale: keepStale}, shared)
		warns = append(warns, harnessWarnings(harness.CheckClaude(h.Home, true))...)
	} else {
		warns = append(warns, s.skippedNote(config.HarnessClaude))
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		rel := excludeOrigin(scope.RelevantFor(mems, s.cwd, s.agentFor("codex"), s.host), config.HarnessCodex)
		targets = append(targets, sync.CodexTarget{
			ExtensionDir: codexExtDir(h.Home), Desired: rel, Now: time.Now, KeepStale: keepStale,
			InView: codexInView(mems, s.cwd, s.agentFor("codex"), s.host),
		})
		warns = append(warns, harnessWarnings(harness.CheckCodex(h.Home, true))...)
	} else {
		warns = append(warns, s.skippedNote(config.HarnessCodex))
	}
	return targets, warns
}

// excludeOrigin drops memories imported from the given harness, so they are not
// propagated back onto their own native originals.
func excludeOrigin(mems []*schema.CanonicalMemory, harnessName string) []*schema.CanonicalMemory {
	out := mems[:0:0]
	for _, m := range mems {
		if originHarness(m) == harnessName {
			continue
		}
		out = append(out, m)
	}
	return out
}

// excludeSourceSlug drops the Claude-imported memories whose original lives in
// the given project slug (the slug in provenance.import_source). One with no
// recorded source could have come from any slug, so it is dropped too.
func excludeSourceSlug(mems []*schema.CanonicalMemory, slug string) []*schema.CanonicalMemory {
	out := mems[:0:0]
	for _, m := range mems {
		if originHarness(m) == config.HarnessClaude {
			src, _, _ := strings.Cut(m.Provenance.ImportSource, "/")
			if src == "" || src == slug {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// originHarness maps a memory's import provenance to the harness it came from, or
// "" for memories not produced by import (authored directly, so they render
// everywhere).
func originHarness(m *schema.CanonicalMemory) string {
	switch {
	case strings.HasPrefix(m.Provenance.Origin, "import:claude-code"):
		return config.HarnessClaude
	case strings.HasPrefix(m.Provenance.Origin, "import:codex"):
		return config.HarnessCodex
	}
	return ""
}

// sourceHarness is the harness a memory was imported from, looking through a
// detach: a detached memory no longer tracks its source, but it still came
// from there.
func sourceHarness(m *schema.CanonicalMemory) string {
	c := *m
	c.Provenance.Origin = strings.TrimPrefix(c.Provenance.Origin, schema.DetachedPrefix)
	return originHarness(&c)
}

// withheldNames maps each name a withheld canonical file claims to why it was
// withheld. An unparseable file's name is taken from its <name>.md filename.
func withheldNames(perrs []discover.ParseError) map[string]string {
	out := map[string]string{}
	for _, pe := range perrs {
		name := pe.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(pe.Path), ".md")
		}
		out[name] = pe.Path + ": " + pe.Err.Error()
	}
	return out
}

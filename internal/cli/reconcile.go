package cli

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/harness"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/review"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
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

	// 1. Import each enabled harness into canonical (read-only gather first).
	imports, warns, ierr := s.gatherImports()
	if ierr != nil {
		e.emit(name, false, nil, warns, ierr, nil)
		return exitError
	}
	exit := exitOK
	importEntries := make([]map[string]any, 0, len(imports))
	if e.apply {
		release, lerr := canonLock(s.cfg.CanonicalRoot)
		if lerr != nil {
			e.emit(name, false, nil, warns, lerr, nil)
			return exitError
		}
		for _, imp := range imports {
			entry := map[string]any{"harness": imp.harness, "would_import": len(imp.result.Memories), "skipped": orEmpty(imp.result.Skipped), "dropped": orEmpty(imp.result.Dropped)}
			outcomes := make([]map[string]string, 0, len(imp.result.Memories))
			for _, m := range imp.result.Memories {
				if verr := m.Validate(); verr != nil {
					outcomes = append(outcomes, map[string]string{"name": m.Name, "outcome": "invalid", "error": verr.Error()})
					exit = worseExit(exit, exitConflicts)
					continue
				}
				outcome, _, serr := store.Save(s.cfg.CanonicalRoot, m, false)
				if serr != nil {
					release()
					e.emit(name, false, nil, warns, &RespError{Code: "save", Message: serr.Error()}, nil)
					return exitError
				}
				if outcome == store.Conflict {
					exit = worseExit(exit, exitConflicts)
				}
				outcomes = append(outcomes, map[string]string{"name": m.Name, "outcome": string(outcome)})
			}
			entry["results"] = outcomes
			importEntries = append(importEntries, entry)
		}
		release()
	} else {
		for _, imp := range imports {
			importEntries = append(importEntries, map[string]any{
				"harness": imp.harness, "would_import": len(imp.result.Memories),
				"memories": memoryItems(imp.result.Memories),
				"skipped":  orEmpty(imp.result.Skipped), "dropped": orEmpty(imp.result.Dropped),
			})
		}
	}
	for _, imp := range imports {
		if len(imp.result.Dropped) > 0 {
			warns = append(warns, imp.harness+": some sources could not be imported and were dropped; see data.import[].dropped")
		}
	}

	// 2. Review the (now-updated under --apply) canonical set for leads.
	mems, perrs, derr := discover.Discover(s.cfg.CanonicalRoot)
	if derr != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "discover", Message: derr.Error()}, nil)
		return exitError
	}
	warns = append(warns, warnParseErrors(perrs)...)
	findings := review.Analyze(mems)
	reviewItems := make([]map[string]any, 0, len(findings))
	var next []NextStep
	for _, f := range findings {
		reviewItems = append(reviewItems, map[string]any{"kind": f.Kind, "names": f.Names, "detail": f.Detail})
		cmd := f.Suggested
		if f.Kind == "near-duplicate" {
			cmd = agentexec.ClaudeCommand("Compare canonical memories "+strings.Join(f.Names, " and ")+" and decide whether to merge them; if so, rewrite one and delete the other.", "Read", "Edit")
		}
		next = append(next, NextStep{Reason: f.Detail, Command: cmd})
	}
	if len(findings) > 0 {
		next = append(next, NextStep{
			Reason:  "review found overlapping memories; curate can merge/rescope them (judgment step, run explicitly)",
			Command: "engram curate --apply",
		})
	}

	// 3. Propagate canonical into the *other* harness(es): cross-harness only.
	targets, twarns := s.enricherTargets(mems)
	warns = append(warns, twarns...)
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
				next = append(next, conflictNextSteps(actions)...)
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
					next = append(next, conflictNextSteps(res.Conflicts)...)
				}
			}
		}
		syncEntries = append(syncEntries, entry)
	}

	if !e.apply {
		warns = append(warns, "dry-run: the sync preview reflects canonical before import; run --apply to reconcile imported memories into the harnesses")
	}

	e.emit(name, exit == exitOK, map[string]any{
		"apply": e.apply, "cwd": s.cwd, "host": s.host,
		"import": importEntries,
		"review": map[string]any{"count": len(findings), "findings": reviewItems},
		"sync":   syncEntries,
	}, warns, nil, next)
	return exit
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
		warns = append(warns, "claude-code disabled; skipped")
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		res, err := importer.ImportCodex(filepath.Join(h.Home, "memories", "MEMORY.md"))
		if err != nil {
			return nil, warns, &RespError{Code: "import", Message: err.Error()}
		}
		out = append(out, importGather{harness: config.HarnessCodex, result: res})
	} else {
		warns = append(warns, "codex disabled; skipped")
	}
	return out, warns, nil
}

// enricherTargets builds a render target per enabled harness whose desired set is
// the scope-relevant memories *excluding those imported from that same harness* —
// so reconcile propagates each harness's lessons to the others, not back onto the
// native originals it already holds. (Plain `sync` renders everything; the origin
// filter is a reconcile-specific enricher policy, right for the same-machine
// cross-harness case reconcile serves.)
func (s *session) enricherTargets(mems []*schema.CanonicalMemory) ([]sync.Target, []string) {
	var targets []sync.Target
	var warns []string
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		rel := excludeOrigin(scope.RelevantFor(mems, s.cwd, s.agentFor("claude"), s.host), config.HarnessClaude)
		targets = append(targets, sync.ClaudeTarget{MemoryDir: claudeMemoryDir(h.Home, s.cwd), Desired: rel})
		warns = append(warns, harnessWarnings(harness.CheckClaude(h.Home, true))...)
	} else {
		warns = append(warns, "claude-code disabled; skipped")
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		rel := excludeOrigin(scope.RelevantFor(mems, s.cwd, s.agentFor("codex"), s.host), config.HarnessCodex)
		targets = append(targets, sync.CodexTarget{ExtensionDir: codexExtDir(h.Home), Desired: rel, Now: time.Now})
		warns = append(warns, harnessWarnings(harness.CheckCodex(h.Home, true))...)
	} else {
		warns = append(warns, "codex disabled; skipped")
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

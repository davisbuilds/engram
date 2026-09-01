package cli

import (
	"bytes"
	"fmt"

	"github.com/davisbuilds/engram/internal/schema"
)

// decideImportScope resolves how an imported candidate interacts with an existing
// canonical memory's scope, so import never lets a machine's filesystem state
// silently re-scope a memory (see docs/BACKLOG.md — "Scope derivation depends on
// the live filesystem").
//
// The rule follows the live-vs-provisional split. A *provisional* import (a
// full-tree sweep, a Codex consolidated-file import, or reconcile) derives scope
// from reconstructed/recorded paths that may not resolve on this machine, so an
// absent repo would fall back to "global" — a silent widening. Such an import
// never re-scopes an existing memory: it preserves the stored scope and surfaces
// a note. An *authoritative* import (a live single import, whose cwd is the real
// session directory) may revise scope — a project rename honored — but only when
// scope is the sole difference, so a curated body is never force-overwritten.
//
// It returns the scope the candidate should carry, whether the save may force a
// scope-only revision, and a non-empty note when a provisional import was held
// back from re-scoping an existing memory (surface it as a warning).
func decideImportScope(existing, cand *schema.CanonicalMemory, authoritative bool) (scope string, forceScopeRevision bool, note string) {
	if existing == nil || existing.Scope == cand.Scope {
		return cand.Scope, false, ""
	}
	if !authoritative {
		note = fmt.Sprintf(
			"scope for %q kept as %q; a provisional import derived %q but does not re-scope an existing memory "+
				"(re-import from within the project, or curate, to change scope)",
			cand.Name, existing.Scope, cand.Scope)
		return existing.Scope, false, note
	}
	if scopeIsSoleDiff(existing, cand) {
		return cand.Scope, true, ""
	}
	// Scope differs but so does the body/metadata: a real content conflict, not a
	// rename. Keep the derived scope and let the save path report the conflict.
	return cand.Scope, false, ""
}

// scopeIsSoleDiff reports whether the only difference between existing and cand is
// their scope: it renders existing with cand's scope substituted and checks that
// the result is byte-identical to cand's render. When true, an authoritative
// import is a clean scope revision (e.g. a repo rename); when false, other fields
// diverged and the change is a genuine content conflict.
func scopeIsSoleDiff(existing, cand *schema.CanonicalMemory) bool {
	probe := *existing
	probe.Scope = cand.Scope
	er, err1 := probe.Render()
	cr, err2 := cand.Render()
	return err1 == nil && err2 == nil && bytes.Equal(er, cr)
}

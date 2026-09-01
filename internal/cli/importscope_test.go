package cli

import (
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
)

// mem builds a single-memory fixture named "m" (these tests each exercise one
// memory, and mergeImports keys on the name to collide candidate with existing).
func mem(scope, desc, body string) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name:        "m",
		Description: desc,
		Type:        schema.TypeReference,
		Scope:       scope,
		Body:        body,
	}
}

// A brand-new memory (no existing canonical) is seeded with its derived scope,
// authoritative or not.
func TestDecideImportScope_NewSeeds(t *testing.T) {
	for _, auth := range []bool{true, false} {
		cand := mem("project:foo", "d", "b")
		got, force, note := decideImportScope(nil, cand, auth)
		if got != "project:foo" || force || note != "" {
			t.Fatalf("auth=%v: got (%q,%v,%q), want (project:foo,false,\"\")", auth, got, force, note)
		}
	}
}

// A provisional (sweep/codex/reconcile) import must never re-scope an existing
// memory: it preserves the stored scope and surfaces a note naming both scopes.
func TestDecideImportScope_ProvisionalPreservesAndNotes(t *testing.T) {
	existing := mem("project:foo", "d", "b")
	cand := mem("global", "d", "b") // repo absent on this machine -> derived global
	got, force, note := decideImportScope(existing, cand, false)
	if got != "project:foo" {
		t.Fatalf("scope: got %q, want project:foo (preserved)", got)
	}
	if force {
		t.Fatalf("provisional preserve must not force-save")
	}
	if note == "" || !strings.Contains(note, "project:foo") || !strings.Contains(note, "global") {
		t.Fatalf("note %q should name both the kept and derived scopes", note)
	}
}

// A provisional import whose derived scope already matches the stored scope is a
// clean no-op: no note, no force.
func TestDecideImportScope_ProvisionalMatchingScopeSilent(t *testing.T) {
	existing := mem("project:foo", "d", "b")
	cand := mem("project:foo", "d", "b")
	got, force, note := decideImportScope(existing, cand, false)
	if got != "project:foo" || force || note != "" {
		t.Fatalf("got (%q,%v,%q), want (project:foo,false,\"\")", got, force, note)
	}
}

// An authoritative (live single import) whose ONLY difference from the stored
// memory is scope may revise it — a rename honored — and signals a forced save.
func TestDecideImportScope_AuthoritativeScopeOnlyRevises(t *testing.T) {
	existing := mem("project:oldname", "d", "b")
	cand := mem("project:newname", "d", "b")
	got, force, note := decideImportScope(existing, cand, true)
	if got != "project:newname" {
		t.Fatalf("scope: got %q, want project:newname (revised)", got)
	}
	if !force {
		t.Fatalf("a scope-only authoritative revision must force-save the new scope")
	}
	if note != "" {
		t.Fatalf("a clean revision needs no note, got %q", note)
	}
}

// An authoritative import whose body ALSO diverges (canonical was curated) is a
// real content conflict, not a scope revision: keep the derived scope but do NOT
// force — the save path reports the conflict for a human/curate to resolve.
func TestDecideImportScope_AuthoritativeBodyDiffDoesNotForce(t *testing.T) {
	existing := mem("project:oldname", "curated desc", "curated body")
	cand := mem("project:newname", "d", "b")
	got, force, _ := decideImportScope(existing, cand, true)
	if got != "project:newname" {
		t.Fatalf("scope: got %q, want project:newname", got)
	}
	if force {
		t.Fatalf("a body divergence must not be force-overwritten as if it were a scope revision")
	}
}

// mergeImports is the shared preview/apply seam for reconcile. A provisional
// import (ScopeAuthoritative=false) that derives a different scope for an existing
// memory must preserve the stored scope — so the simulated outcome is Unchanged,
// not Conflict — and surface a note. This guards the reconcile path against the
// same silent re-scoping the single-import path avoids.
func TestMergeImportsProvisionalPreservesScope(t *testing.T) {
	existing := []*schema.CanonicalMemory{mem("project:foo", "d", "b")}
	// Reconstructed sweep on a machine lacking the repo derives global for the same
	// memory (identical body, only scope differs).
	cand := mem("global", "d", "b")
	imports := []importGather{{
		harness: "claude-code",
		result:  importer.Result{Memories: []*schema.CanonicalMemory{cand}, ScopeAuthoritative: false},
	}}

	merged, entries, hadConflict, notes := mergeImports(existing, imports)

	if hadConflict {
		t.Errorf("preserving scope should not register a conflict")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "project:foo") {
		t.Errorf("expected one preservation note naming project:foo, got %v", notes)
	}
	if cand.Scope != "project:foo" {
		t.Errorf("candidate scope = %q, want project:foo (preserved for apply)", cand.Scope)
	}
	if len(merged) != 1 || merged[0].Scope != "project:foo" {
		t.Errorf("merged memory scope = %v, want project:foo kept", merged)
	}
	got := entries[0]["results"].([]map[string]string)
	if len(got) != 1 || got[0]["outcome"] != "unchanged" {
		t.Errorf("outcome = %v, want unchanged (scope preserved, body identical)", got)
	}
}

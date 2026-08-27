package review

import (
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

func mem(name, scope string, cwd ...string) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name: name, Description: "d", Type: schema.TypeLesson, Scope: scope,
		AppliesTo: schema.AppliesTo{Cwd: cwd},
	}
}

func findingsOfKind(fs []Finding, kind string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestAnalyzeFlagsNearDuplicateNames(t *testing.T) {
	mems := []*schema.CanonicalMemory{
		mem("rg-replace-flag-gotcha", "global"),
		mem("rg-replace-flag", "global"),
		mem("totally-different-thing", "global"),
	}
	dupes := findingsOfKind(Analyze(mems), "near-duplicate")
	if len(dupes) != 1 {
		t.Fatalf("expected 1 near-duplicate finding, got %d: %+v", len(dupes), dupes)
	}
	if len(dupes[0].Names) != 2 {
		t.Errorf("near-duplicate should name both memories: %+v", dupes[0])
	}
}

func TestAnalyzeFlagsPromotionCandidate(t *testing.T) {
	mems := []*schema.CanonicalMemory{
		mem("proj-no-cwd", "project:acme"),                 // project-scoped, no cwd -> candidate
		mem("proj-with-cwd", "project:acme", "/work/acme"), // constrained -> not a candidate
		mem("a-global", "global"),                          // already global -> not a candidate
	}
	cands := findingsOfKind(Analyze(mems), "promotion-candidate")
	if len(cands) != 1 || cands[0].Names[0] != "proj-no-cwd" {
		t.Errorf("expected only proj-no-cwd as a promotion candidate; got %+v", cands)
	}
	if cands[0].Suggested == "" {
		t.Error("a promotion candidate should carry a suggested command")
	}
}

func TestAnalyzeIsReadOnly(t *testing.T) {
	m := mem("proj-no-cwd", "project:acme")
	Analyze([]*schema.CanonicalMemory{m})
	if m.Name != "proj-no-cwd" || m.Scope != "project:acme" {
		t.Error("Analyze must not mutate the memories it inspects")
	}
}

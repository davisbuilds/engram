package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

// imported builds an import candidate: native-authored fields plus the
// provenance an importer records, including the native's hash.
func imported(body string) *schema.CanonicalMemory {
	m := &schema.CanonicalMemory{
		Name: "lesson", Description: "d", Type: schema.TypeLesson, Scope: "global", Body: body,
		Provenance: schema.Provenance{Origin: "import:codex"},
	}
	m.Provenance.ImportHash = schema.NativeHash(m)
	return m
}

// stored is canonical as import last left it, with its merge base recorded.
func stored(body string) *schema.CanonicalMemory {
	return imported(body)
}

func TestNativeHashCoversOnlyNativeAuthoredFields(t *testing.T) {
	a := imported("body\n")
	b := *a
	b.Scope, b.AppliesTo.Cwd, b.Related = "project:x", []string{"/w/*"}, []string{"other"}
	b.Provenance = schema.Provenance{Origin: "remember"}
	if schema.NativeHash(a) == "" || schema.NativeHash(a) != schema.NativeHash(&b) {
		t.Errorf("hash must ignore scope, applies_to, related and provenance")
	}
	for _, edit := range []func(m *schema.CanonicalMemory){
		func(m *schema.CanonicalMemory) { m.Body = "other\n" },
		func(m *schema.CanonicalMemory) { m.Description = "other" },
		func(m *schema.CanonicalMemory) { m.Type = schema.TypeReference },
	} {
		c := *a
		edit(&c)
		if schema.NativeHash(&c) == schema.NativeHash(a) {
			t.Errorf("hash must change with description, type and body")
		}
	}
}

// When canonical and native agree, import records (or refreshes) the base.
func TestPlanRecordsTheBaseWhenImportAgrees(t *testing.T) {
	for label, s := range map[string]*schema.CanonicalMemory{
		"no base":    func() *schema.CanonicalMemory { m := imported("b\n"); m.Provenance.ImportHash = ""; return m }(),
		"stale base": func() *schema.CanonicalMemory { m := imported("b\n"); m.Provenance.ImportHash = "sha256:old"; return m }(),
	} {
		c := imported("b\n")
		out, planned := Plan(s, c)
		if out != Updated || planned.Provenance.ImportHash != c.Provenance.ImportHash {
			t.Errorf("%s: Plan = %s with base %q, want updated with the candidate's base", label, out, planned.Provenance.ImportHash)
		}
	}
}

// Native edited, canonical untouched since the base: take the native's content,
// keep what canonical owns.
func TestPlanFastForwardsANativeOnlyEdit(t *testing.T) {
	s := stored("old\n")
	s.Scope, s.AppliesTo.Agents, s.Related = "project:x", []string{"claude"}, []string{"other"}
	c := imported("new\n")
	out, planned := Plan(s, c)
	if out != Updated {
		t.Fatalf("Plan = %s, want updated (fast-forward)", out)
	}
	if planned.Body != "new\n" || planned.Scope != "project:x" || len(planned.AppliesTo.Agents) != 1 || len(planned.Related) != 1 {
		t.Errorf("fast-forward must take the native body and keep canonical's fields: %+v", planned)
	}
	if planned.Provenance.ImportHash != c.Provenance.ImportHash {
		t.Errorf("fast-forward must move the base to the new native")
	}
}

// Canonical edited, native untouched since the base: nothing to do, no conflict.
func TestPlanCanonicalAhead(t *testing.T) {
	s := stored("original\n")
	base := s.Provenance.ImportHash
	s.Body = "curated\n"
	s.Provenance.ImportHash = base
	out, planned := Plan(s, imported("original\n"))
	if out != CanonicalAhead || planned != s {
		t.Errorf("Plan = %s, want canonical_ahead keeping the stored memory", out)
	}
}

func TestPlanTwoSidedEditOrNoBaseConflicts(t *testing.T) {
	s := stored("original\n")
	s.Body = "curated\n"
	if out, _ := Plan(s, imported("native edit\n")); out != Conflict {
		t.Errorf("both sides moved: Plan = %s, want conflict", out)
	}
	legacy := imported("old\n")
	legacy.Provenance.ImportHash = ""
	if out, _ := Plan(legacy, imported("new\n")); out != Conflict {
		t.Errorf("no base: Plan = %s, want conflict", out)
	}
}

// Only an import candidate carries a base; any other writer (remember without
// --force) still conflicts rather than fast-forwarding.
func TestPlanNeverFastForwardsANonImportCandidate(t *testing.T) {
	s := stored("old\n")
	c := imported("new\n")
	c.Provenance = schema.Provenance{Origin: "remember"}
	if out, _ := Plan(s, c); out != Conflict {
		t.Errorf("Plan = %s, want conflict", out)
	}
}

func TestSaveLeavesCanonicalAheadUnlessForced(t *testing.T) {
	root := t.TempDir()
	s := stored("original\n")
	base := s.Provenance.ImportHash
	s.Body = "curated\n"
	s.Provenance.ImportHash = base
	if _, _, err := Save(root, s, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "lesson.md")
	if out, _, _ := Save(root, imported("original\n"), false); out != CanonicalAhead {
		t.Errorf("Save = %s, want canonical_ahead", out)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "curated") {
		t.Errorf("canonical_ahead must not write:\n%s", got)
	}
	if out, _, _ := Save(root, imported("original\n"), true); out != Updated {
		t.Errorf("forced Save = %s, want updated", out)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "original") {
		t.Errorf("a forced save takes the candidate:\n%s", got)
	}
}

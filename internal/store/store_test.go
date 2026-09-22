package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

func newMem(desc string) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name: "a-mem", Description: desc, Type: schema.TypeLesson, Scope: "global", Body: "body\n",
	}
}

func TestSaveCreatesThenIsIdempotent(t *testing.T) {
	root := t.TempDir()
	m := newMem("d")

	out, path, err := Save(root, m, false)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if out != Created {
		t.Errorf("first save outcome = %q, want created", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("saved file missing: %v", err)
	}

	out2, _, err := Save(root, m, false)
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if out2 != Unchanged {
		t.Errorf("second save outcome = %q, want unchanged (idempotent)", out2)
	}
}

func TestSaveConflictWithoutForcePreservesFile(t *testing.T) {
	root := t.TempDir()
	handPath := filepath.Join(root, "a-mem.md")
	hand := []byte("---\nname: a-mem\ndescription: hand\ntype: user\nscope: global\n---\nmine\n")
	if err := os.WriteFile(handPath, hand, 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := Save(root, newMem("different"), false)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if out != Conflict {
		t.Errorf("outcome = %q, want conflict", out)
	}
	after, _ := os.ReadFile(handPath)
	if !bytes.Equal(hand, after) {
		t.Error("conflicting canonical file must not be overwritten without force")
	}
}

func TestSaveForceOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a-mem.md"),
		[]byte("---\nname: a-mem\ndescription: hand\ntype: user\nscope: global\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := Save(root, newMem("new-desc"), true)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if out != Updated {
		t.Errorf("outcome = %q, want updated", out)
	}
	got, _, found, err := Load(root, "a-mem")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Description != "new-desc" {
		t.Errorf("after force overwrite: found=%v mem=%+v", found, got)
	}
}

// withProv returns a copy of m carrying the given provenance.
func withProv(m *schema.CanonicalMemory, p schema.Provenance) *schema.CanonicalMemory {
	c := *m
	c.Provenance = p
	return &c
}

func TestPlanDecisionTable(t *testing.T) {
	base := newMem("d")
	imported := withProv(base, schema.Provenance{Origin: "import:claude-code"})

	tests := []struct {
		name     string
		existing *schema.CanonicalMemory
		cand     *schema.CanonicalMemory
		want     Outcome
		wantSrc  string // provenance.source on the memory Plan says to persist
	}{
		{"absent is created", nil, imported, Created, ""},
		{"identical is unchanged", imported, imported, Unchanged, ""},
		{
			// The regression: an importer that started stamping provenance.source
			// after canonical was written must backfill it, not conflict forever.
			"provenance-only backfill of an empty field",
			imported,
			withProv(base, schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Updated, "a-mem.md",
		},
		{
			// A differing non-empty provenance value is not a content conflict, and
			// the stored value wins: nothing to write.
			"provenance-only difference in a populated field keeps the stored value",
			withProv(base, schema.Provenance{Origin: "import:codex"}),
			withProv(base, schema.Provenance{Origin: "import:claude-code"}),
			Unchanged, "",
		},
		{
			// Origin and Source together identify where a memory came from; filling
			// only Source under a different Origin would make a hybrid that two
			// consumers (origin-based propagation, source-based migrate) read as
			// different harnesses.
			"a source is not backfilled under a different origin",
			withProv(base, schema.Provenance{Origin: "import:codex"}),
			withProv(base, schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Unchanged, "",
		},
		{
			"a source is backfilled when the origins agree",
			withProv(base, schema.Provenance{Origin: "import:claude-code"}),
			withProv(base, schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Updated, "a-mem.md",
		},
		{
			"an unattributed memory adopts the whole identity of an identical import",
			base,
			withProv(base, schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Updated, "a-mem.md",
		},
		{
			"candidate with no provenance never strips the stored one",
			withProv(base, schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			base,
			Unchanged, "a-mem.md",
		},
		{
			"body change is a conflict",
			imported,
			withProv(&schema.CanonicalMemory{Name: "a-mem", Description: "d", Type: schema.TypeLesson, Scope: "global", Body: "edited\n"},
				schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Conflict, "",
		},
		{
			"description change is a conflict even alongside a provenance backfill",
			imported,
			withProv(newMem("other"), schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"}),
			Conflict, "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, merged := Plan(tc.existing, tc.cand)
			if got != tc.want {
				t.Fatalf("Plan outcome = %q, want %q", got, tc.want)
			}
			if tc.want == Conflict {
				return
			}
			if merged == nil {
				t.Fatal("Plan returned nil memory for a non-conflict outcome")
			}
			if merged.Provenance.Source != tc.wantSrc {
				t.Errorf("merged provenance.source = %q, want %q", merged.Provenance.Source, tc.wantSrc)
			}
		})
	}
}

func TestDiffNamesTheDifferingFields(t *testing.T) {
	a := newMem("d")
	b := newMem("d")
	if got := Diff(a, b); len(got) != 0 {
		t.Errorf("identical memories: Diff = %v, want none", got)
	}

	b.Body = "changed\n"
	b.Provenance.Source = "a-mem.md"
	b.Scope = "project:x"
	got := Diff(a, b)
	want := []string{"scope", "provenance", "body"}
	if len(got) != len(want) {
		t.Fatalf("Diff = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Diff = %v, want %v", got, want)
		}
	}
}

// End to end through the filesystem: the canonical file gains the provenance
// field, keeps its body byte-for-byte, and no force is needed.
func TestSaveBackfillsProvenanceWithoutForce(t *testing.T) {
	root := t.TempDir()
	stored := withProv(newMem("d"), schema.Provenance{Origin: "import:claude-code"})
	if _, _, err := Save(root, stored, false); err != nil {
		t.Fatal(err)
	}

	cand := withProv(newMem("d"), schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"})
	out, _, err := Save(root, cand, false)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if out != Updated {
		t.Fatalf("outcome = %q, want updated (provenance backfill)", out)
	}
	got, _, found, err := Load(root, "a-mem")
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}
	if got.Provenance.Source != "a-mem.md" {
		t.Errorf("provenance.source = %q, want backfilled a-mem.md", got.Provenance.Source)
	}
	if got.Body != stored.Body || got.Description != stored.Description {
		t.Errorf("backfill altered content: %+v", got)
	}

	// Second time round it is a no-op.
	if out2, _, _ := Save(root, cand, false); out2 != Unchanged {
		t.Errorf("repeat outcome = %q, want unchanged", out2)
	}
}

func TestSaveBodyConflictStillRefusesWithoutForce(t *testing.T) {
	root := t.TempDir()
	stored := withProv(newMem("d"), schema.Provenance{Origin: "import:claude-code"})
	if _, _, err := Save(root, stored, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a-mem.md")
	before, _ := os.ReadFile(path)

	cand := withProv(newMem("d"), schema.Provenance{Origin: "import:claude-code", Source: "a-mem.md"})
	cand.Body = "edited natively\n"
	out, _, err := Save(root, cand, false)
	if err != nil {
		t.Fatal(err)
	}
	if out != Conflict {
		t.Fatalf("outcome = %q, want conflict", out)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a genuine content conflict must leave the file untouched, provenance included")
	}
}

func TestLoadNotFound(t *testing.T) {
	_, _, found, err := Load(t.TempDir(), "nope")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("Load should not find a memory in an empty root")
	}
}

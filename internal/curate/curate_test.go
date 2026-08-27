package curate

import (
	"os"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

func mem(name string) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name: name, Description: "d", Type: schema.TypeLesson, Scope: "global", Body: "b\n",
	}
}

func corpus(names ...string) []*schema.CanonicalMemory {
	out := make([]*schema.CanonicalMemory, len(names))
	for i, n := range names {
		out[i] = mem(n)
	}
	return out
}

func TestBuildPromptEmbedsCorpusAndContract(t *testing.T) {
	p, err := BuildPrompt(Corpus{Memories: corpus("alpha", "beta")})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alpha", "beta", "```json", `"operations"`, "op"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestParseProposalFencedBlock(t *testing.T) {
	text := "Here is my plan:\n```json\n{\"operations\":[{\"op\":\"remove\",\"name\":\"x\"}]}\n```\nDone."
	p, err := ParseProposal(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Operations) != 1 || p.Operations[0].Op != "remove" || p.Operations[0].Name != "x" {
		t.Errorf("unexpected proposal: %+v", p)
	}
}

func TestParseProposalBareObjectFallback(t *testing.T) {
	text := `{"operations":[{"op":"remove","name":"y","reason":"has a } brace in a string"}]}`
	p, err := ParseProposal(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Operations) != 1 || p.Operations[0].Name != "y" {
		t.Errorf("unexpected proposal: %+v", p)
	}
}

func TestParseProposalRejectsNoJSON(t *testing.T) {
	if _, err := ParseProposal("I could not decide, sorry."); err == nil {
		t.Error("expected an error when no JSON is present")
	}
}

func TestValidateAcceptsGoodOps(t *testing.T) {
	c := corpus("dup-one", "dup-two", "keep")
	ops := []Operation{
		{Op: OpMerge, Sources: []string{"dup-one", "dup-two"}, Memory: mem("dup-merged")},
		{Op: OpRemove, Name: "keep"},
		{Op: OpRescope, Name: "dup-one", ToScope: "project:acme"},
		{Op: OpAdd, Memory: mem("brand-new")},
	}
	results := Validate(ops, c)
	if !AllValid(results) {
		for _, r := range results {
			if !r.Valid {
				t.Errorf("op %+v invalid: %s", r.Op, r.Error)
			}
		}
	}
}

func TestValidateRejectsBadOps(t *testing.T) {
	c := corpus("real")
	cases := []struct {
		name string
		op   Operation
	}{
		{"unknown op", Operation{Op: "frobnicate", Name: "real"}},
		{"remove missing", Operation{Op: OpRemove, Name: "ghost"}},
		{"add existing", Operation{Op: OpAdd, Memory: mem("real")}},
		{"add invalid memory", Operation{Op: OpAdd, Memory: &schema.CanonicalMemory{Name: "Bad Name"}}},
		{"update rename", Operation{Op: OpUpdate, Name: "real", Memory: mem("renamed")}},
		{"merge one source", Operation{Op: OpMerge, Sources: []string{"real"}, Memory: mem("m")}},
		{"merge ghost source", Operation{Op: OpMerge, Sources: []string{"real", "ghost"}, Memory: mem("m")}},
		{"rescope bad scope", Operation{Op: OpRescope, Name: "real", ToScope: "nonsense"}},
	}
	for _, tc := range cases {
		results := Validate([]Operation{tc.op}, c)
		if results[0].Valid {
			t.Errorf("%s: expected invalid, got valid", tc.name)
		}
	}
}

func TestApplyFailsClosedOnInvalidBatch(t *testing.T) {
	root := t.TempDir()
	seed(t, root, mem("real"))
	// One good op, one bad op: nothing should be applied.
	ops := []Operation{
		{Op: OpRemove, Name: "real"},
		{Op: OpRemove, Name: "ghost"},
	}
	if _, err := Apply(root, ops, corpus("real")); err == nil {
		t.Fatal("expected Apply to refuse an invalid batch")
	}
	if _, _, found, _ := store.Load(root, "real"); !found {
		t.Error("real was deleted despite a fail-closed batch")
	}
}

func TestApplyMergeWritesAndDeletesSources(t *testing.T) {
	root := t.TempDir()
	c := corpus("a", "b")
	seed(t, root, c...)
	ops := []Operation{
		{Op: OpMerge, Sources: []string{"a", "b"}, Memory: mem("ab")},
	}
	applied, err := Apply(root, ops, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Name != "ab" {
		t.Fatalf("unexpected applied: %+v", applied)
	}
	if _, _, found, _ := store.Load(root, "ab"); !found {
		t.Error("merged memory 'ab' was not written")
	}
	for _, gone := range []string{"a", "b"} {
		if _, _, found, _ := store.Load(root, gone); found {
			t.Errorf("source %q should have been deleted", gone)
		}
	}
}

func TestApplyMergeKeepsSourceReusedAsTarget(t *testing.T) {
	root := t.TempDir()
	c := corpus("a", "b")
	seed(t, root, c...)
	// Merged memory reuses source name "a": a stays, b is deleted.
	ops := []Operation{
		{Op: OpMerge, Sources: []string{"a", "b"}, Memory: mem("a")},
	}
	if _, err := Apply(root, ops, c); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := store.Load(root, "a"); !found {
		t.Error("reused source 'a' should be kept")
	}
	if _, _, found, _ := store.Load(root, "b"); found {
		t.Error("source 'b' should be deleted")
	}
}

func TestApplyRescopeChangesScope(t *testing.T) {
	root := t.TempDir()
	c := corpus("m")
	seed(t, root, c...)
	ops := []Operation{{Op: OpRescope, Name: "m", ToScope: "project:acme"}}
	if _, err := Apply(root, ops, c); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ := store.Load(root, "m")
	if got.Scope != "project:acme" {
		t.Errorf("scope = %q, want project:acme", got.Scope)
	}
}

func seed(t *testing.T, root string, mems ...*schema.CanonicalMemory) {
	t.Helper()
	for _, m := range mems {
		if _, _, err := store.Save(root, m, true); err != nil {
			t.Fatalf("seed %s: %v", m.Name, err)
		}
	}
	// sanity: files exist
	if entries, _ := os.ReadDir(root); len(entries) != len(mems) {
		t.Fatalf("seed wrote %d files, want %d", len(entries), len(mems))
	}
}

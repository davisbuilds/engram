package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

// claudeContent must set engram's managed fields while preserving every other
// frontmatter key (top-level and nested under metadata) that the harness wrote.
func TestClaudeContentPreservesUnknownKeys(t *testing.T) {
	existing := []byte("---\n" +
		"name: old-free-text\n" +
		"description: stale desc\n" +
		"metadata:\n" +
		"  node_type: memory\n" +
		"  type: reference\n" +
		"  originSessionId: abc-123\n" +
		"custom_top: keepme\n" +
		"---\n" +
		"the body\n")
	m := &schema.CanonicalMemory{
		Name: "new-name", Description: "new desc", Type: schema.TypeLesson,
		Scope: "global", Body: "the body\n",
	}
	got, err := claudeContent(existing, m)
	if err != nil {
		t.Fatalf("claudeContent: %v", err)
	}
	s := string(got)
	// Managed fields are set from canonical.
	for _, want := range []string{"name: new-name", "description: new desc", "type: lesson", "origin: engram-sync"} {
		if !strings.Contains(s, want) {
			t.Errorf("managed field %q missing:\n%s", want, s)
		}
	}
	// Unmanaged keys survive, top-level and nested.
	for _, keep := range []string{"node_type: memory", "originSessionId: abc-123", "custom_top: keepme"} {
		if !strings.Contains(s, keep) {
			t.Errorf("unmanaged key %q was dropped:\n%s", keep, s)
		}
	}
	// The stale native name/desc must be gone (replaced, not duplicated).
	if strings.Contains(s, "old-free-text") || strings.Contains(s, "stale desc") {
		t.Errorf("stale managed values not replaced:\n%s", s)
	}
	if !isEngramOwned(got) {
		t.Errorf("result is not engram-owned:\n%s", s)
	}
}

// A file the pure renderer produced must round-trip byte-identically through
// claudeContent, or the first sync after this change would reindent every
// existing engram-owned file and flag it as a spurious UPDATE.
func TestRenderRoundTripsThroughClaudeContent(t *testing.T) {
	m := &schema.CanonicalMemory{
		Name: "a-b-c", Description: "has: a colon, and \"quotes\"",
		Type: schema.TypeLesson, Scope: "global", Body: "body line\n",
	}
	rr, err := render.ClaudeRenderer{}.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := claudeContent(rr.Content, m)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(rr.Content) {
		t.Errorf("claudeContent reindented an existing render (spurious UPDATE risk):\n--render--\n%s\n--merged--\n%s", rr.Content, got)
	}
}

// A frontmatter-less existing file has no keys to preserve; claudeContent falls
// back to the schema-only render.
func TestClaudeContentNoFrontmatterFallsBackToRender(t *testing.T) {
	m := &schema.CanonicalMemory{
		Name: "x", Description: "d", Type: schema.TypeLesson, Scope: "global", Body: "b\n",
	}
	got, err := claudeContent([]byte("no frontmatter here\n"), m)
	if err != nil {
		t.Fatalf("claudeContent: %v", err)
	}
	if !isEngramOwned(got) || !strings.Contains(string(got), "name: x") {
		t.Errorf("fallback render wrong:\n%s", got)
	}
}

// Migrate adoption must carry the native file's harness-specific frontmatter
// (node_type, originSessionId) through, not strip it — the non-lossy guarantee.
func TestMigrateAdoptPreservesNativeFrontmatter(t *testing.T) {
	dir := t.TempDir()
	body := "kept body\n"
	native := "---\nname: Free Text Name\ndescription: nd\nmetadata:\n  node_type: memory\n  type: reference\n  originSessionId: sess-xyz\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "free_text_name.md"), []byte(native), 0o644); err != nil {
		t.Fatal(err)
	}
	m := memBody("free-text-name", body)

	if _, err := (ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "free-text-name.md"))
	if err != nil {
		t.Fatalf("adopted file missing: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "origin: engram-sync") {
		t.Errorf("adopted file not engram-owned:\n%s", s)
	}
	for _, keep := range []string{"node_type: memory", "originSessionId: sess-xyz"} {
		if !strings.Contains(s, keep) {
			t.Errorf("adoption dropped native key %q:\n%s", keep, s)
		}
	}

	// And a subsequent sync sees it as in-sync: preservation must not break idempotency.
	assertSyncClean(t, dir, m)
}

// Sync on an owned file that carries extra keys is a no-op (idempotent), and when
// canonical changes, the UPDATE still preserves the extra keys.
func TestSyncPreservesUnknownKeysAndStaysIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := memBody("kept-mem", "b\n")
	// Seed an engram-owned file (in canonical form) that also carries an unmanaged
	// key, by running it through claudeContent — the same path sync writes — so the
	// idempotency check reflects a real steady-state file, not hand-written YAML.
	rawWithExtra := []byte("---\nname: kept-mem\nmetadata:\n  origin: engram-sync\n  node_type: memory\n---\nb\n")
	seed, err := claudeContent(rawWithExtra, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kept-mem.md"), seed, 0o644); err != nil {
		t.Fatal(err)
	}
	// Index line must exist and be marked, or sync flags an index UPDATE.
	rr, err := render.ClaudeRenderer{}.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeIndexLines(dir, []string{rr.IndexLine}); err != nil {
		t.Fatal(err)
	}

	// Idempotent: no action despite the extra node_type key.
	if acts := mustPlan(t, target(dir, m)); len(acts) != 0 {
		t.Fatalf("expected no actions (idempotent), got %+v", acts)
	}

	// Change canonical → UPDATE, and the extra key must survive the rewrite.
	m2 := memBody("kept-mem", "b\n")
	m2.Description = "changed description"
	if _, err := target(dir, m2).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "kept-mem.md"))
	if !strings.Contains(string(got), "node_type: memory") {
		t.Errorf("UPDATE dropped the unmanaged key:\n%s", got)
	}
	if !strings.Contains(string(got), "changed description") {
		t.Errorf("UPDATE did not apply the new description:\n%s", got)
	}
}

// P2: a canonical value that resolves as bool/int/null must stay a string after
// merge, not flip type. (name "true" and "123" are both valid kebab names.)
func TestClaudeContentKeepsManagedValuesAsStrings(t *testing.T) {
	existing := []byte("---\nname: x\ndescription: d\nmetadata:\n  type: lesson\n  origin: engram-sync\n---\nb\n")
	for _, tc := range []struct{ name, desc string }{
		{"true", "null"},
		{"123", "42"},
	} {
		m := &schema.CanonicalMemory{Name: tc.name, Description: tc.desc, Type: schema.TypeLesson, Scope: "global", Body: "b\n"}
		got, err := claudeContent(existing, m)
		if err != nil {
			t.Fatalf("claudeContent: %v", err)
		}
		// Re-parse via the ownership path's frontmatter and assert string identity.
		var fm struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal(frontmatterBytes(got), &fm); err != nil {
			t.Fatalf("reparse: %v\n%s", err, got)
		}
		if fm.Name != tc.name || fm.Description != tc.desc {
			t.Errorf("type flipped: name=%q desc=%q, want %q/%q\n%s", fm.Name, fm.Description, tc.name, tc.desc, got)
		}
	}
}

// P2: a native file whose `metadata` is a non-mapping (scalar) must not produce
// duplicate `metadata` keys — the result must still parse and be engram-owned.
func TestClaudeContentReplacesNonMappingMetadata(t *testing.T) {
	existing := []byte("---\nname: x\ndescription: d\nmetadata: just-a-scalar\ntop_keep: v\n---\nb\n")
	m := &schema.CanonicalMemory{Name: "x", Description: "d", Type: schema.TypeLesson, Scope: "global", Body: "b\n"}
	got, err := claudeContent(existing, m)
	if err != nil {
		t.Fatalf("claudeContent: %v", err)
	}
	if !isEngramOwned(got) {
		t.Errorf("result not parseable/engram-owned (duplicate metadata key?):\n%s", got)
	}
	// The unrelated top-level key still survives the metadata replacement.
	if !strings.Contains(string(got), "top_keep: v") {
		t.Errorf("unrelated key dropped:\n%s", got)
	}
}

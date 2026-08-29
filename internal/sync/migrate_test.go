package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

// --- helpers ------------------------------------------------------------------

func memBody(name, body string) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name: name, Description: "desc of " + name, Type: schema.TypeLesson,
		Scope: "global", Body: body,
	}
}

func memProv(name, body, source string) *schema.CanonicalMemory {
	m := memBody(name, body)
	m.Provenance = schema.Provenance{Origin: "import:claude-code", Source: source}
	return m
}

// writeHand writes a hand-authored (unmarked) Claude memory file with frontmatter
// and appends its unmarked MEMORY.md index line.
func writeHand(t *testing.T, dir, fileBase, fmName, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + fmName + "\nmetadata:\n    type: lesson\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, fileBase+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	appendIndexLine(t, dir, "- ["+fmName+"]("+fileBase+".md) — hook")
}

func appendIndexLine(t *testing.T, dir, line string) {
	t.Helper()
	lines := readIndexLines(dir)
	if err := writeIndexLines(dir, append(lines, line)); err != nil {
		t.Fatal(err)
	}
}

func migrateActions(t *testing.T, dir string, mems ...*schema.CanonicalMemory) map[MigrateActionKind][]MigrateAction {
	t.Helper()
	acts, err := ClaudeMigrateTarget{MemoryDir: dir, Desired: mems}.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	out := map[MigrateActionKind][]MigrateAction{}
	for _, a := range acts {
		out[a.Kind] = append(out[a.Kind], a)
	}
	return out
}

// --- EV-01: normalized-name adoption via slug-equality ------------------------

func TestMigrateEV01NormalizedNameAdoption(t *testing.T) {
	dir := t.TempDir()
	body := "the rust gotcha body\n"
	writeHand(t, dir, "rust_gotchas", "Rust gotchas", body)
	m := memBody("rust-gotchas", body) // no provenance: exercises the slug tier

	plan := migrateActions(t, dir, m)
	if len(plan[Adopt]) != 1 || plan[Adopt][0].Source != "rust_gotchas" || plan[Adopt][0].Name != "rust-gotchas" {
		t.Fatalf("want 1 adopt rust_gotchas->rust-gotchas, got %+v", plan)
	}

	res, err := ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Adopted) != 1 {
		t.Fatalf("adopted %d, want 1", len(res.Adopted))
	}
	if _, err := os.Stat(filepath.Join(dir, "rust_gotchas.md")); !os.IsNotExist(err) {
		t.Errorf("old rust_gotchas.md should be gone, stat err=%v", err)
	}
	assertEngramOwned(t, dir, "rust-gotchas")
	assertIndexHasMarked(t, dir, "rust-gotchas")
	assertIndexNoTarget(t, dir, "rust_gotchas.md")

	// EV-07 (per-memory): post-adopt sync is duplicate/conflict-free and idempotent.
	assertSyncClean(t, dir, m)
}

// --- EV-02: exact-name adoption in place --------------------------------------

func TestMigrateEV02ExactNameAdoption(t *testing.T) {
	dir := t.TempDir()
	body := "alpha body\n"
	writeHand(t, dir, "alpha", "alpha", body)
	m := memBody("alpha", body)

	res, err := ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Adopted) != 1 {
		t.Fatalf("adopted %d, want 1", len(res.Adopted))
	}
	assertEngramOwned(t, dir, "alpha")
	// Exactly one index line for alpha, and it is engram-marked (no leftover unmarked).
	if got := countIndexTarget(t, dir, "alpha.md"); got != 1 {
		t.Errorf("index lines for alpha.md = %d, want 1", got)
	}
	assertIndexHasMarked(t, dir, "alpha")
	assertSyncClean(t, dir, m)
}

// --- provenance tier independent of slug-equality -----------------------------

func TestMigrateProvenanceTier(t *testing.T) {
	dir := t.TempDir()
	body := "provenance body\n"
	// Native name slugifies to something unrelated to the canonical name; only the
	// recorded provenance source ("orig.md") can link them.
	writeHand(t, dir, "orig", "Totally Different Title", body)
	m := memProv("renamed-thing", body, "orig.md")

	plan := migrateActions(t, dir, m)
	if len(plan[Adopt]) != 1 || plan[Adopt][0].Name != "renamed-thing" {
		t.Fatalf("provenance match failed: %+v", plan)
	}
	res, _ := ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}.Apply()
	if len(res.Adopted) != 1 {
		t.Fatalf("adopted %d, want 1", len(res.Adopted))
	}
	assertEngramOwned(t, dir, "renamed-thing")
	if _, err := os.Stat(filepath.Join(dir, "orig.md")); !os.IsNotExist(err) {
		t.Errorf("orig.md should be gone after rename-adopt")
	}
}

// --- EV-03: diverged body is untouched ----------------------------------------

func TestMigrateEV03DivergedUntouched(t *testing.T) {
	dir := t.TempDir()
	writeHand(t, dir, "y", "y", "hand-authored body EDITED\n")
	m := memBody("y", "canonical body DIFFERENT\n")

	path := filepath.Join(dir, "y.md")
	before, _ := os.ReadFile(path)

	plan := migrateActions(t, dir, m)
	if len(plan[Diverged]) != 1 || len(plan[Adopt]) != 0 {
		t.Fatalf("want 1 diverged 0 adopt, got %+v", plan)
	}
	res, _ := ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}.Apply()
	if len(res.Adopted) != 0 || len(res.Diverged) != 1 {
		t.Fatalf("apply: adopted=%d diverged=%d, want 0/1", len(res.Adopted), len(res.Diverged))
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("diverged file was modified:\nbefore=%q\nafter=%q", before, after)
	}
}

// --- EV-04: ambiguous matches, both directions --------------------------------

func TestMigrateEV04AmbiguousTwoFilesOneMemory(t *testing.T) {
	dir := t.TempDir()
	body := "shared body\n"
	writeHand(t, dir, "a-b", "A B", body) // slugify -> a-b
	writeHand(t, dir, "a_b", "A_B", body) // slugify -> a-b
	m := memBody("a-b", body)

	plan := migrateActions(t, dir, m)
	if len(plan[Ambiguous]) != 2 || len(plan[Adopt]) != 0 {
		t.Fatalf("want 2 ambiguous 0 adopt, got %+v", plan)
	}
	// Neither file is touched on apply.
	if _, err := (ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, base := range []string{"a-b", "a_b"} {
		if _, err := os.Stat(filepath.Join(dir, base+".md")); err != nil {
			t.Errorf("%s.md should still exist: %v", base, err)
		}
	}
}

func TestMigrateEV04AmbiguousOneFileTwoMemories(t *testing.T) {
	dir := t.TempDir()
	body := "dup body\n"
	writeHand(t, dir, "dup", "shared", body) // slugify -> shared
	m1 := memBody("shared", body)            // matches by slug
	m2 := memProv("other", body, "dup.md")   // matches by provenance source

	plan := migrateActions(t, dir, m1, m2)
	if len(plan[Ambiguous]) != 1 || len(plan[Adopt]) != 0 {
		t.Fatalf("want 1 ambiguous 0 adopt, got %+v", plan)
	}
}

// --- destination-collision guard (P1): never clobber an unrelated file --------

func TestMigrateDestinationOccupiedIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	body := "shared body\n"
	// Source normalizes to "foo-bar"; a *different* hand-authored foo-bar.md
	// already occupies that destination name with unrelated content.
	writeHand(t, dir, "foo_bar", "Foo bar", body) // slugify -> foo-bar
	writeHand(t, dir, "foo-bar", "unrelated other lesson", "totally different body\n")
	m := memBody("foo-bar", body)

	plan := migrateActions(t, dir, m)
	if len(plan[Adopt]) != 0 {
		t.Fatalf("must not adopt when destination is occupied by a different file: %+v", plan)
	}
	// Apply must leave the unrelated foo-bar.md byte-for-byte intact.
	unrelated := filepath.Join(dir, "foo-bar.md")
	before, _ := os.ReadFile(unrelated)
	if _, err := (ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	after, _ := os.ReadFile(unrelated)
	if string(before) != string(after) {
		t.Errorf("unrelated foo-bar.md was clobbered:\nbefore=%q\nafter=%q", before, after)
	}
	// The source file is also left untouched (not deleted).
	if _, err := os.Stat(filepath.Join(dir, "foo_bar.md")); err != nil {
		t.Errorf("source foo_bar.md should remain: %v", err)
	}
}

// --- EV-05: no local original -> not a migrate candidate ----------------------

func TestMigrateEV05NoLocalOriginSkipped(t *testing.T) {
	dir := t.TempDir()
	writeHand(t, dir, "unrelated", "unrelated", "unrelated body\n")
	codexOnly := memBody("codex-only-lesson", "codex body\n") // no file for it

	plan := migrateActions(t, dir, codexOnly)
	// The canonical memory with no local file produces no migrate action at all.
	for _, k := range []MigrateActionKind{Adopt, Diverged, Ambiguous} {
		for _, a := range plan[k] {
			if a.Name == "codex-only-lesson" {
				t.Errorf("codex-only-lesson should not be a migrate candidate, got %s", k)
			}
		}
	}
	// The unrelated hand-authored file is a SKIP (nothing supersedes it).
	if len(plan[Skip]) != 1 || plan[Skip][0].Source != "unrelated" {
		t.Fatalf("want unrelated skipped, got %+v", plan[Skip])
	}
}

// --- EV-06: idempotency -------------------------------------------------------

func TestMigrateEV06Idempotent(t *testing.T) {
	dir := t.TempDir()
	body := "idem body\n"
	writeHand(t, dir, "thing_one", "Thing One", body)
	m := memBody("thing-one", body)
	tgt := ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}

	if res, _ := tgt.Apply(); len(res.Adopted) != 1 {
		t.Fatalf("first apply adopted %d, want 1", len(res.Adopted))
	}
	res2, err := tgt.Apply()
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(res2.Adopted) != 0 {
		t.Errorf("second apply adopted %d, want 0 (idempotent)", len(res2.Adopted))
	}
}

// --- EV-07: steady-state convergence on a mixed slug --------------------------

func TestMigrateEV07SteadyStateConvergence(t *testing.T) {
	dir := t.TempDir()
	nb := "normalized body\n"
	eb := "exact body\n"
	writeHand(t, dir, "norm_case", "Norm case", nb)   // normalized-name conflict
	writeHand(t, dir, "exact-case", "exact-case", eb) // exact-name conflict
	norm := memBody("norm-case", nb)
	exact := memBody("exact-case", eb)
	codexOnly := memBody("codex-only", "new knowledge\n") // no local original
	desired := []*schema.CanonicalMemory{norm, exact, codexOnly}

	// Before migrate: sync sees at least one CONFLICT (exact-name) and the
	// normalized case would render as a duplicate CREATE. Non-degenerate start.
	before := kinds(mustPlan(t, target(dir, desired...)))
	if before[Conflict] < 1 {
		t.Fatalf("fixture should start with >=1 CONFLICT, got %v", before)
	}

	if _, err := (ClaudeMigrateTarget{MemoryDir: dir, Desired: desired}).Apply(); err != nil {
		t.Fatalf("migrate Apply: %v", err)
	}

	after := mustPlan(t, target(dir, desired...))
	adopted := map[string]bool{"norm-case": true, "exact-case": true}
	for _, a := range after {
		if a.Kind == Conflict {
			t.Errorf("post-migrate CONFLICT remains: %s", a.Name)
		}
		if a.Kind == Create && adopted[a.Name] {
			t.Errorf("post-migrate duplicate CREATE for adopted %s", a.Name)
		}
	}
	// The genuinely-new memory still renders as a legitimate CREATE.
	sawCodexCreate := false
	for _, a := range after {
		if a.Kind == Create && a.Name == "codex-only" {
			sawCodexCreate = true
		}
	}
	if !sawCodexCreate {
		t.Errorf("codex-only should still be a legitimate CREATE post-migrate; actions=%v", after)
	}
}

// --- assertions ---------------------------------------------------------------

func mustPlan(t *testing.T, tgt ClaudeTarget) []Action {
	t.Helper()
	acts, err := tgt.Plan()
	if err != nil {
		t.Fatalf("sync Plan: %v", err)
	}
	return acts
}

func assertSyncClean(t *testing.T, dir string, mems ...*schema.CanonicalMemory) {
	t.Helper()
	for _, a := range mustPlan(t, target(dir, mems...)) {
		t.Errorf("expected clean sync after adopt, got %s %s", a.Kind, a.Name)
	}
}

func assertEngramOwned(t *testing.T, dir, name string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name+".md"))
	if err != nil {
		t.Fatalf("read %s.md: %v", name, err)
	}
	if !isEngramOwned(content) {
		t.Errorf("%s.md is not engram-owned after adopt", name)
	}
}

func assertIndexHasMarked(t *testing.T, dir, name string) {
	t.Helper()
	if _, ok := currentIndexLine(dir, name); !ok {
		t.Errorf("no engram-marked index line for %s", name)
	}
}

func assertIndexNoTarget(t *testing.T, dir, target string) {
	t.Helper()
	if countIndexTarget(t, dir, target) != 0 {
		t.Errorf("index still references %s", target)
	}
}

func countIndexTarget(t *testing.T, dir, target string) int {
	t.Helper()
	n := 0
	for _, ln := range readIndexLines(dir) {
		if strings.Contains(ln, "]("+target+")") {
			n++
		}
	}
	return n
}

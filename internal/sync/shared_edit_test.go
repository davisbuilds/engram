package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

// stampedTarget is a shared target over one memory, applied once.
func stampedTarget(t *testing.T, m *schema.CanonicalMemory) ClaudeSharedTarget {
	t.Helper()
	tg := sharedTarget(t, m)
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	return tg
}

// editBody rewrites the body of the render at path, as an agent editing the
// file in place would, leaving its frontmatter (and stamp) alone.
func editBody(t *testing.T, path, body string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s[4:], "\n---\n") + 4 + len("\n---\n")
	if err := os.WriteFile(path, []byte(s[:i]+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSharedRenderCarriesItsBase pins the stamp: a shared render records the
// NativeHash of what engram wrote, and stamping keeps apply idempotent.
func TestSharedRenderCarriesItsBase(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	got := readOr(t, filepath.Join(tg.Dir, "alpha.md"))
	if !strings.Contains(got, "engram_base: "+schema.NativeHash(m)) {
		t.Errorf("render lacks its base stamp:\n%s", got)
	}
	actions, err := tg.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Errorf("second plan = %v, want none", actions)
	}
}

// TestEditedSharedRenderIsHeld pins that sync never overwrites an edit: an
// edited render is a CONFLICT, left byte-for-byte, and reported by
// ScanSharedEdits with its base and edited content.
func TestEditedSharedRenderIsHeld(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	before := readOr(t, path)

	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(res.Conflicts, Conflict, "alpha") {
		t.Errorf("conflicts = %v, want alpha held", res.Conflicts)
	}
	if got := readOr(t, path); got != before {
		t.Errorf("edited render was rewritten:\n%s", got)
	}
	edits, err := ScanSharedEdits(tg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].Name != "alpha" || edits[0].Body != "edited in place\n" ||
		edits[0].Base != schema.NativeHash(m) || edits[0].Description != m.Description || edits[0].Type != m.Type {
		t.Fatalf("edits = %+v, want alpha's edit with its base", edits)
	}
	want := *m
	want.Body = "edited in place\n"
	if edits[0].Hash() != schema.NativeHash(&want) {
		t.Errorf("edit hash = %s, want the NativeHash of the edited content", edits[0].Hash())
	}
}

// TestEditMatchingCanonicalIsRestamped pins the agreement case: once canonical
// holds the edited content (imported), the render is restamped, not held.
func TestEditMatchingCanonicalIsRestamped(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	m2 := *m
	m2.Body = "edited in place\n"
	tg.Desired = []*schema.CanonicalMemory{&m2}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 || !hasAction(res.Applied, Update, "alpha") {
		t.Errorf("result = %+v, want alpha restamped (UPDATE), no conflict", res)
	}
	if edits, _ := ScanSharedEdits(tg.Dir); len(edits) != 0 {
		t.Errorf("render still reads as edited after restamp: %+v", edits)
	}
}

// TestDiscardOverwritesAnEdit pins the keep-canonical resolution.
func TestDiscardOverwritesAnEdit(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	tg.Discard = map[string]bool{"alpha": true}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := readOr(t, path); !strings.HasSuffix(got, m.Body) {
		t.Errorf("discarded edit survived:\n%s", got)
	}
}

// TestEditedRenderOfUnsharedMemoryIsHeld pins that STALE removal never deletes
// an edit either; Discard removes it.
func TestEditedRenderOfUnsharedMemoryIsHeld(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	tg.Desired = []*schema.CanonicalMemory{mem("beta")}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(res.Conflicts, Conflict, "alpha") || readOr(t, path) == "" {
		t.Errorf("edited render of a no-longer-shared memory was not held: %+v", res)
	}
	tg.Discard = map[string]bool{"alpha": true}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("discarded stale edit survived (err=%v)", err)
	}
}

// TestKnownSettlesAnEditCanonicalHolds pins Known: an edited render of a memory
// that no longer renders here is removed once canonical says what the edit says
// (the edit lives on in canonical), and still held while canonical differs.
func TestKnownSettlesAnEditCanonicalHolds(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	tg.Desired = []*schema.CanonicalMemory{mem("beta")}

	moved := mem("alpha")
	moved.Body = "a different canonical body\n"
	tg.Known = map[string]*schema.CanonicalMemory{"alpha": moved}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(res.Conflicts, Conflict, "alpha") || readOr(t, path) == "" {
		t.Fatalf("an edit canonical does not hold was not kept: %+v", res)
	}

	took := mem("alpha")
	took.Body = "edited in place\n"
	tg.Known = map[string]*schema.CanonicalMemory{"alpha": took}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an edit canonical already holds was kept (err=%v)", err)
	}
}

// TestUnstampedRenderIsNotAnEdit pins backward compatibility: a render written
// before stamping has no base, so it is updated as before, never held.
func TestUnstampedRenderIsNotAnEdit(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"))
	writeOwned(t, tg.Dir, mem("alpha"))
	editBody(t, filepath.Join(tg.Dir, "alpha.md"), "older text\n")
	if edits, _ := ScanSharedEdits(tg.Dir); len(edits) != 0 {
		t.Errorf("unstamped render reported as edited: %+v", edits)
	}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 || !hasAction(res.Applied, Update, "alpha") {
		t.Errorf("result = %+v, want an UPDATE that stamps it", res)
	}
}

// TestDiscardBypassesTheStaleHold pins an explicit --keep under a global hold:
// removals are held while canonical may be incomplete, but a render the
// operator named for discarding goes anyway.
func TestDiscardBypassesTheStaleHold(t *testing.T) {
	m := mem("alpha")
	tg := stampedTarget(t, m)
	path := filepath.Join(tg.Dir, "alpha.md")
	editBody(t, path, "edited in place\n")
	tg.Desired, tg.KeepStale, tg.Discard = nil, true, map[string]bool{"alpha": true}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a discarded edit survived the stale hold (err=%v)", err)
	}
}

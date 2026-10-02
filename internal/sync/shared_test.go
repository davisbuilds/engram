package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
)

func sharedTarget(t *testing.T, mems ...*schema.CanonicalMemory) ClaudeSharedTarget {
	t.Helper()
	home := t.TempDir()
	return ClaudeSharedTarget{
		Dir:       filepath.Join(home, "engram", "memory"),
		RulesFile: filepath.Join(home, "rules", "engram-memory.md"),
		Desired:   mems,
	}
}

func readOr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// TestSharedApplyRendersOnceWithRules pins the shared target: each memory renders
// once into the shared dir with one index line, the rules file imports that
// index by absolute path, and a second apply is a no-op.
func TestSharedApplyRendersOnceWithRules(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"), mem("beta"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(tg.Dir, n+".md")); err != nil {
			t.Errorf("%s not rendered into the shared dir: %v", n, err)
		}
		if idx := readOr(t, filepath.Join(tg.Dir, "MEMORY.md")); !containsLineFor(idx, n) {
			t.Errorf("shared index lacks %s:\n%s", n, idx)
		}
	}
	rules := readOr(t, tg.RulesFile)
	if !marker.IsSharedRules([]byte(rules)) {
		t.Errorf("rules file is not marked as engram's:\n%s", rules)
	}
	if !strings.Contains(rules, "\n@"+filepath.Join(tg.Dir, "MEMORY.md")+"\n") {
		t.Errorf("rules file does not import the shared index by absolute path:\n%s", rules)
	}
	actions, err := tg.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Errorf("second plan = %v, want no actions", actions)
	}
}

// TestSharedRemovesWhenEmpty pins cleanup: once nothing is shared, the renders,
// the index and the rules file all go.
func TestSharedRemovesWhenEmpty(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	tg.Desired = nil
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(tg.Dir, "alpha.md"), filepath.Join(tg.Dir, "MEMORY.md"), tg.RulesFile} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed once nothing is shared (err=%v)", p, err)
		}
	}
}

// TestSharedKeepStaleKeepsEverything pins the hold: when canonical may be
// incomplete, an empty desired set removes nothing, the rules file included.
func TestSharedKeepStaleKeepsEverything(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	tg.Desired, tg.KeepStale = nil, true
	actions, err := tg.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Errorf("held plan = %v, want no actions", actions)
	}
}

// TestSharedRulesConflictLeavesHandAuthoredFile pins marker discipline for the
// rules file: an unmarked file at that path is a CONFLICT, left byte-for-byte,
// while the shared memories still render.
func TestSharedRulesConflictLeavesHandAuthoredFile(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"))
	hand := "# my own rules\n"
	if err := os.MkdirAll(filepath.Dir(tg.RulesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tg.RulesFile, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Name != RulesActionName {
		t.Errorf("conflicts = %v, want one for the rules file", res.Conflicts)
	}
	if got := readOr(t, tg.RulesFile); got != hand {
		t.Errorf("hand-authored rules file was modified:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "alpha.md")); err != nil {
		t.Errorf("shared memory not rendered despite the rules conflict: %v", err)
	}
}

// TestSharedRulesUpdateWhenDrifted pins that an engram-owned rules file with
// outdated content (here, pointing at another dir) is rewritten.
func TestSharedRulesUpdateWhenDrifted(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"))
	if err := os.MkdirAll(filepath.Dir(tg.RulesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tg.RulesFile, SharedRulesContent("/elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	actions, err := tg.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(actions, Update, RulesActionName) {
		t.Errorf("plan = %v, want UPDATE of the rules file", actions)
	}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := readOr(t, tg.RulesFile); got != string(SharedRulesContent(tg.Dir)) {
		t.Errorf("rules file not rewritten:\n%s", got)
	}
}

// TestSharedRulesContentEscapesSpaces pins Claude Code's import syntax: a space
// in the path is backslash-escaped, or the import would stop at it.
func TestSharedRulesContentEscapesSpaces(t *testing.T) {
	got := string(SharedRulesContent("/a b/engram/memory"))
	if !strings.Contains(got, "\n@/a\\ b/engram/memory/MEMORY.md\n") {
		t.Errorf("import line does not escape the space:\n%s", got)
	}
}

// TestPurgeSharedDir pins forget's reach: a forgotten memory's render and index
// line are removed from the shared dir too.
func TestPurgeSharedDir(t *testing.T) {
	tg := sharedTarget(t, mem("alpha"), mem("beta"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	p := Purge{SharedDir: tg.Dir, Names: []string{"alpha"}}
	actions, err := p.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(actions, Stale, "alpha") {
		t.Errorf("purge plan = %v, want STALE alpha in the shared dir", actions)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "alpha.md")); !os.IsNotExist(err) {
		t.Errorf("alpha's shared render survived the purge (err=%v)", err)
	}
	if idx := readOr(t, filepath.Join(tg.Dir, "MEMORY.md")); containsLineFor(idx, "alpha") || !containsLineFor(idx, "beta") {
		t.Errorf("shared index after purge:\n%s", idx)
	}
}

func hasAction(as []Action, k ActionKind, name string) bool {
	for _, a := range as {
		if a.Kind == k && a.Name == name {
			return true
		}
	}
	return false
}

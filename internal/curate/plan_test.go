package curate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/lock"
)

func fingerprint(t *testing.T, root string) Binding {
	t.Helper()
	b, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The binding covers what the batch and its rollback cover: a memory read
// through a symlink drifts when its target's content changes, a tombstone
// counts, and Git's metadata, the lock, write temporaries and a mode change do
// not. A missing root is an empty store, as discovery treats it.
func TestFingerprintBindsTheStoreFiles(t *testing.T) {
	if b := fingerprint(t, filepath.Join(t.TempDir(), "missing")); len(b.Files) != 0 {
		t.Errorf("missing root = %v, want an empty binding", b.Files)
	}
	root := t.TempDir()
	seed(t, root, corpus("a")...)
	target := filepath.Join(t.TempDir(), "l.md")
	writeTestFile(t, target, "---\nname: l\ndescription: d\ntype: lesson\nscope: global\n---\nb\n")
	if err := os.Symlink(target, filepath.Join(root, "l.md")); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, root)
	if got := strings.Join(keys(before.Files), ","); got != "a.md,l.md" {
		t.Fatalf("bound files = %s, want a.md,l.md", got)
	}
	for _, p := range []string{".git/HEAD", lock.Name, ".engram-1.tmp"} {
		writeTestFile(t, filepath.Join(root, p), "runtime\n")
	}
	if err := os.Chmod(filepath.Join(root, "a.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := before.Drift(fingerprint(t, root)); len(d) != 0 {
		t.Errorf("drift = %v, want none for Git, runtime files and a mode change", d)
	}
	writeTestFile(t, target, "---\nname: l\ndescription: d\ntype: lesson\nscope: global\n---\nedited behind the link\n")
	writeTestFile(t, filepath.Join(root, ".forgotten", "x.yaml"), "name: x\n")
	if d := strings.Join(before.Drift(fingerprint(t, root)), ","); d != ".forgotten/x.yaml,l.md" {
		t.Errorf("drift = %s, want .forgotten/x.yaml,l.md", d)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func testPlan(t *testing.T, root string, ops ...Operation) *Plan {
	t.Helper()
	p, err := NewPlan(root, fingerprint(t, root), &Proposer{Harness: "claude-code"}, ops)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A plan survives its own file format exactly, bodies unescaped, and the
// parser refuses what it cannot read unambiguously.
func TestParsePlanIsStrict(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a")...)
	m := edited("a", "keep <!-- this --> & that\n")
	p := testPlan(t, root, Operation{Op: OpUpdate, Name: "a", Memory: m, Reason: "r"})
	raw, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("<!-- this --> & that")) {
		t.Errorf("the body is escaped in the plan file:\n%s", raw)
	}
	got, err := ParsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Errorf("round trip = %+v, want %+v", got, p)
	}

	digest := p.Corpus.Files["a.md"]
	for name, bad := range map[string]string{
		"case-variant key":   strings.Replace(string(raw), `"version": 1,`, `"version": 1, "Version": 1,`, 1),
		"unclean path":       strings.Replace(string(raw), `"a.md"`, `"x/../a.md"`, 1),
		"escaping path":      strings.Replace(string(raw), `"a.md"`, `"../a.md"`, 1),
		"malformed digest":   strings.Replace(string(raw), digest, "sha256:abc", 1),
		"relative root":      strings.Replace(string(raw), `"canonical_root": "`+p.CanonicalRoot, `"canonical_root": "canonical`, 1),
		"second plan object": string(raw) + string(raw),
	} {
		if _, err := ParsePlan([]byte(bad)); err == nil {
			t.Errorf("%s: parsed, want refused", name)
		}
	}
	// Paths are compared exactly: two files differing only in case are two files.
	twoFiles := strings.Replace(string(raw), `"a.md": "`+digest+`"`, `"a.md": "`+digest+`", "A.md": "`+digest+`"`, 1)
	if _, err := ParsePlan([]byte(twoFiles)); err != nil {
		t.Errorf("case-distinct corpus paths: %v", err)
	}
}

// ApplyPlan checks the binding after it takes the lock and before it writes:
// a store changed since the plan was made, or another store, gets nothing
// written; a held lock is refused before the check.
func TestApplyPlanChecksTheBindingUnderTheLock(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a", "b")...)
	p := testPlan(t, root, Operation{Op: OpRemove, Name: "a", Reason: "old"})

	release, err := lock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(root, p); err == nil {
		t.Error("ApplyPlan ran while another apply held the lock")
	}
	release()

	other := t.TempDir()
	seed(t, other, corpus("a", "b")...)
	if _, err := ApplyPlan(other, p); !errors.Is(err, ErrRootMismatch) {
		t.Errorf("another root: err = %v, want ErrRootMismatch", err)
	}

	writeTestFile(t, filepath.Join(root, "b.md"), "---\nname: b\ndescription: d\ntype: lesson\nscope: global\n---\nb\n\n")
	before := tree(t, root)
	_, err = ApplyPlan(root, p)
	var stale *StaleError
	if !errors.As(err, &stale) || strings.Join(stale.Drift, ",") != "b.md" {
		t.Fatalf("err = %v, want a StaleError naming b.md", err)
	}
	sameTree(t, before, tree(t, root))
	if _, err := os.Stat(filepath.Join(other, "a.md")); err != nil {
		t.Errorf("the other store was written: %v", err)
	}
}

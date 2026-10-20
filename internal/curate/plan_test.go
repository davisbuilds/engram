package curate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/discover"
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

// A symlink beneath the root that points at a directory is bound by its target
// alone. Discovery does not descend into it and rollback restores the link, not
// what it points at, so the directory's files are neither bound nor corpus:
// changing them is no drift. Retargeting or removing the link is, and so is the
// target turning into a file, whose content discovery could then read.
func TestFingerprintBindsADirectoryLinkByItsTarget(t *testing.T) {
	root, shelf := dirLinkStore(t, "a")
	before := fingerprint(t, root)
	if got := strings.Join(keys(before.Files), ","); got != "a.md,shelf" {
		t.Fatalf("bound files = %s, want a.md,shelf", got)
	}
	mems, perrs, err := discover.Discover(root)
	if err != nil || len(perrs) != 0 || len(mems) != 1 || mems[0].Name != "a" {
		t.Fatalf("Discover = %v, %v, %v; want only a", mems, perrs, err)
	}

	writeTestFile(t, filepath.Join(shelf, "hidden.md"), "---\nname: hidden\ndescription: d\ntype: lesson\nscope: global\n---\nedited\n")
	writeTestFile(t, filepath.Join(shelf, "more.md"), "added\n")
	if d := before.Drift(fingerprint(t, root)); len(d) != 0 {
		t.Errorf("drift = %v, want none for files behind a directory link", d)
	}
	if d := before.Drift(fingerprint(t, linkedRootTo(t, root))); len(d) != 0 {
		t.Errorf("a root link binds a directory link beneath it differently: drift %v", d)
	}

	for name, change := range map[string]func(t *testing.T, root, shelf string){
		"retargeted": func(t *testing.T, root, _ string) {
			link := filepath.Join(root, "shelf")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
		},
		"removed": func(t *testing.T, root, _ string) {
			if err := os.Remove(filepath.Join(root, "shelf")); err != nil {
				t.Fatal(err)
			}
		},
		"target now a file": func(t *testing.T, _, shelf string) {
			if err := os.RemoveAll(shelf); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, shelf, "a file now\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, shelf := dirLinkStore(t, "a")
			before := fingerprint(t, root)
			change(t, root, shelf)
			if d := strings.Join(before.Drift(fingerprint(t, root)), ","); d != "shelf" {
				t.Errorf("drift = %s, want shelf", d)
			}
		})
	}
}

// dirLinkStore returns a store holding names and shelf, a symlink to a
// directory outside it that holds a valid memory, hidden, and that directory.
func dirLinkStore(t *testing.T, names ...string) (root, shelf string) {
	t.Helper()
	root = t.TempDir()
	seed(t, root, corpus(names...)...)
	shelf = t.TempDir()
	writeTestFile(t, filepath.Join(shelf, "hidden.md"), "---\nname: hidden\ndescription: d\ntype: lesson\nscope: global\n---\nb\n")
	if err := os.Symlink(shelf, filepath.Join(root, "shelf")); err != nil {
		t.Fatal(err)
	}
	return root, shelf
}

// linkedRootTo returns a symlink to the directory real.
func linkedRootTo(t *testing.T, real string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "canonical")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// linkedRoot returns a canonical root that is a symlink to a store holding
// names, and the store's real path.
func linkedRoot(t *testing.T, names ...string) (link, real string) {
	t.Helper()
	dir := t.TempDir()
	real = filepath.Join(dir, "store")
	seed(t, real, corpus(names...)...)
	link = filepath.Join(dir, "canonical")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	return link, real
}

// A root that is a symlink to a directory binds the directory's files, the
// same binding its real path gives, with a memory linked beneath it still bound
// as a link. A dangling root link is a missing root: an empty store.
func TestFingerprintFollowsALinkedRoot(t *testing.T) {
	link, real := linkedRoot(t, "a")
	target := filepath.Join(t.TempDir(), "l.md")
	writeTestFile(t, target, "---\nname: l\ndescription: d\ntype: lesson\nscope: global\n---\nb\n")
	if err := os.Symlink(target, filepath.Join(real, "l.md")); err != nil {
		t.Fatal(err)
	}
	viaLink := fingerprint(t, link)
	if got := strings.Join(keys(viaLink.Files), ","); got != "a.md,l.md" {
		t.Fatalf("bound files = %s, want a.md,l.md", got)
	}
	if d := viaLink.Drift(fingerprint(t, real)); len(d) != 0 {
		t.Errorf("the link and the real path bind differently: drift %v", d)
	}
	writeTestFile(t, target, "---\nname: l\ndescription: d\ntype: lesson\nscope: global\n---\nedited behind the link\n")
	if d := strings.Join(viaLink.Drift(fingerprint(t, link)), ","); d != "l.md" {
		t.Errorf("drift = %s, want l.md", d)
	}

	dangling := filepath.Join(t.TempDir(), "canonical")
	if err := os.Symlink(filepath.Join(filepath.Dir(dangling), "gone"), dangling); err != nil {
		t.Fatal(err)
	}
	if b := fingerprint(t, dangling); len(b.Files) != 0 {
		t.Errorf("dangling root = %v, want an empty binding", b.Files)
	}
}

// A plan binds the store, not the spelling of its root: one made through a
// root link applies through the real path and the other way round, and a change
// made through either spelling makes it stale.
func TestApplyPlanThroughALinkedRoot(t *testing.T) {
	for _, tc := range []struct{ name, made, applied string }{
		{"made through the link", "link", "real"},
		{"applied through the link", "real", "link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link, real := linkedRoot(t, "a", "b")
			at := map[string]string{"link": link, "real": real}
			p := testPlan(t, at[tc.made], Operation{Op: OpRemove, Name: "a", Reason: "old"})
			if _, err := ApplyPlan(at[tc.applied], p); err != nil {
				t.Fatalf("ApplyPlan: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(real, "a.md")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a.md was not removed: %v", err)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the root link was replaced: %v", err)
			}
		})
	}

	link, real := linkedRoot(t, "a", "b")
	p := testPlan(t, link, Operation{Op: OpRemove, Name: "a", Reason: "old"})
	writeTestFile(t, filepath.Join(real, "b.md"), "---\nname: b\ndescription: d\ntype: lesson\nscope: global\n---\nedited\n")
	before := tree(t, real)
	_, err := ApplyPlan(link, p)
	var stale *StaleError
	if !errors.As(err, &stale) || strings.Join(stale.Drift, ",") != "b.md" {
		t.Fatalf("err = %v, want a StaleError naming b.md", err)
	}
	sameTree(t, before, tree(t, real))
}

// A root spelled relative to the working directory names the same store as its
// absolute and linked spellings: a plan made through any of them applies through
// any other. Another store is refused however either is spelled, and a missing
// root matches only its own path.
func TestApplyPlanThroughARelativeRoot(t *testing.T) {
	for _, tc := range []struct{ made, applied string }{
		{"canonical", "store"},
		{"store", "canonical"},
		{"link", "store"},
		{"store", "link"},
		{"canonical", "real"},
		{"real", "canonical"},
	} {
		t.Run(tc.made+" then "+tc.applied, func(t *testing.T) {
			link, real := linkedRoot(t, "a", "b")
			t.Chdir(filepath.Dir(real))
			at := map[string]string{"link": link, "real": real, "canonical": "canonical", "store": "store"}
			p := testPlan(t, at[tc.made], Operation{Op: OpRemove, Name: "a", Reason: "old"})
			if _, err := ApplyPlan(at[tc.applied], p); err != nil {
				t.Fatalf("ApplyPlan: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(real, "a.md")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a.md was not removed: %v", err)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the root link was replaced: %v", err)
			}
		})
	}

	t.Run("another store", func(t *testing.T) {
		_, real := linkedRoot(t, "a", "b")
		dir := filepath.Dir(real)
		seed(t, filepath.Join(dir, "other"), corpus("a", "b")...)
		t.Chdir(dir)
		for _, made := range []string{"canonical", "store"} {
			p := testPlan(t, made, Operation{Op: OpRemove, Name: "a", Reason: "old"})
			for _, other := range []string{"other", filepath.Join(dir, "other")} {
				before := tree(t, filepath.Join(dir, "other"))
				if _, err := ApplyPlan(other, p); !errors.Is(err, ErrRootMismatch) {
					t.Errorf("made through %s, applied to %s: err = %v, want ErrRootMismatch", made, other, err)
				}
				sameTree(t, before, tree(t, filepath.Join(dir, "other")))
			}
		}
	})

	t.Run("missing root", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		p := testPlan(t, "absent")
		for _, root := range []string{"absent", filepath.Join(dir, "absent")} {
			if err := p.Check(root); err != nil {
				t.Errorf("Check(%s) = %v, want nil", root, err)
			}
		}
		if err := p.Check("elsewhere"); !errors.Is(err, ErrRootMismatch) {
			t.Errorf("another missing root: err = %v, want ErrRootMismatch", err)
		}
	})
}

// A store whose .forgotten links elsewhere cannot be bound: the link alone
// would be bound while forget writes the history behind it, so a change there
// would leave a plan current. A plan bound to the link alone (as one saved
// before this refusal was) is refused by Check and, under the lock, by
// ApplyPlan, even after the history behind the link changed, and nothing is
// written on either side of the link.
func TestPlanRefusesLinkedHistory(t *testing.T) {
	root, hist := historyLinkStore(t, "a", "b")
	if _, err := Fingerprint(root); !errors.Is(err, ErrLinkedHistory) {
		t.Errorf("Fingerprint: err = %v, want ErrLinkedHistory", err)
	}
	if _, err := Fingerprint(linkedRootTo(t, root)); !errors.Is(err, ErrLinkedHistory) {
		t.Errorf("Fingerprint through a root link: err = %v, want ErrLinkedHistory", err)
	}

	sum := sha256.Sum256([]byte("dirlink\x00" + hist))
	binding := Binding{Files: map[string]string{".forgotten": "sha256:" + hex.EncodeToString(sum[:])}}
	for _, name := range []string{"a.md", "b.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(append([]byte("file\x00"), data...))
		binding.Files[name] = "sha256:" + hex.EncodeToString(sum[:])
	}
	p, err := NewPlan(root, binding, nil, []Operation{{Op: OpRemove, Name: "b", Reason: "old"}})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(hist, "b.yaml"), "name: b\nforgotten_at: \"2026-01-01T00:00:00Z\"\npath: b.md\nmemory: edited behind the link\n")
	before, behind := tree(t, root), tree(t, hist)
	if err := p.Check(root); !errors.Is(err, ErrLinkedHistory) {
		t.Errorf("Check: err = %v, want ErrLinkedHistory", err)
	}
	if _, err := ApplyPlan(root, p); !errors.Is(err, ErrLinkedHistory) {
		t.Errorf("ApplyPlan: err = %v, want ErrLinkedHistory", err)
	}
	sameTree(t, before, tree(t, root))
	sameTree(t, behind, tree(t, hist))
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

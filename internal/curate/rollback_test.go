package curate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

// tree returns every regular file under root but the apply lock (which Apply
// creates) as path -> mode and content.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || d.Name() == lock.Name {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = info.Mode().Perm().String() + " " + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	for p, v := range before {
		if got, ok := after[p]; !ok {
			t.Errorf("%s was not restored", p)
		} else if got != v {
			t.Errorf("%s = %q, want %q", p, got, v)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("%s was left behind", p)
		}
	}
}

func edited(name, body string) *schema.CanonicalMemory {
	m := mem(name)
	m.Body = body
	return m
}

// A write that fails midway rolls the whole batch back: the update already made
// is undone, the merged memory already written is removed, and each file keeps
// its mode.
func TestApplyRollsBackAFailedBatch(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a", "b", "c")...)
	if err := os.Chmod(filepath.Join(root, "a.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file where the tombstone directory belongs makes the merge's forget fail
	// after the merged memory is written.
	if err := os.WriteFile(filepath.Join(root, ".forgotten"), []byte("in the way\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tree(t, root)
	ops := []Operation{
		{Op: OpUpdate, Name: "a", Memory: edited("a", "new\n")},
		{Op: OpMerge, Sources: []string{"b", "c"}, Memory: mem("bc")},
	}
	applied, err := Apply(root, ops)
	if err == nil {
		t.Fatal("Apply succeeded; want the forget to fail")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("err = %v, want it to say the batch was rolled back", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %+v, want nothing after a rollback", applied)
	}
	sameTree(t, before, tree(t, root))
}

// Save reports a path occupied by something discovery did not index as a
// conflict, not an error. Apply must treat it as a failure: the add did not
// happen, and the remove before it, tombstone included, is undone.
func TestApplyFailsAnAddBlockedOnDisk(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a", "b")...)
	if err := os.Mkdir(filepath.Join(root, "x.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := tree(t, root)
	ops := []Operation{
		{Op: OpRemove, Name: "b", Reason: "old"},
		{Op: OpAdd, Memory: mem("x")},
	}
	if _, err := Apply(root, ops); err == nil {
		t.Fatal("Apply succeeded although x.md is a directory")
	}
	sameTree(t, before, tree(t, root))
	if _, _, found, _ := store.Load(root, "b"); !found {
		t.Error("the removed memory was not restored")
	}
}

// When the rollback itself cannot finish, Apply says so, names what it could
// not restore, and returns the operations that did apply.
func TestApplyReportsARollbackThatFails(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a", "b")...)
	defer func(orig func(string, Operation) (Applied, error)) { applyStep = orig }(applyStep)
	applyStep = func(root string, op Operation) (Applied, error) {
		if op.Op == OpUpdate {
			return applyOne(root, op)
		}
		// Leave a.md where no file can be written back.
		if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
			return Applied{}, err
		}
		if err := os.MkdirAll(filepath.Join(root, "a.md", "blocker"), 0o755); err != nil {
			return Applied{}, err
		}
		return Applied{}, errors.New("disk full")
	}
	ops := []Operation{
		{Op: OpUpdate, Name: "a", Memory: edited("a", "new\n")},
		{Op: OpRemove, Name: "b", Reason: "old"},
	}
	applied, err := Apply(root, ops)
	var partial *PartialApplyError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a PartialApplyError", err)
	}
	if !strings.Contains(partial.Rollback.Error(), "a.md") {
		t.Errorf("rollback error = %v, want it to name a.md", partial.Rollback)
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want the original failure", err)
	}
	if len(applied) != 1 || applied[0].Name != "a" {
		t.Errorf("applied = %+v, want the update that stayed applied", applied)
	}
}

// The snapshot covers canonical's own files, not Git's or the lock's: a restore
// leaves them as they are.
func TestRestoreLeavesGitAndRuntimeFilesAlone(t *testing.T) {
	root := t.TempDir()
	seed(t, root, corpus("a")...)
	for _, p := range []string{".git/HEAD", ".engram.lock", ".engram-1.tmp"} {
		writeTestFile(t, filepath.Join(root, p), "before\n")
	}
	snap, err := takeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".git/HEAD", ".engram.lock", ".engram-1.tmp"} {
		writeTestFile(t, filepath.Join(root, p), "after\n")
	}
	writeTestFile(t, filepath.Join(root, ".git", "ORIG_HEAD"), "new\n")
	writeTestFile(t, filepath.Join(root, "a.md"), "changed\n")
	if err := snap.restore(root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".git/HEAD", ".engram.lock", ".engram-1.tmp", ".git/ORIG_HEAD"} {
		if b, _ := os.ReadFile(filepath.Join(root, p)); string(b) == "before\n" || len(b) == 0 {
			t.Errorf("restore touched %s (now %q)", p, b)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.md")); string(b) == "changed\n" {
		t.Error("restore did not put a.md back")
	}
}

func writeTestFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

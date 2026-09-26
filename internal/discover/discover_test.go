package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMem(t *testing.T, path, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverParsesAllRecursively(t *testing.T) {
	root := t.TempDir()
	writeMem(t, filepath.Join(root, "a.md"), "a-mem")
	writeMem(t, filepath.Join(root, "sub", "b.md"), "b-mem")
	// a non-markdown file must be ignored
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mems, perrs, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(perrs) != 0 {
		t.Errorf("unexpected parse errors: %v", perrs)
	}
	if len(mems) != 2 {
		t.Fatalf("got %d memories, want 2", len(mems))
	}
}

func TestDiscoverCollectsParseErrorsWithoutAborting(t *testing.T) {
	root := t.TempDir()
	writeMem(t, filepath.Join(root, "good.md"), "good-mem")
	// a git-conflicted file: parse must fail for this one only
	bad := "---\nname: x\ndescription: y\ntype: lesson\nscope: global\n---\n<<<<<<< HEAD\na\n=======\nb\n>>>>>>> z\n"
	if err := os.WriteFile(filepath.Join(root, "bad.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	mems, perrs, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("got %d good memories, want 1", len(mems))
	}
	if len(perrs) != 1 {
		t.Errorf("got %d parse errors, want 1", len(perrs))
	}
}

func TestDiscoverMissingRootIsEmpty(t *testing.T) {
	mems, perrs, err := Discover(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if len(mems) != 0 || len(perrs) != 0 {
		t.Errorf("missing root should be empty; got %d mems, %d errs", len(mems), len(perrs))
	}
}

// A file that parses but violates the schema (here an empty project repo, which
// scope matching would read as "every cwd") is reported, never used.
func TestDiscoverRejectsSchemaInvalidMemory(t *testing.T) {
	root := t.TempDir()
	writeMem(t, filepath.Join(root, "ok.md"), "ok-mem")
	bad := "---\nname: bad-mem\ndescription: d\ntype: lesson\nscope: \"project:\"\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(root, "bad.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	mems, perrs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 || mems[0].Name != "ok-mem" {
		t.Errorf("memories = %v, want only ok-mem", mems)
	}
	if len(perrs) != 1 || filepath.Base(perrs[0].Path) != "bad.md" {
		t.Errorf("parse errors = %v, want one for bad.md", perrs)
	}
}

// Two files claiming one name would let remove or share act on one copy and
// orphan the other; every copy is withheld and reported instead.
func TestDiscoverWithholdsEveryCopyOfADuplicateName(t *testing.T) {
	root := t.TempDir()
	writeMem(t, filepath.Join(root, "a.md"), "dup-mem")
	writeMem(t, filepath.Join(root, "sub", "b.md"), "dup-mem")
	writeMem(t, filepath.Join(root, "c.md"), "c-mem")
	mems, perrs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 || mems[0].Name != "c-mem" {
		t.Errorf("memories = %v, want only c-mem", mems)
	}
	if len(perrs) != 2 {
		t.Errorf("parse errors = %v, want one per dup-mem copy", perrs)
	}
}

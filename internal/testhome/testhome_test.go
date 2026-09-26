package testhome

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

// TestEveryTestPackageIsSandboxed keeps new test packages from reaching the real
// home: each directory holding a _test.go file must run under Main.
func TestEveryTestPackageIsSandboxed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	missing, err := unsandboxed(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("test packages without `func TestMain(m *testing.M) { testhome.Main(m) }`: %v", missing)
	}
}

// TestUnsandboxedFindsAMissingPackage is the known-positive control for the
// guard above: a package with tests but no Main must be reported.
func TestUnsandboxedFindsAMissingPackage(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("guarded/a_test.go", "package guarded\n")
	write("guarded/main_test.go", "package guarded\nfunc TestMain(m *testing.M) { testhome.Main(m) }\n")
	write("bare/b_test.go", "package bare\n")
	write("notests/c.go", "package notests\n")

	got, err := unsandboxed(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bare"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unsandboxed = %v, want %v", got, want)
	}
}

func TestLeaksListsEveryFileSorted(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"home/.codex/n.md", "home/a", "xdg/engram/c.yaml"} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Leaks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"home/.codex/n.md", "home/a", "xdg/engram/c.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Leaks = %v, want %v", got, want)
	}
}

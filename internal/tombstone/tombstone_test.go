package tombstone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

var at = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func saveMem(t *testing.T, root, name, origin string) []byte {
	t.Helper()
	m := &schema.CanonicalMemory{
		Name: name, Description: "d", Type: schema.TypeLesson, Scope: "global", Body: "body of " + name + "\n",
		Provenance: schema.Provenance{Origin: origin, Source: name + ".md"},
	}
	if _, _, err := store.Save(root, m, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, name+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestForgetRemovesTheMemoryAndRecordsIt(t *testing.T) {
	root := t.TempDir()
	orig := saveMem(t, root, "old-lesson", "import:codex")

	ts, err := Forget(root, "old-lesson", Note{Reason: "superseded", Successor: "new-lesson"}, at)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old-lesson.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("canonical file should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(Path(root, "old-lesson")); err != nil {
		t.Errorf("tombstone not written at %s: %v", Path(root, "old-lesson"), err)
	}
	set, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := set["old-lesson"]
	if !ok {
		t.Fatalf("Load did not return the tombstone: %v", set)
	}
	want := Tombstone{
		Name: "old-lesson", Origin: "import:codex", Source: "old-lesson.md",
		ForgottenAt: "2026-09-27T12:00:00Z", Reason: "superseded", Successor: "new-lesson",
		Path: "old-lesson.md", Memory: string(orig),
	}
	if got != want {
		t.Errorf("tombstone = %+v\nwant %+v", got, want)
	}
	if ts != want {
		t.Errorf("Forget returned %+v, want %+v", ts, want)
	}
}

func TestForgetUnknownNameWritesNothing(t *testing.T) {
	root := t.TempDir()
	saveMem(t, root, "kept", "remember")
	if _, err := Forget(root, "missing", Note{}, at); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("err = %v, want ErrNoMemory", err)
	}
	if _, err := os.Stat(Path(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no tombstone may be written for an unknown name")
	}
}

// A tombstone holds a whole memory file, but discovery must never read it (or
// anything else under the tombstone dir) as canonical.
func TestTombstonesAreNeverDiscovered(t *testing.T) {
	root := t.TempDir()
	saveMem(t, root, "gone", "remember")
	if _, err := Forget(root, "gone", Note{}, at); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(filepath.Dir(Path(root, "gone")), "stray.md")
	if err := os.WriteFile(stray, []byte("---\nname: stray\ndescription: d\ntype: lesson\nscope: global\n---\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mems, perrs, err := discover.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 0 || len(perrs) != 0 {
		t.Errorf("discover read the tombstone dir: mems=%d perrs=%v", len(mems), perrs)
	}
}

func TestRestoreRoundTripsByteForByte(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := saveMem(t, root, "nested", "import:claude-code")
	// Move it into a subdirectory: restore must return it to where it was.
	if err := os.Rename(filepath.Join(root, "nested.md"), filepath.Join(root, "sub", "nested.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Forget(root, "nested", Note{}, at); err != nil {
		t.Fatal(err)
	}
	path, err := Restore(root, "nested")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if path != filepath.Join(root, "sub", "nested.md") {
		t.Errorf("restored to %s, want the original subdirectory path", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(orig) {
		t.Errorf("restored content differs (err %v):\n%s\nwant\n%s", err, got, orig)
	}
	if _, err := os.Stat(Path(root, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tombstone should be removed after restore")
	}
}

func TestRestoreRefusesATakenName(t *testing.T) {
	root := t.TempDir()
	saveMem(t, root, "dup", "import:codex")
	if _, err := Forget(root, "dup", Note{}, at); err != nil {
		t.Fatal(err)
	}
	saveMem(t, root, "dup", "remember")
	if _, err := Restore(root, "dup"); !errors.Is(err, ErrTaken) {
		t.Fatalf("err = %v, want ErrTaken", err)
	}
	if _, err := os.Stat(Path(root, "dup")); err != nil {
		t.Errorf("a refused restore must keep the tombstone: %v", err)
	}
	if _, err := Restore(root, "never-forgotten"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestBlocksMatchesNameAndOriginHarness(t *testing.T) {
	set := Set{
		"claude-one": {Name: "claude-one", Origin: "import:claude-code"},
		"codex-one":  {Name: "codex-one", Origin: "detached:import:codex"},
	}
	cand := func(name, origin string) *schema.CanonicalMemory {
		return &schema.CanonicalMemory{Name: name, Provenance: schema.Provenance{Origin: origin}}
	}
	cases := []struct {
		m    *schema.CanonicalMemory
		want bool
	}{
		{cand("claude-one", "import:claude-code"), true},
		{cand("claude-one", "import:claude-code:no-frontmatter"), true},
		{cand("claude-one", "import:codex"), false},
		{cand("codex-one", "import:codex"), true},
		{cand("other", "import:claude-code"), false},
	}
	for _, c := range cases {
		if got := set.Blocks(c.m); got != c.want {
			t.Errorf("Blocks(%s, %s) = %v, want %v", c.m.Name, c.m.Provenance.Origin, got, c.want)
		}
	}
}

// A restore name or a recorded path that escapes the canonical root is refused
// before anything is read or written outside it.
func TestRestoreStaysInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Restore(root, "../escape"); err == nil {
		t.Error("a name with a path separator must be refused")
	}
	bad := "name: evil\npath: ../outside.md\nmemory: x\nforgotten_at: t\n"
	if err := os.MkdirAll(filepath.Dir(Path(root, "evil")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(root, "evil"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(root, "evil"); err == nil {
		t.Error("a recorded path outside the root must be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("restore wrote outside the canonical root")
	}
}

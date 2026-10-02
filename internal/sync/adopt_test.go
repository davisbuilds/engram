package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

// adoptFixture lays out one Claude slug holding a hand-authored original and
// its index line, plus a shared dir holding that memory's render. m is the
// canonical memory imported from the original.
func adoptFixture(t *testing.T, nativeBody string) (a ClaudeSharedAdopt, native, index string) {
	t.Helper()
	home := t.TempDir()
	slugDir := filepath.Join(home, "projects", "-work-x", "memory")
	native = filepath.Join(slugDir, "lesson.md")
	index = filepath.Join(slugDir, "MEMORY.md")
	writeAt(t, native, "---\nname: lesson\ndescription: desc of lesson\nmetadata:\n  type: lesson\n---\n"+nativeBody)
	writeAt(t, index, "- [lesson](lesson.md) — desc of lesson\n- [other](other.md) — keep me\n")
	m := mem("lesson")
	m.Provenance = schema.Provenance{Origin: "import:claude-code", Source: "lesson.md", ImportSource: "-work-x/lesson.md"}
	shared := ClaudeSharedTarget{Dir: filepath.Join(home, "engram", "memory"), RulesFile: filepath.Join(home, "rules", "r.md"), Desired: []*schema.CanonicalMemory{m}}
	if _, err := shared.Apply(); err != nil {
		t.Fatal(err)
	}
	return ClaudeSharedAdopt{ClaudeProjects: filepath.Join(home, "projects"), SharedDir: shared.Dir, Memories: []*schema.CanonicalMemory{m}}, native, index
}

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kindOf(as []MigrateAction, name string) MigrateActionKind {
	for _, a := range as {
		if a.Name == name {
			return a.Kind
		}
	}
	return ""
}

// TestAdoptRetiresAnIdenticalOriginal pins the happy path: an original whose
// content matches canonical is removed with its index line, foreign lines stay.
func TestAdoptRetiresAnIdenticalOriginal(t *testing.T) {
	a, native, index := adoptFixture(t, "body of lesson\n")
	actions, err := a.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if kindOf(actions, "lesson") != Adopt {
		t.Fatalf("plan = %+v, want lesson ADOPT", actions)
	}
	if _, err := os.Stat(native); err != nil {
		t.Fatal("a plan must not remove anything")
	}
	res, err := a.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 1 {
		t.Errorf("adopted = %+v, want lesson", res.Adopted)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Errorf("original survived adoption (err=%v)", err)
	}
	idx := readOr(t, index)
	if strings.Contains(idx, "(lesson.md)") || !strings.Contains(idx, "(other.md)") {
		t.Errorf("index after adoption:\n%s", idx)
	}
}

// TestAdoptLeavesADivergedOriginal pins the gate: an original that says
// something canonical does not is reported and left byte-for-byte.
func TestAdoptLeavesADivergedOriginal(t *testing.T) {
	a, native, _ := adoptFixture(t, "a newer native edit\n")
	before := readOr(t, native)
	res, err := a.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 0 || kindOf(res.Diverged, "lesson") != Diverged {
		t.Errorf("result = %+v, want lesson DIVERGED", res)
	}
	if readOr(t, native) != before {
		t.Error("a diverged original was modified")
	}
}

// TestAdoptNeedsTheSharedRender pins the order: an original is never retired
// before its shared render exists, or the memory would vanish for a session.
func TestAdoptNeedsTheSharedRender(t *testing.T) {
	a, native, _ := adoptFixture(t, "body of lesson\n")
	if err := os.Remove(filepath.Join(a.SharedDir, "lesson.md")); err != nil {
		t.Fatal(err)
	}
	actions, err := a.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if kindOf(actions, "lesson") != Skip {
		t.Errorf("plan = %+v, want lesson SKIP", actions)
	}
	if _, err := a.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(native); err != nil {
		t.Error("original removed while its shared render is missing")
	}
}

// TestAdoptSkipsAnEngramOwnedFile pins marker discipline in reverse: a file at
// the original's path that engram wrote is not a hand-authored original.
func TestAdoptSkipsAnEngramOwnedFile(t *testing.T) {
	a, native, _ := adoptFixture(t, "body of lesson\n")
	writeAt(t, native, "---\nname: lesson\ndescription: desc of lesson\nmetadata:\n  type: lesson\n  origin: engram-sync\n---\nbody of lesson\n")
	actions, err := a.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if kindOf(actions, "lesson") != Skip {
		t.Errorf("plan = %+v, want lesson SKIP", actions)
	}
}

// TestAdoptSkipsAMemoryWithoutAnOriginal pins the guard for a memory with no
// usable import source: a skip, with nothing locked or removed.
func TestAdoptSkipsAMemoryWithoutAnOriginal(t *testing.T) {
	a, native, _ := adoptFixture(t, "body of lesson\n")
	orphan := mem("loose")
	orphan.Provenance.ImportSource = "no-slash"
	a.Memories = append(a.Memories, orphan)
	res, err := a.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if kindOf(res.Skipped, "loose") != Skip || len(res.Adopted) != 1 {
		t.Errorf("result = %+v, want loose skipped and lesson adopted", res)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Error("lesson should still be adopted")
	}
}

package importer

import (
	"path/filepath"
	"reflect"
	"testing"
)

// claudeNote is a native Claude memory file carrying the given frontmatter name.
func claudeNote(name, body string) string {
	return "---\nname: " + name + "\ndescription: d\nmetadata:\n  type: lesson\n---\n" + body + "\n"
}

// Two natives in one slug that normalize to one name are both kept as
// candidates, both lose their import hash, and the collision names the shared
// name and each source in batch order. A distinct name is untouched.
func TestImportClaudeReportsWithinBatchCollision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects", "-work-x", "memory")
	writeMem(t, filepath.Join(dir, "a.md"), claudeNote("Shared Lesson", "first"))
	writeMem(t, filepath.Join(dir, "b.md"), claudeNote("shared_lesson", "second"))
	writeMem(t, filepath.Join(dir, "c.md"), claudeNote("other-lesson", "third"))

	res, err := ImportClaude(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memories) != 3 || len(res.Skipped)+len(res.Dropped) != 0 {
		t.Fatalf("every source stays a candidate: memories %d, skipped %v, dropped %v",
			len(res.Memories), res.Skipped, res.Dropped)
	}
	for _, m := range res.Memories {
		if ambiguous := m.Name == "shared-lesson"; ambiguous != (m.Provenance.ImportHash == "") {
			t.Errorf("%s (%s): import_hash %q; only colliding candidates lose it",
				m.Name, m.Provenance.Source, m.Provenance.ImportHash)
		}
	}
	want := []Collision{{Name: "shared-lesson", Sources: []string{"-work-x/a.md", "-work-x/b.md"}}}
	if !reflect.DeepEqual(res.Collisions, want) {
		t.Errorf("collisions = %+v, want %+v", res.Collisions, want)
	}
}

// A batch whose names are all distinct reports no collision.
func TestImportClaudeNoCollisionForDistinctNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects", "-work-x", "memory")
	writeMem(t, filepath.Join(dir, "a.md"), claudeNote("alpha-lesson", "a"))
	writeMem(t, filepath.Join(dir, "b.md"), claudeNote("beta-lesson", "b"))

	res, err := ImportClaude(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Collisions) != 0 {
		t.Errorf("distinct names must not collide; got %+v", res.Collisions)
	}
	for _, m := range res.Memories {
		if m.Provenance.ImportHash == "" {
			t.Errorf("%s lost its import hash without a collision", m.Name)
		}
	}
}

// The full-tree sweep judges collisions across slugs: same-named files in two
// projects collide once, a within-slug pair is reported once (not again per
// slug), and the report is ordered by name with sources in sweep order, the
// same on every run.
func TestImportClaudeAllReportsCrossSlugCollisionsDeterministically(t *testing.T) {
	home := t.TempDir()
	mem := func(slug string) string { return filepath.Join(home, "projects", slug, "memory") }
	writeMem(t, filepath.Join(mem("-work-y"), "notes.md"), claudeNote("notes", "y"))
	writeMem(t, filepath.Join(mem("-work-x"), "notes.md"), claudeNote("Notes", "x"))
	writeMem(t, filepath.Join(mem("-work-x"), "build.md"), claudeNote("build tips", "x1"))
	writeMem(t, filepath.Join(mem("-work-x"), "build2.md"), claudeNote("Build-Tips", "x2"))
	writeMem(t, filepath.Join(mem("-work-y"), "solo.md"), claudeNote("solo", "y"))

	first, err := ImportClaudeAll(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Memories) != 5 {
		t.Fatalf("every source stays a candidate; got %d", len(first.Memories))
	}
	want := []Collision{
		{Name: "build-tips", Sources: []string{"-work-x/build.md", "-work-x/build2.md"}},
		{Name: "notes", Sources: []string{"-work-x/notes.md", "-work-y/notes.md"}},
	}
	if !reflect.DeepEqual(first.Collisions, want) {
		t.Errorf("collisions = %+v, want %+v", first.Collisions, want)
	}
	again, err := ImportClaudeAll(home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Collisions, first.Collisions) {
		t.Errorf("collisions differ between runs: %+v vs %+v", first.Collisions, again.Collisions)
	}
}

// Codex Task Groups whose titles slugify alike collide, named by their titles;
// a distinct group is untouched and the loop-guard skip is not a collision.
func TestImportCodexReportsTitleCollision(t *testing.T) {
	res := importOne(t, "# Task Group: Fix: Build\n\nfirst body\n\n"+
		"# Task Group: Other Work\n\nunrelated body\n\n"+
		"# Task Group: fix build\n\nsecond body\n\n"+
		"# Task Group: Fix build\n\nextension=engram echo\n")
	if len(res.Memories) != 3 || len(res.Skipped) != 1 {
		t.Fatalf("memories %d, skipped %v; want 3 and the engram group", len(res.Memories), res.Skipped)
	}
	want := []Collision{{Name: "fix-build", Sources: []string{"Fix: Build", "fix build"}}}
	if !reflect.DeepEqual(res.Collisions, want) {
		t.Errorf("collisions = %+v, want %+v", res.Collisions, want)
	}
	for _, m := range res.Memories {
		if ambiguous := m.Name == "fix-build"; ambiguous != (m.Provenance.ImportHash == "") {
			t.Errorf("%q: import_hash %q; only colliding candidates lose it", m.Description, m.Provenance.ImportHash)
		}
	}
}

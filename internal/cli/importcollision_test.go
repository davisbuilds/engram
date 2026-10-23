package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/importer"
)

// collisionEnvelope is the slice of an import or reconcile response the
// collision report lives in.
type collisionEnvelope struct {
	Data struct {
		Collisions []importer.Collision `json:"collisions"`
		Import     []struct {
			Harness    string               `json:"harness"`
			Collisions []importer.Collision `json:"collisions"`
		} `json:"import"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
}

func runCollision(t *testing.T, args ...string) (int, collisionEnvelope, string) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = Run(args) })
	var env collisionEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope: %v\n%s", err, out)
	}
	return code, env, out
}

// An import whose batch holds two natives normalizing to one name reports the
// collision in data.collisions, naming both sources, and warns; the exit code
// stays what it was. A batch without one reports an empty list.
func TestImportReportsWithinBatchCollision(t *testing.T) {
	_, claudeMem, _, args := setupTwoHarnesses(t)

	code, env, out := runCollision(t, append([]string{"import", "claude-code"}, args...)...)
	if code != exitOK || env.Data.Collisions == nil || len(env.Data.Collisions) != 0 {
		t.Fatalf("control: exit %d, collisions %+v; want 0 and []\n%s", code, env.Data.Collisions, out)
	}

	writeFile(t, filepath.Join(claudeMem, "lesson-b.md"),
		"---\nname: Claude Lesson\ndescription: same name, other text\nmetadata:\n  type: lesson\n---\nother body\n")
	code, env, out = runCollision(t, append([]string{"import", "claude-code"}, args...)...)
	if code != exitOK {
		t.Errorf("dry-run exit = %d, want %d", code, exitOK)
	}
	want := []importer.Collision{{Name: "claude-lesson", Sources: []string{"-work-x/lesson-a.md", "-work-x/lesson-b.md"}}}
	if !reflect.DeepEqual(env.Data.Collisions, want) {
		t.Errorf("data.collisions = %+v, want %+v\n%s", env.Data.Collisions, want, out)
	}
	if !hasWarning(env.Warnings, "data.collisions") {
		t.Errorf("collision must be warned about; warnings %v", env.Warnings)
	}
}

// reconcile carries each harness's collisions on its data.import[] entry.
func TestReconcileReportsWithinBatchCollision(t *testing.T) {
	_, _, codexNotes, args := setupTwoHarnesses(t)
	codexFile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(codexNotes))), "MEMORY.md")
	if err := os.WriteFile(codexFile, []byte("# Task Group: Codex Lesson\n\napplies_to: none\n\nfirst body\n\n"+
		"# Task Group: codex lesson\n\napplies_to: none\n\nsecond body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, out := runCollision(t, append([]string{"reconcile"}, args...)...)
	if code != exitConflicts {
		t.Errorf("reconcile exit = %d, want %d (the second candidate conflicts)", code, exitConflicts)
	}
	got := map[string][]importer.Collision{}
	for _, imp := range env.Data.Import {
		got[imp.Harness] = imp.Collisions
	}
	want := []importer.Collision{{Name: "codex-lesson", Sources: []string{"Codex Lesson", "codex lesson"}}}
	if !reflect.DeepEqual(got["codex"], want) {
		t.Errorf("codex collisions = %+v, want %+v\n%s", got["codex"], want, out)
	}
	if c, ok := got["claude-code"]; !ok || c == nil || len(c) != 0 {
		t.Errorf("claude-code collisions = %+v (present %v), want []", c, ok)
	}
	if !hasWarning(env.Warnings, "data.import[].collisions") {
		t.Errorf("collision must be warned about; warnings %v", env.Warnings)
	}
}

func hasWarning(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

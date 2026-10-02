package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/store"
)

// TestMigrateSharedRetiresIdenticalOriginals pins migrate --shared end to end:
// after reconcile shares a Claude-authored global memory, the dry-run plans
// adopting its original; --apply removes the original and its index line and
// detaches the canonical memory, so the next reconcile reports no orphan and
// exits clean, with the memory still in the shared dir.
func TestMigrateSharedRetiresIdenticalOriginals(t *testing.T) {
	_, claudeMem, _, args := setupTwoHarnesses(t)
	writeFile(t, filepath.Join(claudeMem, "MEMORY.md"), "- [claude-lesson](lesson-a.md) — a claude lesson\n- hand line\n")
	defer silenceStdout(t)()
	if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
		t.Fatalf("seed reconcile exit = %d", code)
	}
	native := filepath.Join(claudeMem, "lesson-a.md")

	code, env := runEnvelope(t, append([]string{"migrate", "claude-code", "--shared"}, args...)...)
	if code != exitOK || !strings.Contains(stringify(env["data"]), `"kind":"ADOPT"`) {
		t.Fatalf("dry-run exit = %d, data = %s", code, stringify(env["data"]))
	}
	if !exists(native) {
		t.Fatal("a dry-run must not remove the original")
	}
	if code, env := runEnvelope(t, append([]string{"migrate", "claude-code", "--shared", "--apply"}, args...)...); code != exitOK {
		t.Fatalf("apply exit = %d: %v", code, env["error"])
	}
	if exists(native) || exists(filepath.Join(claudeMem, "claude-lesson.md")) {
		t.Error("the slug still holds the memory; adoption into the shared dir leaves no copy there")
	}
	idx, _ := os.ReadFile(filepath.Join(claudeMem, "MEMORY.md"))
	if strings.Contains(string(idx), "lesson-a.md") || !strings.Contains(string(idx), "- hand line") {
		t.Errorf("index after adoption:\n%s", idx)
	}
	canon := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(claudeMem)))), "canonical")
	m, _, found, err := store.Load(canon, "claude-lesson")
	if err != nil || !found || !strings.HasPrefix(m.Provenance.Origin, "detached:") {
		t.Fatalf("canonical claude-lesson = %+v (found %v, err %v), want it detached", m, found, err)
	}
	if !exists(filepath.Join(sharedDirOf(claudeMem), "claude-lesson.md")) {
		t.Error("the shared render is gone")
	}
	code, env = runEnvelope(t, append([]string{"reconcile"}, args...)...)
	if code != exitOK || strings.Contains(stringify(env["data"]), `"orphaned":[{`) {
		t.Errorf("reconcile after adoption exit = %d, data = %s", code, stringify(env["data"]))
	}
}

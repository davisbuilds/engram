package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/tombstone"
)

// forgetFixture reconciles both harnesses, so canonical holds claude-lesson
// (native lesson-a.md in slug -work-x) and codex-lesson (a MEMORY.md Task
// Group), each rendered into the other harness. It adds an engram-owned render
// of codex-lesson in a second slug and a hand-authored file of that name in a
// third.
func forgetFixture(t *testing.T) (canon, claudeMem, codexNotes string, args []string) {
	t.Helper()
	canon, claudeMem, codexNotes, args = setupTwoHarnesses(t)
	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
			t.Fatalf("seed reconcile exit = %d", code)
		}
	}()
	projects := filepath.Dir(filepath.Dir(claudeMem))
	render, err := os.ReadFile(filepath.Join(claudeMem, "codex-lesson.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(projects, "-work-y", "memory", "codex-lesson.md"), string(render))
	writeFile(t, filepath.Join(projects, "-work-y", "memory", "MEMORY.md"),
		"- [codex-lesson](codex-lesson.md) — a lesson <!-- engram name=codex-lesson -->\n- hand line\n")
	writeFile(t, filepath.Join(projects, "-work-z", "memory", "codex-lesson.md"),
		"---\nname: codex-lesson\ndescription: mine\n---\nhand-authored\n")
	return canon, claudeMem, codexNotes, args
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestForgetDryRunWritesNothing(t *testing.T) {
	canon, claudeMem, _, args := forgetFixture(t)
	code, env := runEnvelope(t, append([]string{"forget", "codex-lesson"}, args...)...)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d: %v", code, exitOK, env["error"])
	}
	if !exists(filepath.Join(canon, "codex-lesson.md")) || exists(tombstone.Path(canon, "codex-lesson")) {
		t.Error("a dry-run must not touch canonical or write a tombstone")
	}
	if !exists(filepath.Join(claudeMem, "codex-lesson.md")) {
		t.Error("a dry-run must not remove renders")
	}
	data, _ := env["data"].(map[string]any)
	renders, _ := data["renders"].([]any)
	if len(renders) != 2 {
		t.Errorf("dry-run should plan the two engram-owned Claude renders, got %v", renders)
	}
}

func TestForgetApplyRetiresTheMemoryAndItsRenders(t *testing.T) {
	canon, claudeMem, codexNotes, args := forgetFixture(t)
	projects := filepath.Dir(filepath.Dir(claudeMem))
	code, env := runEnvelope(t, append([]string{"forget", "codex-lesson", "claude-lesson", "--reason", "retired", "--apply"}, args...)...)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d: %v", code, exitOK, env["error"])
	}
	for _, n := range []string{"codex-lesson", "claude-lesson"} {
		if exists(filepath.Join(canon, n+".md")) {
			t.Errorf("%s still in canonical", n)
		}
	}
	set, err := tombstone.Load(canon)
	cl, _ := set.Latest("codex-lesson")
	al, _ := set.Latest("claude-lesson")
	if err != nil || cl.Reason != "retired" || al.Origin != "import:claude-code" {
		t.Errorf("tombstones = %+v (err %v)", set, err)
	}

	// Every engram-owned render is gone, index lines included...
	for _, p := range []string{
		filepath.Join(claudeMem, "codex-lesson.md"),
		filepath.Join(projects, "-work-y", "memory", "codex-lesson.md"),
	} {
		if exists(p) {
			t.Errorf("engram render %s should be removed", p)
		}
	}
	for _, dir := range []string{claudeMem, filepath.Join(projects, "-work-y", "memory")} {
		idx, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
		if strings.Contains(string(idx), "engram name=codex-lesson") {
			t.Errorf("%s still indexes codex-lesson:\n%s", dir, idx)
		}
	}
	if notes := codexNotesFor(t, filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(codexNotes))))); notes["claude-lesson"] {
		t.Error("claude-lesson's Codex note should be removed")
	}
	// ...while hand-authored files and lines are untouched.
	if !exists(filepath.Join(projects, "-work-z", "memory", "codex-lesson.md")) {
		t.Error("a hand-authored file sharing the name must never be removed")
	}
	if idx, _ := os.ReadFile(filepath.Join(projects, "-work-y", "memory", "MEMORY.md")); !strings.Contains(string(idx), "- hand line") {
		t.Errorf("a hand-authored index line was removed:\n%s", idx)
	}
	native := filepath.Join(claudeMem, "lesson-a.md")
	if !exists(native) {
		t.Fatal("the Claude native source must never be removed")
	}

	// The surviving native sources are reported, and the Claude one gets a lead.
	data, _ := env["data"].(map[string]any)
	natives := map[string][]any{}
	for _, m := range data["memories"].([]any) {
		row := m.(map[string]any)
		natives[row["name"].(string)], _ = row["natives"].([]any)
	}
	if len(natives["claude-lesson"]) != 1 || natives["claude-lesson"][0] != native {
		t.Errorf("claude-lesson natives = %v, want [%s]", natives["claude-lesson"], native)
	}
	if len(natives["codex-lesson"]) != 1 || !strings.Contains(natives["codex-lesson"][0].(string), "Codex Lesson") {
		t.Errorf("codex-lesson natives = %v, want its Task Group", natives["codex-lesson"])
	}
	next, _ := env["next_steps"].([]any)
	found := false
	for _, n := range next {
		if strings.Contains(n.(map[string]any)["command"].(string), native) {
			found = true
		}
	}
	if !found {
		t.Errorf("no next_step points at the surviving Claude native: %v", next)
	}
}

func TestForgetUnknownNameWritesNothing(t *testing.T) {
	canon, _, _, args := forgetFixture(t)
	code, env := runEnvelope(t, append([]string{"forget", "codex-lesson", "no-such-memory", "--apply"}, args...)...)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if e, _ := env["error"].(map[string]any); e == nil || e["code"] != "unknown_memory" || !strings.Contains(e["message"].(string), "no-such-memory") {
		t.Errorf("error = %v, want unknown_memory naming no-such-memory", env["error"])
	}
	if !exists(filepath.Join(canon, "codex-lesson.md")) || exists(tombstone.Path(canon, "codex-lesson")) {
		t.Error("an unknown name must stop the whole batch before any write")
	}
}

func TestForgetRestore(t *testing.T) {
	canon, _, _, args := forgetFixture(t)
	orig, err := os.ReadFile(filepath.Join(canon, "codex-lesson.md"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(argv ...string) (int, map[string]any) {
		return runEnvelope(t, append(argv, args...)...)
	}
	if code, env := run("forget", "codex-lesson", "--apply"); code != exitOK {
		t.Fatalf("forget exit = %d: %v", code, env["error"])
	}
	if code, _ := run("forget", "--restore", "codex-lesson"); code != exitOK || exists(filepath.Join(canon, "codex-lesson.md")) {
		t.Fatalf("restore dry-run exit = %d, or it wrote the file", code)
	}
	if code, env := run("forget", "--restore", "never-forgotten", "--apply"); code != exitUsage {
		t.Errorf("restoring an unknown tombstone: exit = %d, want %d: %v", code, exitUsage, env["error"])
	}
	if code, env := run("forget", "--restore", "codex-lesson", "--apply"); code != exitOK {
		t.Fatalf("restore exit = %d: %v", code, env["error"])
	}
	got, err := os.ReadFile(filepath.Join(canon, "codex-lesson.md"))
	if err != nil || string(got) != string(orig) {
		t.Errorf("restore did not bring back the original (err %v)", err)
	}
	if exists(tombstone.Path(canon, "codex-lesson")) {
		t.Error("restore should remove the tombstone")
	}

	// A name taken again since the forget is refused, and nothing is written.
	if code, _ := run("forget", "codex-lesson", "--apply"); code != exitOK {
		t.Fatal("re-forget failed")
	}
	writeFile(t, filepath.Join(canon, "codex-lesson.md"), "---\nname: codex-lesson\ndescription: new\ntype: lesson\nscope: global\n---\nnew\n")
	if code, _ := run("forget", "--restore", "codex-lesson", "--apply"); code != exitConflicts {
		t.Errorf("restore over a taken name: exit = %d, want %d", code, exitConflicts)
	}
	if !exists(tombstone.Path(canon, "codex-lesson")) {
		t.Error("a refused restore must keep the tombstone")
	}
}

func TestForgetArgumentErrors(t *testing.T) {
	_, _, _, args := forgetFixture(t)
	for _, argv := range [][]string{
		{"forget"},
		{"forget", "--apply"},
		{"forget", "--restore", "codex-lesson", "--reason", "x"},
		{"forget", "Not_A_Name"},
	} {
		if code, _ := runEnvelope(t, append(argv, args...)...); code != exitUsage {
			t.Errorf("%v: exit = %d, want %d", argv, code, exitUsage)
		}
	}
}

// The dry-run's lead is the command to run next, so it must carry every choice
// the dry-run was given, not just the names.
func TestForgetDryRunLeadKeepsReasonAndSuccessor(t *testing.T) {
	_, _, _, args := forgetFixture(t)
	_, env := runEnvelope(t, append([]string{"forget", "codex-lesson", "--reason", "it's stale", "--successor", "claude-lesson"}, args...)...)
	cmds := nextCommands(env)
	for _, want := range []string{"--reason 'it'\\''s stale'", "--successor claude-lesson", "--apply"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("dry-run lead missing %q:\n%s", want, cmds)
		}
	}
}

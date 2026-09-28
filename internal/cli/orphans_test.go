package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonFile(t *testing.T, canon, name, origin, source, body string) {
	t.Helper()
	src := ""
	if source != "" {
		src = "\n    source: " + source
	}
	writeFile(t, filepath.Join(canon, name+".md"), "---\nname: "+name+"\ndescription: d\ntype: reference\nscope: global\nprovenance:\n    origin: "+origin+src+"\n---\n"+body)
}

// orphanFixture: canonical holds Codex-origin old-group (gone from MEMORY.md,
// sharing a session with New Group), still-here (present), kept-one (gone but
// detached), and Claude-origin gone-lesson (its native file deleted) and
// live-lesson (native present).
func orphanFixture(t *testing.T) (canon, codexHome string, args []string) {
	t.Helper()
	dir := t.TempDir()
	canon = filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	codexHome = filepath.Join(dir, "codex")
	const u1 = "019ffc76-3cb2-7420-8af3-95fbe1a4a753"
	canonFile(t, canon, "old-group", "import:codex", "", "- thread_id="+u1+"\n")
	canonFile(t, canon, "still-here", "import:codex", "", "still\n")
	canonFile(t, canon, "kept-one", "detached:import:codex", "", "kept\n")
	canonFile(t, canon, "gone-lesson", "import:claude-code", "gone.md", "gone\n")
	canonFile(t, canon, "live-lesson", "import:claude-code", "live.md", "live\n")
	writeFile(t, filepath.Join(codexHome, "memories", "MEMORY.md"),
		"# Task Group: New Group\n\n- thread_id="+u1+"\n\n# Task Group: Still Here\n\nstill\n\n# Task Group: Unrelated\n\nother\n")
	writeFile(t, filepath.Join(claude, "projects", "-work-x", "memory", "live.md"),
		"---\nname: live-lesson\ndescription: d\nmetadata:\n  type: reference\n---\nlive\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n  codex:\n    home: "+codexHome+"\n")
	return canon, codexHome, []string{"--config", cfg, "--cwd", "/work/x"}
}

// orphanRows returns the orphaned rows of an envelope's data (or of each
// reconcile import entry), by name.
func orphanRows(env map[string]any) (map[string]map[string]any, bool) {
	data, _ := env["data"].(map[string]any)
	var lists []any
	present := false
	if l, ok := data["orphaned"].([]any); ok {
		lists, present = append(lists, l...), true
	}
	if imps, ok := data["import"].([]any); ok {
		for _, imp := range imps {
			if l, ok := imp.(map[string]any)["orphaned"].([]any); ok {
				lists, present = append(lists, l...), true
			}
		}
	}
	rows := map[string]map[string]any{}
	for _, r := range lists {
		row := r.(map[string]any)
		rows[row["name"].(string)] = row
	}
	return rows, present
}

func nextCommands(env map[string]any) string {
	var b strings.Builder
	for _, n := range env["next_steps"].([]any) {
		b.WriteString(n.(map[string]any)["command"].(string) + "\n")
	}
	return b.String()
}

func TestCodexOrphansAreReportedWithSuccessors(t *testing.T) {
	_, _, args := orphanFixture(t)
	for _, argv := range [][]string{{"import", "codex"}, {"reconcile"}} {
		_, env := runEnvelope(t, append(argv, args...)...)
		rows, _ := orphanRows(env)
		row, ok := rows["old-group"]
		if !ok {
			t.Fatalf("%v: old-group not reported as orphaned: %v", argv, rows)
		}
		if succ, _ := row["successors"].([]any); len(succ) != 1 || succ[0] != "new-group" {
			t.Errorf("%v: successors = %v, want [new-group]", argv, row["successors"])
		}
		for _, n := range []string{"still-here", "kept-one", "live-lesson"} {
			if _, ok := rows[n]; ok {
				t.Errorf("%v: %s must not be reported as orphaned", argv, n)
			}
		}
		cmds := nextCommands(env)
		for _, want := range []string{"engram forget old-group --successor new-group", "engram detach old-group --apply"} {
			if !strings.Contains(cmds, want) {
				t.Errorf("%v: next_steps missing %q:\n%s", argv, want, cmds)
			}
		}
	}
}

// The negative control: with no Task Groups to compare against (a stalled or
// reset consolidator), nothing is an orphan, and the skip is said out loud.
func TestOrphanDetectionSkipsAnEmptyCodexSource(t *testing.T) {
	_, codexHome, args := orphanFixture(t)
	if err := os.Remove(filepath.Join(codexHome, "memories", "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	_, env := runEnvelope(t, append([]string{"import", "codex"}, args...)...)
	if rows, present := orphanRows(env); !present || len(rows) != 0 {
		t.Errorf("orphaned = %v (present %v), want an empty list", rows, present)
	}
	warns, _ := env["warnings"].([]any)
	if !strings.Contains(strings.Join(anyStrings(warns), "\n"), "orphan detection skipped") {
		t.Errorf("warnings %v should say orphan detection was skipped", warns)
	}
}

// A single-slug Claude import cannot tell a deleted native from one in another
// slug, so only the all-slug scan (and reconcile, which uses it) reports.
func TestClaudeOrphansOnlyOnTheAllSlugScan(t *testing.T) {
	_, _, args := orphanFixture(t)
	if _, env := runEnvelope(t, append([]string{"import", "claude-code"}, args...)...); true {
		if _, present := orphanRows(env); present {
			t.Error("a single-slug import must not run orphan detection")
		}
	}
	for _, argv := range [][]string{{"import", "claude-code", "--all"}, {"reconcile"}} {
		_, env := runEnvelope(t, append(argv, args...)...)
		rows, _ := orphanRows(env)
		if _, ok := rows["gone-lesson"]; !ok {
			t.Errorf("%v: gone-lesson not reported as orphaned: %v", argv, rows)
		}
		if _, ok := rows["live-lesson"]; ok {
			t.Errorf("%v: live-lesson's native exists; not an orphan", argv)
		}
	}
}

func TestDetach(t *testing.T) {
	canon, _, args := orphanFixture(t)
	before, _ := os.ReadFile(filepath.Join(canon, "old-group.md"))
	if code, env := runEnvelope(t, append([]string{"detach", "old-group"}, args...)...); code != exitOK {
		t.Fatalf("dry-run exit = %d: %v", code, env["error"])
	}
	if now, _ := os.ReadFile(filepath.Join(canon, "old-group.md")); string(now) != string(before) {
		t.Fatal("a dry-run must not write")
	}
	if code, _ := runEnvelope(t, append([]string{"detach", "old-group", "nope", "--apply"}, args...)...); code != exitUsage {
		t.Errorf("unknown name: exit = %d, want %d", code, exitUsage)
	}
	if code, env := runEnvelope(t, append([]string{"detach", "old-group", "--apply"}, args...)...); code != exitOK {
		t.Fatalf("detach exit = %d: %v", code, env["error"])
	}
	after, _ := os.ReadFile(filepath.Join(canon, "old-group.md"))
	if !strings.Contains(string(after), "origin: detached:import:codex") {
		t.Errorf("origin not detached:\n%s", after)
	}
	if !strings.HasSuffix(string(after), "- thread_id=019ffc76-3cb2-7420-8af3-95fbe1a4a753\n") {
		t.Errorf("detach changed the body:\n%s", after)
	}
	_, env := runEnvelope(t, append([]string{"import", "codex"}, args...)...)
	if rows, _ := orphanRows(env); len(rows) != 0 {
		t.Errorf("a detached memory must no longer be reported: %v", rows)
	}
	_, env = runEnvelope(t, append([]string{"detach", "old-group"}, args...)...)
	data, _ := env["data"].(map[string]any)
	if row := data["memories"].([]any)[0].(map[string]any); row["outcome"] != "unchanged" {
		t.Errorf("detaching again: outcome = %v, want unchanged", row["outcome"])
	}
}

func anyStrings(xs []any) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.(string))
	}
	return out
}

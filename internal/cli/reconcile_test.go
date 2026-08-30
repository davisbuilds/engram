package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTwoHarnesses writes a Claude native memory and a Codex Task Group, plus a
// config enabling both, and returns the canonical root, the Claude memory dir,
// the Codex extension notes dir, and the shared CLI args.
func setupTwoHarnesses(t *testing.T) (canon, claudeMem, codexNotes string, args []string) {
	t.Helper()
	dir := t.TempDir()
	canon = filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	codex := filepath.Join(dir, "codex")
	claudeMem = filepath.Join(claude, "projects", "-work-x", "memory")
	codexNotes = filepath.Join(codex, "memories", "extensions", "engram", "notes")

	writeFile(t, filepath.Join(claudeMem, "lesson-a.md"),
		"---\nname: claude-lesson\ndescription: a claude lesson\nmetadata:\n  type: lesson\n---\nclaude body\n")
	writeFile(t, filepath.Join(codex, "memories", "MEMORY.md"),
		"# Task Group: Codex Lesson\n\napplies_to: none\n\ncodex body worth keeping\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n  codex:\n    home: "+codex+"\n")
	return canon, claudeMem, codexNotes, []string{"--config", cfg, "--cwd", "/work/x", "--json"}
}

// End-to-end: reconcile --apply imports both harnesses into canonical and
// propagates each harness's lesson into the *other* one, without conflicting on
// or overwriting either harness's own native memory.
func TestRunReconcileApplyCrossPropagates(t *testing.T) {
	canon, claudeMem, codexNotes, args := setupTwoHarnesses(t)
	defer silenceStdout(t)()

	if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
		t.Fatalf("reconcile --apply exit = %d, want %d (origin filter should avoid self-conflict)", code, exitOK)
	}

	// Both native memories are now in canonical.
	for _, n := range []string{"claude-lesson", "codex-lesson"} {
		if _, err := os.Stat(filepath.Join(canon, n+".md")); err != nil {
			t.Errorf("canonical missing %s: %v", n, err)
		}
	}

	// Codex's lesson propagated INTO Claude as an engram-owned file...
	got, err := os.ReadFile(filepath.Join(claudeMem, "codex-lesson.md"))
	if err != nil {
		t.Fatalf("codex lesson not propagated into Claude: %v", err)
	}
	if !strings.Contains(string(got), "origin: engram-sync") {
		t.Errorf("propagated file not engram-owned:\n%s", got)
	}
	// ...while Claude's own native lesson is untouched (not re-rendered, no marker).
	orig, err := os.ReadFile(filepath.Join(claudeMem, "lesson-a.md"))
	if err != nil || strings.Contains(string(orig), "origin: engram-sync") {
		t.Errorf("Claude's native lesson-a.md should be untouched; err=%v content=%s", err, orig)
	}

	// Claude's lesson propagated INTO Codex as a note.
	entries, _ := os.ReadDir(codexNotes)
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-claude-lesson.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("claude lesson not propagated into Codex notes dir %s: %v", codexNotes, entries)
	}
}

// Dry-run reconcile writes nothing to canonical.
func TestRunReconcileDryRunWritesNothing(t *testing.T) {
	canon, _, _, args := setupTwoHarnesses(t)
	var out string
	code := -1
	out = captureStdout(t, func() { code = Run(append([]string{"reconcile"}, args...)) })
	if code != exitOK {
		t.Fatalf("reconcile dry-run exit = %d", code)
	}
	if entries, err := os.ReadDir(canon); err == nil && len(entries) > 0 {
		t.Errorf("dry-run wrote to canonical: %v", entries)
	}
	// It still previews what it would import.
	if !strings.Contains(out, "claude-lesson") || !strings.Contains(out, "codex-lesson") {
		t.Errorf("dry-run should preview both imports; output:\n%s", out)
	}
}

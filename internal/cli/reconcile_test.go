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

// sharedDirOf is the shared Claude memory dir for the Claude home holding the
// project memory dir claudeMem (<home>/projects/<slug>/memory).
func sharedDirOf(claudeMem string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(claudeMem))), "engram", "memory")
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

	for _, n := range []string{"claude-lesson", "codex-lesson"} {
		if _, err := os.Stat(filepath.Join(canon, n+".md")); err != nil {
			t.Errorf("canonical missing %s: %v", n, err)
		}
	}

	got, err := os.ReadFile(filepath.Join(sharedDirOf(claudeMem), "codex-lesson.md"))
	if err != nil {
		t.Fatalf("codex lesson not propagated into Claude's shared dir: %v", err)
	}
	if !strings.Contains(string(got), "origin: engram-sync") {
		t.Errorf("propagated file not engram-owned:\n%s", got)
	}
	orig, err := os.ReadFile(filepath.Join(claudeMem, "lesson-a.md"))
	if err != nil || strings.Contains(string(orig), "origin: engram-sync") {
		t.Errorf("Claude's native lesson-a.md should be untouched; err=%v content=%s", err, orig)
	}

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

func TestReconcileApplyReportsSaveConflict(t *testing.T) {
	canon, _, _, args := setupTwoHarnesses(t)
	occupied := filepath.Join(canon, "claude-lesson.md")
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	code, rows := reconcileRows(t, append([]string{"--apply"}, args...)...)
	if got := rows["claude-lesson"]["outcome"]; code != exitConflicts || got != "conflict" {
		t.Errorf("occupied canonical path: exit = %d, outcome = %q; want %d, conflict", code, got, exitConflicts)
	}
	if got := rows["codex-lesson"]["outcome"]; got != "created" {
		t.Errorf("unblocked import outcome = %q, want created", got)
	}
}

// Preview parity (regression for the dry-run-vs-apply gap): on a first run with
// empty canonical, the dry-run must already show the cross-harness propagation
// CREATEs that --apply will perform — computed from the merged (canonical +
// imports) set, not the pre-import canonical.
func TestRunReconcileDryRunPreviewsPostImportPropagation(t *testing.T) {
	_, _, _, args := setupTwoHarnesses(t)
	out := captureStdout(t, func() {
		if code := Run(append([]string{"reconcile"}, args...)); code != exitOK {
			t.Fatalf("dry-run exit = %d", code)
		}
	})
	for _, want := range []string{"\"CREATE\"", "codex-lesson", "claude-lesson"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run preview missing %q (post-import propagation not simulated):\n%s", want, out)
		}
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
	if !strings.Contains(out, "claude-lesson") || !strings.Contains(out, "codex-lesson") {
		t.Errorf("dry-run should preview both imports; output:\n%s", out)
	}
}

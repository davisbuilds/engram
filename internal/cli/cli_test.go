package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// silenceStdout redirects command output to /dev/null for the test's duration so
// the JSON envelopes don't clutter the test log.
func silenceStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f
	return func() { os.Stdout = old; _ = f.Close() }
}

// TestRunSyncLifecycle exercises the full CLI wiring and pins the agent-first
// exit-code contract: 0 for a clean apply, 0 for an idempotent audit, 3 when an
// unmarked collision forces a CONFLICT.
func TestRunSyncLifecycle(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	writeFile(t, filepath.Join(canon, "m.md"),
		"---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg,
		"canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n")

	defer silenceStdout(t)()
	base := []string{"--config", cfg, "--cwd", "/work/x", "--json"}

	if code := Run(append([]string{"sync", "--apply"}, base...)); code != exitOK {
		t.Fatalf("apply exit = %d, want %d", code, exitOK)
	}
	memFile := filepath.Join(claude, "projects", "-work-x", "memory", "a-mem.md")
	if _, err := os.Stat(memFile); err != nil {
		t.Fatalf("rendered memory file missing: %v", err)
	}

	if code := Run(append([]string{"audit"}, base...)); code != exitOK {
		t.Errorf("idempotent audit exit = %d, want %d", code, exitOK)
	}

	// A hand-authored file colliding with the desired name forces a conflict.
	writeFile(t, memFile, "---\nname: a-mem\ndescription: hand\n---\nmine\n")
	if code := Run(append([]string{"sync", "--apply"}, base...)); code != exitConflicts {
		t.Errorf("conflict apply exit = %d, want %d", code, exitConflicts)
	}
}

// TestRunRememberAndShare pins the authoring write-path: create, idempotent
// re-remember, canonical-side CONFLICT on a hand-edited file (exit 3, SC-13), and
// a scope move via share.
func TestRunRememberAndShare(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")
	defer silenceStdout(t)()
	c := []string{"--config", cfg, "--json"}
	memArgs := []string{"remember", "--name", "a-mem", "--description", "d", "--type", "lesson", "--scope", "global"}

	if code := Run(append(memArgs, c...)); code != exitOK {
		t.Fatalf("remember exit = %d, want %d", code, exitOK)
	}
	memFile := filepath.Join(canon, "a-mem.md")
	if _, err := os.Stat(memFile); err != nil {
		t.Fatalf("canonical file missing: %v", err)
	}
	if code := Run(append(memArgs, c...)); code != exitOK {
		t.Errorf("idempotent remember exit = %d, want %d", code, exitOK)
	}

	// Hand-edit to differing content, then remember without --force must conflict.
	writeFile(t, memFile, "---\nname: a-mem\ndescription: HAND\ntype: lesson\nscope: global\n---\nx\n")
	if code := Run(append(memArgs, c...)); code != exitConflicts {
		t.Errorf("conflict remember exit = %d, want %d", code, exitConflicts)
	}

	if code := Run(append([]string{"share", "a-mem", "--to", "project:acme"}, c...)); code != exitOK {
		t.Fatalf("share exit = %d, want %d", code, exitOK)
	}
	got, err := os.ReadFile(memFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "scope: project:acme") {
		t.Errorf("share did not update scope:\n%s", got)
	}
}

func TestRunUnknownCommandIsUsageError(t *testing.T) {
	defer silenceStdout(t)()
	if code := Run([]string{"bogus", "--json"}); code != exitUsage {
		t.Errorf("unknown command exit = %d, want %d", code, exitUsage)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexNotesFor lists the canonical names of the engram notes in a Codex home.
func codexNotesFor(t *testing.T, codexHome string) map[string]bool {
	t.Helper()
	dir := filepath.Join(codexHome, "memories", "extensions", "engram", "notes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if _, rest, ok := strings.Cut(string(b), "canonical="); ok {
			got[strings.Fields(rest)[0]] = true
		}
	}
	return got
}

// Codex has one notes directory for every cwd, so propagating from one project
// must not delete the notes another project's run rendered; a retired memory's
// note is still removed by a run that can see it.
func TestCodexNotesSurviveARunFromAnotherProject(t *testing.T) {
	for _, cmd := range []string{"sync", "reconcile"} {
		t.Run(cmd, func(t *testing.T) {
			dir := t.TempDir()
			canon := filepath.Join(dir, "canonical")
			codex := filepath.Join(dir, "codex")
			writeFile(t, filepath.Join(canon, "alpha-lesson.md"),
				"---\nname: alpha-lesson\ndescription: d\ntype: lesson\nscope: project:alpha\n---\nbody\n")
			writeFile(t, filepath.Join(canon, "beta-lesson.md"),
				"---\nname: beta-lesson\ndescription: d\ntype: lesson\nscope: project:beta\n---\nbody\n")
			cfg := filepath.Join(dir, "c.yaml")
			writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+
				filepath.Join(dir, "claude")+"\n  codex:\n    home: "+codex+"\n")
			defer silenceStdout(t)()
			run := func(cwd string) {
				t.Helper()
				if code := Run([]string{cmd, "--apply", "--config", cfg, "--cwd", cwd, "--json"}); code != exitOK {
					t.Fatalf("%s --apply from %s exit = %d, want %d", cmd, cwd, code, exitOK)
				}
			}

			run(filepath.Join(dir, "w", "alpha"))
			run(filepath.Join(dir, "w", "beta"))
			if got := codexNotesFor(t, codex); !got["alpha-lesson"] || !got["beta-lesson"] {
				t.Fatalf("after runs from alpha then beta, Codex notes = %v; want both", got)
			}

			if err := os.Remove(filepath.Join(canon, "alpha-lesson.md")); err != nil {
				t.Fatal(err)
			}
			run(filepath.Join(dir, "w", "beta"))
			if !codexNotesFor(t, codex)["alpha-lesson"] {
				t.Error("a run from beta removed a note only alpha can see")
			}
			run(filepath.Join(dir, "w", "alpha"))
			if codexNotesFor(t, codex)["alpha-lesson"] {
				t.Error("a run from alpha kept the note of a retired alpha memory")
			}
		})
	}
}

// The agent axis narrows a session the same way cwd does: a note a --agent
// claude run rendered into the shared Codex directory is not stale to a
// --agent codex run from the same cwd.
func TestCodexNotesSurviveARunAsAnotherAgent(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	codex := filepath.Join(dir, "codex")
	writeFile(t, filepath.Join(canon, "claude-only.md"),
		"---\nname: claude-only\ndescription: d\ntype: lesson\nscope: global\napplies_to:\n  agents: [claude]\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+
		filepath.Join(dir, "claude")+"\n  codex:\n    home: "+codex+"\n")
	defer silenceStdout(t)()
	for _, agent := range []string{"claude", "codex"} {
		if code := Run([]string{"sync", "--apply", "--config", cfg, "--cwd", dir, "--agent", agent, "--json"}); code != exitOK {
			t.Fatalf("sync --agent %s exit = %d, want %d", agent, code, exitOK)
		}
	}
	if !codexNotesFor(t, codex)["claude-only"] {
		t.Error("a --agent codex run removed a note only a claude session can see")
	}
}

// A config listing only claude-code (a scratch area) must not write the
// unlisted Codex harness at its default, real home.
func TestPartialHarnessConfigLeavesTheUnlistedHarnessAlone(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	writeFile(t, filepath.Join(canon, "a-mem.md"),
		"---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+filepath.Join(dir, "claude")+"\n")
	defer silenceStdout(t)()
	if code := Run([]string{"sync", "--apply", "--config", cfg, "--cwd", dir, "--json"}); code != exitOK {
		t.Fatalf("sync --apply exit = %d, want %d", code, exitOK)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "memories", "extensions", "engram")); err == nil {
		t.Error("sync wrote the unlisted codex harness at the default home")
	}
}

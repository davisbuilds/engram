package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sharedFixture is a config with a Claude home and a canonical root holding a
// plain global memory, a project memory, and a cwd-narrowed global one.
func sharedFixture(t *testing.T) (claude string, c []string) {
	t.Helper()
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude = filepath.Join(dir, "claude")
	writeFile(t, filepath.Join(canon, "g-mem.md"), "---\nname: g-mem\ndescription: d\ntype: lesson\nscope: global\n---\nglobal\n")
	writeFile(t, filepath.Join(canon, "p-mem.md"), "---\nname: p-mem\ndescription: d\ntype: lesson\nscope: project:x\n---\nproject\n")
	writeFile(t, filepath.Join(canon, "n-mem.md"), "---\nname: n-mem\ndescription: d\ntype: lesson\nscope: global\napplies_to:\n    cwd:\n        - /work/x\n---\nnarrowed\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n")
	return claude, []string{"--config", cfg, "--json"}
}

// TestSyncRendersGlobalOnceInShared pins the split: a plain global memory renders
// once into the shared dir (with the rules file importing its index) and not
// into the project slug, while project-tier and cwd-narrowed memories stay
// per-slug.
func TestSyncRendersGlobalOnceInShared(t *testing.T) {
	claude, c := sharedFixture(t)
	defer silenceStdout(t)()
	if code := Run(append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)); code != exitOK {
		t.Fatalf("sync exit = %d, want %d", code, exitOK)
	}
	shared := filepath.Join(claude, "engram", "memory")
	slug := filepath.Join(claude, "projects", "-work-x", "memory")
	if !exists(filepath.Join(shared, "g-mem.md")) {
		t.Error("global memory not rendered into the shared dir")
	}
	if exists(filepath.Join(slug, "g-mem.md")) {
		t.Error("global memory also rendered into the project slug")
	}
	for _, n := range []string{"p-mem", "n-mem"} {
		if !exists(filepath.Join(slug, n+".md")) {
			t.Errorf("%s not rendered into the project slug", n)
		}
		if exists(filepath.Join(shared, n+".md")) {
			t.Errorf("%s rendered into the shared dir", n)
		}
	}
	rules, err := os.ReadFile(filepath.Join(claude, "rules", "engram-memory.md"))
	if err != nil {
		t.Fatalf("rules file missing: %v", err)
	}
	if !strings.Contains(string(rules), "@"+filepath.Join(shared, "MEMORY.md")) {
		t.Errorf("rules file does not import the shared index:\n%s", rules)
	}
}

// TestSyncRemovesSlugCopyOfSharedMemory pins the migration: an engram render of
// a now-shared memory left in a project slug by an earlier sync is removed.
func TestSyncRemovesSlugCopyOfSharedMemory(t *testing.T) {
	claude, c := sharedFixture(t)
	slug := filepath.Join(claude, "projects", "-work-x", "memory")
	writeFile(t, filepath.Join(slug, "g-mem.md"), "---\nname: g-mem\ndescription: d\nmetadata:\n  type: lesson\n  origin: engram-sync\n---\nglobal\n")
	defer silenceStdout(t)()
	if code := Run(append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)); code != exitOK {
		t.Fatalf("sync exit = %d, want %d", code, exitOK)
	}
	if exists(filepath.Join(slug, "g-mem.md")) {
		t.Error("the slug's old render of a shared memory was not removed")
	}
}

// TestReconcileSharesClaudeOriginGlobal pins the point of the shared index: a
// global memory authored natively in one Claude slug is shared, while its
// hand-authored original in that slug is left alone.
func TestReconcileSharesClaudeOriginGlobal(t *testing.T) {
	_, claudeMem, _, args := setupTwoHarnesses(t)
	defer silenceStdout(t)()
	if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
		t.Fatalf("reconcile exit = %d, want %d", code, exitOK)
	}
	claude := filepath.Dir(filepath.Dir(filepath.Dir(claudeMem)))
	shared := filepath.Join(claude, "engram", "memory")
	for _, n := range []string{"claude-lesson", "codex-lesson"} {
		if !exists(filepath.Join(shared, n+".md")) {
			t.Errorf("%s not in the shared dir", n)
		}
	}
	if !exists(filepath.Join(claudeMem, "lesson-a.md")) {
		t.Error("the hand-authored original was removed")
	}
}

// TestReconcileProjectClaudeOriginReachesOtherSlug pins exclusion by source slug:
// a Claude-authored project memory renders into another slug in that project,
// but never back into the slug that holds its original.
func TestReconcileProjectClaudeOriginReachesOtherSlug(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	own := filepath.Join(claude, "projects", "-work-y", "memory")
	other := filepath.Join(claude, "projects", "-work-y-sub", "memory")
	writeFile(t, filepath.Join(own, "y-lesson.md"),
		"---\nname: y-lesson\ndescription: a y lesson\nmetadata:\n  type: lesson\n---\ny body\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n")
	c := []string{"--config", cfg, "--json"}
	defer silenceStdout(t)()

	if code := Run(append([]string{"reconcile", "--apply", "--cwd", "/work/y"}, c...)); code != exitOK {
		t.Fatalf("first reconcile exit = %d, want %d", code, exitOK)
	}
	if code := Run(append([]string{"share", "y-lesson", "--to", "project:y"}, c...)); code != exitOK {
		t.Fatalf("share exit = %d, want %d", code, exitOK)
	}
	if code := Run(append([]string{"reconcile", "--apply", "--cwd", "/work/y/sub"}, c...)); code != exitOK {
		t.Fatalf("reconcile from sub exit = %d, want %d", code, exitOK)
	}
	if !exists(filepath.Join(other, "y-lesson.md")) {
		t.Error("a Claude-authored project memory did not reach another slug of its project")
	}
	if code := Run(append([]string{"reconcile", "--apply", "--cwd", "/work/y"}, c...)); code != exitOK {
		t.Fatalf("reconcile from own slug exit = %d, want %d", code, exitOK)
	}
	entries, err := os.ReadDir(own)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "y-lesson.md" && e.Name() != "MEMORY.md" && strings.HasSuffix(e.Name(), ".md") {
			t.Errorf("own slug gained %s; a memory must not render back onto its original", e.Name())
		}
	}
}

// TestForgetPurgesShared pins forget's reach into the shared dir.
func TestForgetPurgesShared(t *testing.T) {
	claude, c := sharedFixture(t)
	defer silenceStdout(t)()
	if code := Run(append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)); code != exitOK {
		t.Fatalf("sync exit = %d, want %d", code, exitOK)
	}
	if code := Run(append([]string{"forget", "g-mem", "--apply"}, c...)); code != exitOK {
		t.Fatalf("forget exit = %d, want %d", code, exitOK)
	}
	if exists(filepath.Join(claude, "engram", "memory", "g-mem.md")) {
		t.Error("forget left the shared render")
	}
	if exists(filepath.Join(claude, "rules", "engram-memory.md")) {
		t.Error("forget of the last shared memory left the rules file importing a missing index")
	}
}

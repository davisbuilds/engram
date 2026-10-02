package cli

import (
	"path/filepath"
	"testing"

	"github.com/davisbuilds/engram/internal/slug"
)

// TestSyncRendersIntoTheRepositorySlug pins Claude Code's own rule: a project's
// auto memory lives in the main repository's slug, for a session in a
// subdirectory or a linked worktree alike. A sync from either renders there,
// with the repository's project memories in tier, and never into a slug named
// after the cwd, which Claude Code would not read.
func TestSyncRendersIntoTheRepositorySlug(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	gitdir := filepath.Join(repo, ".git", "worktrees", "wt")
	writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")
	writeFile(t, filepath.Join(repo, "sub", "f"), "")
	writeFile(t, filepath.Join(dir, "wt", ".git"), "gitdir: "+gitdir+"\n")
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	writeFile(t, filepath.Join(canon, "p-mem.md"), "---\nname: p-mem\ndescription: d\ntype: lesson\nscope: project:repo\n---\nproject\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n")
	repoSlug := filepath.Join(claude, "projects", slug.ForCwd(repo), "memory")

	for _, cwd := range []string{filepath.Join(repo, "sub"), filepath.Join(dir, "wt")} {
		func() {
			defer silenceStdout(t)()
			if code := Run([]string{"sync", "--apply", "--config", cfg, "--cwd", cwd, "--json"}); code != exitOK {
				t.Fatalf("sync from %s exit = %d, want %d", cwd, code, exitOK)
			}
		}()
		if exists(filepath.Join(claude, "projects", slug.ForCwd(cwd))) {
			t.Errorf("sync from %s wrote a slug named after the cwd", cwd)
		}
	}
	if !exists(filepath.Join(repoSlug, "p-mem.md")) {
		t.Error("project memory not rendered into the repository's slug")
	}
}

// TestListFromAWorktreeShowsTheRepositoryMemories pins list against sync: from
// a linked worktree, the repository's project memories are relevant.
func TestListFromAWorktreeShowsTheRepositoryMemories(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	gitdir := filepath.Join(repo, ".git", "worktrees", "wt")
	writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")
	writeFile(t, filepath.Join(dir, "wt-elsewhere", ".git"), "gitdir: "+gitdir+"\n")
	canon := filepath.Join(dir, "canonical")
	writeFile(t, filepath.Join(canon, "p-mem.md"), "---\nname: p-mem\ndescription: d\ntype: lesson\nscope: project:repo\n---\nproject\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+filepath.Join(dir, "claude")+"\n")
	code, env := runEnvelope(t, "list", "--config", cfg, "--cwd", filepath.Join(dir, "wt-elsewhere"))
	if code != exitOK {
		t.Fatalf("list exit = %d", code)
	}
	data, _ := env["data"].(map[string]any)
	mems, _ := data["memories"].([]any)
	if len(mems) != 1 {
		t.Errorf("list from a worktree = %v, want the repository's project memory", mems)
	}
}

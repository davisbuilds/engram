package gitroot

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// layout builds a main repo with a subdirectory, a linked worktree (as `git
// worktree add` lays it out), a submodule-style checkout, and a plain dir.
func layout(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, "main", ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(root, "main", "sub", "deep", "f"), "")
	write(t, filepath.Join(root, "main", ".git", "worktrees", "wt", "commondir"), "../..\n")
	write(t, filepath.Join(root, "wt", ".git"), "gitdir: "+filepath.Join(root, "main", ".git", "worktrees", "wt")+"\n")
	write(t, filepath.Join(root, "wt", "pkg", "f"), "")
	write(t, filepath.Join(root, "main", ".git", "modules", "lib", "HEAD"), "")
	write(t, filepath.Join(root, "main", "lib", ".git"), "gitdir: ../.git/modules/lib\n")
	write(t, filepath.Join(root, "plain", "f"), "")
	return root
}

func TestMainResolvesRepositories(t *testing.T) {
	root := layout(t)
	main := filepath.Join(root, "main")
	cases := []struct {
		name, path, want string
		ok               bool
	}{
		{"repo root", main, main, true},
		{"repo subdirectory", filepath.Join(main, "sub", "deep"), main, true},
		{"linked worktree", filepath.Join(root, "wt"), main, true},
		{"worktree subdirectory", filepath.Join(root, "wt", "pkg"), main, true},
		{"submodule is its own repo", filepath.Join(main, "lib"), filepath.Join(main, "lib"), true},
		{"not a repo", filepath.Join(root, "plain"), "", false},
	}
	for _, c := range cases {
		got, ok := Main(c.path)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Main(%s) = (%q, %v), want (%q, %v)", c.name, c.path, got, ok, c.want, c.ok)
		}
	}
}

// A relative gitdir in a worktree's .git file resolves against the worktree.
func TestMainRelativeWorktreeGitdir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main", ".git", "worktrees", "wt", "commondir"), "../..\n")
	write(t, filepath.Join(root, "wt", ".git"), "gitdir: ../main/.git/worktrees/wt\n")
	if got, ok := Main(filepath.Join(root, "wt")); !ok || got != filepath.Join(root, "main") {
		t.Errorf("Main = (%q, %v), want the main repo", got, ok)
	}
}

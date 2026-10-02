// Package gitroot finds the main repository a path belongs to. A linked
// worktree (`git worktree add`) belongs to the repository it was added from,
// so its root is that repository's, not the worktree directory's: Claude Code
// keys a project's auto memory by that root, and a project scope names it.
package gitroot

import (
	"os"
	"path/filepath"
	"strings"
)

// Main returns the root of the main repository containing path, and whether
// path is inside a repository at all. It walks up to the nearest .git: a
// directory marks a repository root; a file pointing at a linked worktree's
// gitdir resolves through that gitdir's commondir to the main repository; any
// other .git file (a submodule's) marks its own root.
func Main(path string) (string, bool) {
	d := filepath.Clean(path)
	for {
		dotgit := filepath.Join(d, ".git")
		if fi, err := os.Stat(dotgit); err == nil {
			if fi.IsDir() {
				return d, true
			}
			if main, ok := worktreeMain(d, dotgit); ok {
				return main, true
			}
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// worktreeMain resolves a linked worktree's .git file to its main repository:
// the file names the worktree's gitdir, whose commondir names the main
// repository's .git directory.
func worktreeMain(dir, dotgit string) (string, bool) {
	data, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", false
	}
	gitdir = resolve(dir, strings.TrimSpace(gitdir))
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	commonDir := resolve(gitdir, strings.TrimSpace(string(common)))
	if filepath.Base(commonDir) != ".git" {
		return "", false
	}
	return filepath.Dir(commonDir), true
}

func resolve(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// Package testhome sandboxes the home directory for a package's tests.
//
// engram resolves harness homes (~/.claude, ~/.codex), the canonical root and
// its config file from HOME and XDG_CONFIG_HOME. A test that forgets to point one
// of them at a temp dir silently falls through to the user's real memory, where
// an applying sync deletes engram-owned notes as STALE. Every test package runs
// under Main, so the real home is unreachable, and any file a test writes into
// the sandbox fails the run: that write would have landed in the real home.
package testhome

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Main runs m with HOME and XDG_CONFIG_HOME pointed at an empty sandbox and
// exits non-zero if the tests leave any file in it.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	sandbox, err := os.MkdirTemp("", "engram-testhome-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(sandbox) }()

	home := filepath.Join(sandbox, "home")
	xdg := filepath.Join(sandbox, "xdg")
	for _, d := range []string{home, xdg} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "testhome:", err)
			return 1
		}
	}
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "testhome:", err)
			return 1
		}
	}

	code := m.Run()

	leaks, err := Leaks(sandbox)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		return 1
	}
	if len(leaks) > 0 {
		fmt.Fprintln(os.Stderr, "testhome: tests wrote under the default home; each write would have reached the real one:")
		for _, l := range leaks {
			fmt.Fprintln(os.Stderr, "  "+l)
		}
		return 1
	}
	return code
}

// Leaks lists the files under sandbox, relative to it, in sorted order.
func Leaks(sandbox string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(sandbox, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, rerr := filepath.Rel(sandbox, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

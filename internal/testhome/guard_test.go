package testhome

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// unsandboxed returns the directories under root, relative to it, that hold a
// _test.go file but no file calling testhome.Main.
func unsandboxed(root string) ([]string, error) {
	hasTests := map[string]bool{}
	guarded := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "testdata" || n == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		dir := filepath.Dir(p)
		hasTests[dir] = true
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		// testhome's own tests call Main unqualified.
		if strings.Contains(string(body), "testhome.Main(m)") || strings.Contains(string(body), "{ Main(m) }") {
			guarded[dir] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for dir := range hasTests {
		if !guarded[dir] {
			rel, rerr := filepath.Rel(root, dir)
			if rerr != nil {
				return nil, rerr
			}
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

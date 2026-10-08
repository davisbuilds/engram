package curate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/lock"
)

// fileState is one regular file's bytes and permission bits.
type fileState struct {
	data []byte
	perm fs.FileMode
}

// snapshot records every file a curate batch could change under the canonical
// root, keyed by path relative to it. A batch writes memory files and
// tombstones, and which paths it touches depends on the order its operations
// run in, so the snapshot takes every file rather than predicting them.
type snapshot map[string]fileState

// PartialApplyError reports a batch that failed and could not be rolled back:
// canonical is left partly applied, and Rollback names what was not restored.
type PartialApplyError struct {
	Err      error
	Rollback error
}

func (e *PartialApplyError) Error() string {
	return e.Err.Error() + "; rollback failed, canonical is partly applied: " + e.Rollback.Error()
}

func (e *PartialApplyError) Unwrap() error { return e.Err }

// tracked reports whether a file belongs to the store: the apply lock and
// engram's write temporaries do not (walkTracked skips Git's metadata).
func tracked(d fs.DirEntry) bool {
	if !d.Type().IsRegular() {
		return false
	}
	name := d.Name()
	temp := strings.HasPrefix(name, ".engram-") && strings.HasSuffix(name, ".tmp")
	return name != lock.Name && !temp
}

// walkTracked calls fn for each tracked file under root, skipping .git.
func walkTracked(root string, fn func(rel, path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !tracked(d) {
			return nil
		}
		return fn(rel, path, d)
	})
}

// takeSnapshot reads the files under root that a batch could change.
func takeSnapshot(root string) (snapshot, error) {
	s := snapshot{}
	err := walkTracked(root, func(rel, path string, d fs.DirEntry) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		s[rel] = fileState{data: data, perm: info.Mode().Perm()}
		return nil
	})
	return s, err
}

// restore puts root back to the snapshot: a file the batch created is removed,
// and one it changed or removed is written back with its bytes and mode. It
// keeps going past a failure and reports every path it could not restore.
func (s snapshot) restore(root string) error {
	var failed []string
	var errs []error
	fail := func(rel string, err error) {
		failed = append(failed, rel)
		errs = append(errs, fmt.Errorf("%s: %w", rel, err))
	}
	var created []string
	if err := walkTracked(root, func(rel, _ string, _ fs.DirEntry) error {
		if _, ok := s[rel]; !ok {
			created = append(created, rel)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("list canonical for rollback: %w", err)
	}
	for _, rel := range created {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			fail(rel, err)
		}
	}
	rels := make([]string, 0, len(s))
	for rel := range s {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := s[rel].writeBack(filepath.Join(root, rel)); err != nil {
			fail(rel, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("not restored: %s: %w", strings.Join(failed, ", "), errors.Join(errs...))
}

// writeBack makes path hold f, unless it already does.
func (f fileState) writeBack(path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm() == f.perm {
		if cur, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(cur, f.data) {
			return nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".engram-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(f.data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, f.perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Package tombstone records deliberately forgotten canonical memories, so a
// removal stays removed: import consults the tombstones and never re-creates a
// memory an operator (or curate) chose to forget while its native source lives
// on. Each tombstone keeps the forgotten file's full text, so forgetting is
// reversible.
package tombstone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

// Tombstone is the record of one forgotten canonical memory.
type Tombstone struct {
	Name        string `yaml:"name" json:"name"`
	Origin      string `yaml:"origin,omitempty" json:"origin,omitempty"`
	Source      string `yaml:"source,omitempty" json:"source,omitempty"`
	ForgottenAt string `yaml:"forgotten_at" json:"forgotten_at"`
	Reason      string `yaml:"reason,omitempty" json:"reason,omitempty"`
	Successor   string `yaml:"successor,omitempty" json:"successor,omitempty"`
	// Path is the forgotten file's path relative to the canonical root, where
	// Restore writes it back.
	Path string `yaml:"path" json:"path"`
	// Memory is the forgotten file's full text, byte for byte.
	Memory string `yaml:"memory" json:"-"`
	// file is the record's own path, set by read; it orders a name's records.
	file string
}

// Note is the operator's account of why a memory was forgotten.
type Note struct {
	Reason    string
	Successor string
}

// Set is every tombstone under a canonical root, keyed by memory name, most
// recent first. A name forgotten more than once (reused by another harness in
// between) keeps every record, so each earlier origin stays blocked.
type Set map[string][]Tombstone

var (
	// ErrNotFound reports that no tombstone exists for a name.
	ErrNotFound = errors.New("no tombstone")
	// ErrTaken reports that a restore target is occupied again.
	ErrTaken = errors.New("name is taken")
	// ErrNoMemory reports that no canonical memory has the name to forget.
	ErrNoMemory = errors.New("no canonical memory")
)

// Path returns where the tombstone for name lives under root.
func Path(root, name string) string {
	return filepath.Join(root, discover.ForgottenDir, name+".yaml")
}

// Forget tombstones the canonical memory name, then removes its file. The
// tombstone is written first, so a failure never loses the memory. The caller
// holds the canonical lock.
func Forget(root, name string, note Note, now time.Time) (Tombstone, error) {
	m, path, found, err := store.Load(root, name)
	if err != nil {
		return Tombstone{}, err
	}
	if !found {
		return Tombstone{}, fmt.Errorf("%w named %s", ErrNoMemory, name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Tombstone{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Tombstone{}, err
	}
	ts := Tombstone{
		Name: name, Origin: m.Provenance.Origin, Source: m.Provenance.Source,
		ForgottenAt: now.UTC().Format(time.RFC3339),
		Reason:      note.Reason, Successor: note.Successor,
		Path: filepath.ToSlash(rel), Memory: string(data),
	}
	out, err := yaml.Marshal(ts)
	if err != nil {
		return Tombstone{}, err
	}
	if err := archive(root, name); err != nil {
		return Tombstone{}, err
	}
	if err := writeAtomic(Path(root, name), out); err != nil {
		return Tombstone{}, err
	}
	if err := os.Remove(path); err != nil {
		return Tombstone{}, err
	}
	return ts, nil
}

// Restore writes a forgotten memory back to its original path and removes its
// tombstone, returning the restored path. A name or path taken again since is
// refused (ErrTaken) and the tombstone kept. The caller holds the canonical lock.
func Restore(root, name string) (string, error) {
	if !schema.ValidName(name) {
		return "", fmt.Errorf("invalid memory name %q", name)
	}
	ts, err := read(Path(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w for %s", ErrNotFound, name)
	}
	if err != nil {
		return "", err
	}
	_, taken, found, err := store.Load(root, name)
	if errors.Is(err, store.ErrWithheld) || found {
		return "", fmt.Errorf("%w: %s (%s)", ErrTaken, name, taken)
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, filepath.FromSlash(ts.Path))
	if !filepath.IsLocal(filepath.FromSlash(ts.Path)) || !contained(root, filepath.Dir(path)) {
		return "", fmt.Errorf("tombstone for %s records a path outside the canonical root: %q", name, ts.Path)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%w: %s is occupied", ErrTaken, path)
	}
	if err := writeAtomic(path, []byte(ts.Memory)); err != nil {
		return "", err
	}
	if err := os.Remove(Path(root, name)); err != nil {
		return path, err
	}
	// The newest earlier record, if any, becomes the latest: it keeps blocking
	// its origin and is what a later restore would bring back.
	if n := archives(root, name); n > 0 {
		if err := os.Rename(archivePath(root, name, n), Path(root, name)); err != nil {
			return path, err
		}
	}
	return path, nil
}

// archivePath is where the i-th earlier record for name is kept.
func archivePath(root, name string, i int) string {
	return filepath.Join(root, discover.ForgottenDir, fmt.Sprintf("%s.%d.yaml", name, i))
}

// archives returns how many earlier records name has (they are numbered 1..n).
func archives(root, name string) int {
	n := 0
	for {
		if _, err := os.Lstat(archivePath(root, name, n+1)); err != nil {
			return n
		}
		n++
	}
}

// archive moves an existing latest record for name aside, so a second forget
// of the name adds a record instead of replacing one.
func archive(root, name string) error {
	if _, err := os.Lstat(Path(root, name)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return os.Rename(Path(root, name), archivePath(root, name, archives(root, name)+1))
}

// contained reports whether dir, once symlinks in its existing part are
// resolved, lies inside root, so a restore cannot write through a symlink.
func contained(root, dir string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	existing := dir
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return false
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, real)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// Load reads every tombstone under root. A root without tombstones yields an
// empty set.
func Load(root string) (Set, error) {
	set := Set{}
	dir := filepath.Join(root, discover.ForgottenDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return set, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		ts, err := read(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		set[ts.Name] = append(set[ts.Name], ts)
	}
	for name, list := range set {
		sort.SliceStable(list, func(i, j int) bool { return recency(root, name, list[i]) > recency(root, name, list[j]) })
	}
	return set, nil
}

// Blocks reports whether an import candidate is a memory that was forgotten:
// same name, and imported from the same harness as the forgotten one. A
// different harness reusing the name is a different memory and imports.
func (s Set) Blocks(m *schema.CanonicalMemory) bool {
	for _, ts := range s[m.Name] {
		if family(ts.Origin) == family(m.Provenance.Origin) {
			return true
		}
	}
	return false
}

// Latest returns the most recent tombstone for name.
func (s Set) Latest(name string) (Tombstone, bool) {
	if list := s[name]; len(list) > 0 {
		return list[0], true
	}
	return Tombstone{}, false
}

// family reduces an origin to the harness it names (import:claude-code and its
// no-frontmatter variant are one family), looking through a detach.
func family(origin string) string {
	origin = strings.TrimPrefix(origin, schema.DetachedPrefix)
	for _, h := range []string{"import:claude-code", "import:codex"} {
		if origin == h || strings.HasPrefix(origin, h+":") {
			return h
		}
	}
	return origin
}

// recency orders a name's records: the latest (at Path) above every archive,
// and a higher-numbered archive above a lower one. Records are told apart by
// the file each was read from, which read stores in file.
func recency(root, name string, ts Tombstone) int {
	if ts.file == Path(root, name) {
		return 1 << 30
	}
	var i int
	if _, err := fmt.Sscanf(filepath.Base(ts.file), name+".%d.yaml", &i); err == nil {
		return i
	}
	return 0
}

func read(path string) (Tombstone, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Tombstone{}, err
	}
	var ts Tombstone
	if err := yaml.Unmarshal(data, &ts); err != nil {
		return Tombstone{}, fmt.Errorf("tombstone %s: %w", path, err)
	}
	if ts.Name == "" {
		return Tombstone{}, fmt.Errorf("tombstone %s: no name", path)
	}
	ts.file = path
	return ts, nil
}

// writeAtomic writes data via a temp file and rename, creating the directory as
// needed, so a crash leaves the target either fully prior or fully new.
func writeAtomic(path string, data []byte) error {
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
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Package sync reconciles canonical memories against a harness's on-disk memory
// store. It owns the filesystem, the marker-based ownership checks, atomic
// writes, an exclusive apply lock, and the four action kinds — so the renderers
// can stay pure. Two invariants are load-bearing: apply is idempotent (a second
// apply on unchanged canonical is a no-op), and a file without an engram marker
// is never modified or deleted.
package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

// ActionKind is one of the reconciliation outcomes for a single memory.
type ActionKind string

const (
	// Create renders a memory that has no target file yet.
	Create ActionKind = "CREATE"
	// Update rewrites an engram-owned target whose content or index line drifted.
	Update ActionKind = "UPDATE"
	// Stale removes an engram-owned target whose canonical no longer renders here.
	Stale ActionKind = "STALE"
	// Conflict flags a target name occupied by a hand-authored (unmarked) file;
	// apply never touches it.
	Conflict ActionKind = "CONFLICT"
)

// Action is one planned change to the harness store.
type Action struct {
	Kind ActionKind `json:"kind"`
	Name string     `json:"name"`
	Path string     `json:"path"`
	Note string     `json:"note,omitempty"`
}

// Result reports what apply did.
type Result struct {
	Applied   []Action `json:"applied"`
	Conflicts []Action `json:"conflicts"`
}

// MarshalJSON encodes empty action lists as [] rather than null, so a consumer
// can iterate either list without a null check.
func (r Result) MarshalJSON() ([]byte, error) {
	type plain Result
	if r.Applied == nil {
		r.Applied = []Action{}
	}
	if r.Conflicts == nil {
		r.Conflicts = []Action{}
	}
	return json.Marshal(plain(r))
}

// Target is one harness's reconcilable memory store. Both harness targets share
// the same plan/apply contract so the caller can drive them uniformly.
type Target interface {
	// Harness names the target harness (e.g. "claude-code", "codex").
	Harness() string
	// DesiredMemories returns the scope-filtered memories that should render here.
	DesiredMemories() []*schema.CanonicalMemory
	// Plan computes the reconciliation actions without writing.
	Plan() ([]Action, error)
	// Apply reconciles under an exclusive lock and reports what it did.
	Apply() (Result, error)
}

// ClaudeTarget is a Claude Code per-project memory directory paired with the
// memories that should render into it (already scope-filtered by the caller).
type ClaudeTarget struct {
	MemoryDir string
	Desired   []*schema.CanonicalMemory
	// KeepStale holds back STALE removals because Desired may be incomplete (the
	// canonical root is missing or a canonical file failed to parse): an owned
	// render absent from Desired might still have a live canonical source.
	KeepStale bool
	// Stamp records in each render the hash of the content engram wrote
	// (metadata.engram_base), and holds a render edited since as a CONFLICT
	// rather than overwrite or remove it, so the edit can be imported.
	Stamp bool
	// Discard names edited renders to overwrite or remove anyway: the operator
	// chose canonical over the edit.
	Discard map[string]bool
}

// Harness identifies this target's harness.
func (ClaudeTarget) Harness() string { return "claude-code" }

// DesiredMemories returns the scope-filtered memories for this target.
func (t ClaudeTarget) DesiredMemories() []*schema.CanonicalMemory { return t.Desired }

// ownedFile is an engram-authored memory file already on disk.
type ownedFile struct {
	content []byte
	path    string
}

// Plan computes the actions needed to reconcile the target, without writing.
func (t ClaudeTarget) Plan() ([]Action, error) {
	owned, unmarked, err := scanMemoryDir(t.MemoryDir)
	if err != nil {
		return nil, err
	}
	renderer := render.ClaudeRenderer{}

	desired := map[string]bool{}
	var actions []Action
	for _, m := range t.Desired {
		desired[m.Name] = true
		rr, err := renderer.Render(m)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(t.MemoryDir, rr.FileName)

		if _, isUnmarked := unmarked[m.Name]; isUnmarked {
			actions = append(actions, Action{Conflict, m.Name, path, "target exists and is not engram-owned"})
			continue
		}
		cur, exists := owned[m.Name]
		if exists && t.held(m.Name, cur.content, m) {
			actions = append(actions, Action{Conflict, m.Name, path, HeldEditNote})
			continue
		}
		switch {
		case !exists && fileExists(path):
			// The name matched neither map, yet the path resolves to a file: a
			// case-folding filesystem is aliasing a hand-authored case variant.
			actions = append(actions, Action{Conflict, m.Name, path, "target exists (as a case variant) and is not engram-owned"})
		case !exists:
			actions = append(actions, Action{Create, m.Name, path, ""})
		default:
			// Desired content is derived from the existing file so unmanaged
			// frontmatter keys are preserved; a file already in that shape is a no-op.
			want, cerr := t.content(cur.content, m)
			if cerr != nil {
				return nil, cerr
			}
			if !bytes.Equal(cur.content, want) || !indexInSync(t.MemoryDir, m.Name, rr.IndexLine) {
				actions = append(actions, Action{Update, m.Name, path, ""})
			}
		}
	}
	for name, cur := range owned {
		switch {
		case desired[name]:
		case t.KeepStale && !t.Discard[name]:
			// Held while canonical may be incomplete, unless the operator named
			// this render for discarding.
		case t.held(name, cur.content, nil):
			actions = append(actions, Action{Conflict, name, cur.path, HeldEditNote + "; it no longer renders here"})
		default:
			actions = append(actions, Action{Stale, name, cur.path, "canonical no longer renders here"})
		}
	}
	sortActions(actions)
	return actions, nil
}

// Apply reconciles the target under an exclusive lock and reports what it did.
// Conflicts are surfaced, not applied.
func (t ClaudeTarget) Apply() (Result, error) {
	unlock, err := acquireLock(t.MemoryDir)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	return t.applyLocked()
}

// applyLocked is Apply for a caller already holding the memory dir's lock.
func (t ClaudeTarget) applyLocked() (Result, error) {
	actions, err := t.Plan()
	if err != nil {
		return Result{}, err
	}

	owned, _, err := scanMemoryDir(t.MemoryDir)
	if err != nil {
		return Result{}, err
	}
	renderer := render.ClaudeRenderer{}
	rendered := map[string]render.ClaudeRender{}
	byName := map[string]*schema.CanonicalMemory{}
	for _, m := range t.Desired {
		rr, err := renderer.Render(m)
		if err != nil {
			return Result{}, err
		}
		rendered[m.Name] = rr
		byName[m.Name] = m
	}

	var res Result
	for _, a := range actions {
		switch a.Kind {
		case Conflict:
			res.Conflicts = append(res.Conflicts, a)
		case Create, Update:
			// Merge onto the existing file (if any) so unmanaged frontmatter keys survive.
			content, cerr := t.content(owned[a.Name].content, byName[a.Name])
			if cerr != nil {
				return res, cerr
			}
			if err := atomicWrite(a.Path, content); err != nil {
				return res, err
			}
			if err := upsertIndexLine(t.MemoryDir, a.Name, rendered[a.Name].IndexLine); err != nil {
				return res, err
			}
			res.Applied = append(res.Applied, a)
		case Stale:
			if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return res, err
			}
			if err := removeIndexLine(t.MemoryDir, a.Name); err != nil {
				return res, err
			}
			res.Applied = append(res.Applied, a)
		}
	}
	return res, nil
}

// HeldEditNote opens the note of a CONFLICT that holds an edited render, so a
// caller can tell it from a hand-authored file blocking a render.
const HeldEditNote = "edited in place since engram rendered it"

// content is the file engram writes for m over existing: claudeContent, plus
// the base stamp on a stamping target.
func (t ClaudeTarget) content(existing []byte, m *schema.CanonicalMemory) ([]byte, error) {
	c, err := claudeContent(existing, m)
	if err != nil || !t.Stamp {
		return c, err
	}
	return stamp(c, schema.NativeHash(m))
}

// held reports whether a stamping target must leave an owned render alone: it
// was edited since engram wrote it, the edit is not what m (nil when m no
// longer renders here) already says, and the operator has not discarded it.
func (t ClaudeTarget) held(name string, content []byte, m *schema.CanonicalMemory) bool {
	if !t.Stamp {
		return false
	}
	e, edited := editedRender(content)
	if !edited || t.Discard[name] {
		return false
	}
	return m == nil || e.Hash() != schema.NativeHash(m)
}

// scanMemoryDir splits the memory dir into engram-owned files (by name) and
// hand-authored files (by name), ignoring the index and non-markdown entries.
func scanMemoryDir(dir string) (owned map[string]ownedFile, unmarked map[string]string, err error) {
	owned = map[string]ownedFile{}
	unmarked = map[string]string{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return owned, unmarked, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, nil, rerr
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if IsEngramOwned(content) {
			owned[name] = ownedFile{content: content, path: path}
		} else {
			unmarked[name] = path
		}
	}
	return owned, unmarked, nil
}

// IsEngramOwned reports whether a memory file carries engram's origin marker in
// its frontmatter. A file without it is hand-authored and off-limits.
func IsEngramOwned(content []byte) bool {
	front := frontmatterBytes(content)
	if front == nil {
		return false
	}
	var fm struct {
		Metadata struct {
			Origin string `yaml:"origin"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(front, &fm); err != nil {
		return false
	}
	return fm.Metadata.Origin == marker.Origin
}

// frontmatterBytes returns the YAML between the opening and closing --- fences,
// or nil when the content has no frontmatter.
func frontmatterBytes(content []byte) []byte {
	// Line endings and a BOM are cosmetic; an editor or git autocrlf changing
	// them must not flip an engram render to hand-authored.
	s := strings.ReplaceAll(strings.TrimPrefix(string(content), "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil
	}
	rest := s[len("---\n"):]
	i := strings.Index(rest, "\n---\n")
	if i < 0 {
		return nil
	}
	return []byte(rest[:i])
}

func indexInSync(dir, name, want string) bool {
	cur, ok := currentIndexLine(dir, name)
	return ok && cur == want
}

func currentIndexLine(dir, name string) (string, bool) {
	for _, ln := range readIndexLines(dir) {
		if n, ok := marker.ClaudeIndexName(ln); ok && n == name {
			return ln, true
		}
	}
	return "", false
}

func indexPath(dir string) string { return filepath.Join(dir, "MEMORY.md") }

func readIndexLines(dir string) []string {
	data, err := os.ReadFile(indexPath(dir))
	if err != nil {
		return nil
	}
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func writeIndexLines(dir string, lines []string) error {
	if len(lines) == 0 {
		if err := os.Remove(indexPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWrite(indexPath(dir), []byte(strings.Join(lines, "\n")+"\n"))
}

// upsertIndexLine replaces the engram line anchored to name, or appends it,
// leaving every foreign line untouched.
func upsertIndexLine(dir, name, line string) error {
	lines := readIndexLines(dir)
	for i, ln := range lines {
		if n, ok := marker.ClaudeIndexName(ln); ok && n == name {
			lines[i] = line
			return writeIndexLines(dir, ensureIndexHeader(lines))
		}
	}
	return writeIndexLines(dir, ensureIndexHeader(append(lines, line)))
}

// removeIndexLine drops the engram line anchored to name, leaving foreign lines.
func removeIndexLine(dir, name string) error {
	lines := readIndexLines(dir)
	out := lines[:0]
	for _, ln := range lines {
		if n, ok := marker.ClaudeIndexName(ln); ok && n == name {
			continue
		}
		out = append(out, ln)
	}
	return writeIndexLines(dir, ensureIndexHeader(out))
}

// ensureIndexHeader keeps the self-documenting engram header as the first line
// whenever the index carries at least one engram-managed entry, and drops it
// when none remain. It first strips any existing header so a reworded or
// misplaced one is not duplicated, then re-asserts a single header at the top;
// foreign lines keep their relative order. Idempotent: an index already in the
// desired shape round-trips unchanged, so a re-sync produces no header churn.
func ensureIndexHeader(lines []string) []string {
	out := make([]string, 0, len(lines)+1)
	hasEntry := false
	for _, ln := range lines {
		if marker.IsClaudeIndexHeader(ln) {
			continue
		}
		if _, ok := marker.ClaudeIndexName(ln); ok {
			hasEntry = true
		}
		out = append(out, ln)
	}
	if !hasEntry {
		return out
	}
	return append([]string{marker.ClaudeIndexHeader}, out...)
}

// atomicWrite writes data to a temp file in the same directory and renames it
// into place, so a crash leaves the target either fully prior or fully new.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".engram-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// acquireLock takes an exclusive apply lock on the memory dir via the shared
// lock package. A second live holder fails rather than interleaving, so no
// check-then-act window can double-write; a lock orphaned by a crash is
// reclaimed once stale.
func acquireLock(dir string) (func(), error) {
	return lock.Acquire(dir)
}

// sortActions orders actions deterministically by name then kind, so plans and
// results are stable across runs.
func sortActions(as []Action) {
	sort.Slice(as, func(i, j int) bool {
		if as[i].Name != as[j].Name {
			return as[i].Name < as[j].Name
		}
		return as[i].Kind < as[j].Kind
	})
}

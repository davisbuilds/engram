package sync

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

// MigrateActionKind classifies what migrate would do with one hand-authored
// (unmarked) file in a target slug.
type MigrateActionKind string

const (
	// Adopt: a canonical memory provably supersedes this file and their bodies are
	// identical, so engram takes ownership in place (name reconciled if normalized).
	Adopt MigrateActionKind = "ADOPT"
	// Diverged: a canonical memory matches this file, but the bodies differ — the
	// original or canonical changed since import. Left untouched; a curate/human call.
	Diverged MigrateActionKind = "DIVERGED"
	// Ambiguous: the match is not one-to-one (a file matches more than one memory,
	// or a memory is matched by more than one file). None of the set is adopted.
	Ambiguous MigrateActionKind = "AMBIGUOUS"
	// Skip: no canonical memory supersedes this hand-authored file; it is left alone.
	Skip MigrateActionKind = "SKIP"
)

// MigrateAction is one candidate's classification. Source is the hand-authored
// file's basename; Name is the canonical memory it maps to (empty for Skip).
type MigrateAction struct {
	Kind   MigrateActionKind `json:"kind"`
	Source string            `json:"source"`
	Name   string            `json:"name,omitempty"`
	Path   string            `json:"path"`
	Reason string            `json:"reason,omitempty"`
}

// MigrateResult reports an applied migration: what was adopted and what was left
// untouched (and why). Every candidate lands in exactly one slice.
type MigrateResult struct {
	Adopted   []MigrateAction `json:"adopted"`
	Diverged  []MigrateAction `json:"diverged"`
	Ambiguous []MigrateAction `json:"ambiguous"`
	Skipped   []MigrateAction `json:"skipped"`
}

// ClaudeMigrateTarget adopts hand-authored Claude memory files that canonical
// provably supersedes, converting them to engram-owned in place. Desired is the
// scope-filtered canonical set for this slug (the same set sync would render).
type ClaudeMigrateTarget struct {
	MemoryDir string
	Desired   []*schema.CanonicalMemory
}

// candidate is one unmarked file plus the canonical memories it matches.
type candidate struct {
	base    string
	path    string
	content []byte
	matches []string // canonical memory names this file corresponds to
}

// Plan classifies every hand-authored file in the memory dir without writing.
// Matching is deterministic (provenance source id, or slug-equality of the native
// name); body-identity then decides adopt vs diverged; non-one-to-one matches are
// ambiguous. It never consults content similarity — that is curate's job.
func (t ClaudeMigrateTarget) Plan() ([]MigrateAction, error) {
	_, unmarked, err := scanMemoryDir(t.MemoryDir)
	if err != nil {
		return nil, err
	}
	byName := map[string]*schema.CanonicalMemory{}
	for _, m := range t.Desired {
		byName[m.Name] = m
	}

	// Build the bidirectional match graph.
	cands := make(map[string]*candidate, len(unmarked))
	memMatches := map[string][]string{} // memory name -> matching file bases
	for base, path := range unmarked {
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		c := &candidate{base: base, path: path, content: content}
		importName := nativeImportName(content, base)
		for name, m := range byName {
			if matchesMemory(m, base, importName) {
				c.matches = append(c.matches, name)
				memMatches[name] = append(memMatches[name], base)
			}
		}
		sort.Strings(c.matches)
		cands[base] = c
	}

	var actions []MigrateAction
	for base, c := range cands {
		switch {
		case len(c.matches) == 0:
			actions = append(actions, MigrateAction{Kind: Skip, Source: base, Path: c.path, Reason: "no canonical memory supersedes this file"})
		case len(c.matches) > 1:
			actions = append(actions, MigrateAction{Kind: Ambiguous, Source: base, Path: c.path, Reason: "file matches multiple canonical memories: " + strings.Join(c.matches, ", ")})
		default:
			name := c.matches[0]
			if len(memMatches[name]) > 1 {
				others := append([]string(nil), memMatches[name]...)
				sort.Strings(others)
				actions = append(actions, MigrateAction{Kind: Ambiguous, Source: base, Path: c.path, Name: name, Reason: "canonical " + name + " is matched by multiple files: " + strings.Join(others, ", ")})
				continue
			}
			if nativeBody(c.content) == byName[name].Body {
				actions = append(actions, MigrateAction{Kind: Adopt, Source: base, Path: c.path, Name: name})
			} else {
				actions = append(actions, MigrateAction{Kind: Diverged, Source: base, Path: c.path, Name: name, Reason: "body differs from canonical; adopting would overwrite hand-authored edits"})
			}
		}
	}
	sortMigrateActions(actions)
	return actions, nil
}

// Apply reconciles the target under an exclusive lock, adopting only ADOPT-classified
// files. Diverged, ambiguous, and skipped files are reported and left byte-for-byte
// untouched. Adoption is idempotent: a second Apply on a migrated dir adopts nothing.
func (t ClaudeMigrateTarget) Apply() (MigrateResult, error) {
	unlock, err := acquireLock(t.MemoryDir)
	if err != nil {
		return MigrateResult{}, err
	}
	defer unlock()

	actions, err := t.Plan()
	if err != nil {
		return MigrateResult{}, err
	}
	byName := map[string]*schema.CanonicalMemory{}
	for _, m := range t.Desired {
		byName[m.Name] = m
	}
	renderer := render.ClaudeRenderer{}

	var res MigrateResult
	for _, a := range actions {
		switch a.Kind {
		case Adopt:
			rr, rerr := renderer.Render(byName[a.Name])
			if rerr != nil {
				return res, rerr
			}
			newPath := filepath.Join(t.MemoryDir, rr.FileName)
			if werr := atomicWrite(newPath, rr.Content); werr != nil {
				return res, werr
			}
			// If the canonical name normalized away from the original filename, the
			// old-named file and its (unmarked) index line must go, or the lesson
			// duplicates. removeUnmarkedIndexLine handles the exact-name case too:
			// the pre-adoption index line for this name is hand-authored, and the
			// marked-only removeIndexLine would leave it in place.
			if a.Source != a.Name {
				if rmErr := os.Remove(a.Path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
					return res, rmErr
				}
			}
			if ierr := removeUnmarkedIndexLine(t.MemoryDir, a.Source); ierr != nil {
				return res, ierr
			}
			if ierr := upsertIndexLine(t.MemoryDir, a.Name, rr.IndexLine); ierr != nil {
				return res, ierr
			}
			res.Adopted = append(res.Adopted, a)
		case Diverged:
			res.Diverged = append(res.Diverged, a)
		case Ambiguous:
			res.Ambiguous = append(res.Ambiguous, a)
		case Skip:
			res.Skipped = append(res.Skipped, a)
		}
	}
	return res, nil
}

// matchesMemory reports whether a hand-authored file corresponds to canonical
// memory m by either deterministic tier: recorded import provenance (the source
// basename), or slug-equality (the file's native name normalizes to m's name).
func matchesMemory(m *schema.CanonicalMemory, fileBase, importName string) bool {
	if m.Provenance.Source != "" && m.Provenance.Source == fileBase+".md" {
		return true
	}
	return m.Name == importName
}

// nativeImportName computes the canonical name a hand-authored file would import
// to, mirroring ImportClaude exactly: the slugified frontmatter name, falling back
// to the slugified filename. This is the slug-equality matching tier.
func nativeImportName(content []byte, fileBase string) string {
	if front := frontmatterBytes(content); front != nil {
		var n struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(front, &n); err == nil {
			if s := importer.Slugify(n.Name); s != "" {
				return s
			}
		}
	}
	return importer.Slugify(fileBase)
}

// nativeBody returns the markdown body of a native file — the text after the
// frontmatter, or the whole file when there is none — for body-identity comparison.
func nativeBody(content []byte) string {
	s := string(content)
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	i := strings.Index(rest, "\n---\n")
	if i < 0 {
		return s
	}
	return rest[i+len("\n---\n"):]
}

// removeUnmarkedIndexLine drops a hand-authored MEMORY.md line pointing at
// <base>.md, leaving engram-marked lines untouched. Adoption needs this because
// the pre-adoption line for a candidate carries no marker, so the marked-only
// removeIndexLine cannot see it.
func removeUnmarkedIndexLine(dir, base string) error {
	target := "](" + base + ".md)"
	lines := readIndexLines(dir)
	out := lines[:0]
	for _, ln := range lines {
		if _, marked := marker.ClaudeIndexName(ln); !marked && strings.Contains(ln, target) {
			continue
		}
		out = append(out, ln)
	}
	return writeIndexLines(dir, out)
}

func sortMigrateActions(a []MigrateAction) {
	sort.Slice(a, func(i, j int) bool {
		if a[i].Kind != a[j].Kind {
			return a[i].Kind < a[j].Kind
		}
		return a[i].Source < a[j].Source
	})
}

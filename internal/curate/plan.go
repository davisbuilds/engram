package curate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// A saved plan file says what it is, so a file of another kind, or a plan from
// a later engram whose format this one does not know, is refused rather than
// read as far as it happens to decode.
const (
	PlanKind    = "engram-curate-plan"
	PlanVersion = 1
)

// Plan is a validated curate batch saved by a dry run for review, and applied
// later exactly as saved, without running an agent. It is bound to the store it
// was made against: the canonical root, and the bytes of every file in it.
type Plan struct {
	Kind          string    `json:"kind"`
	Version       int       `json:"version"`
	CanonicalRoot string    `json:"canonical_root"`
	Corpus        *Binding  `json:"corpus"`
	Proposer      *Proposer `json:"proposer,omitempty"`
	// Operations are the validated operations as Apply performs them (see
	// Validate), so the reviewed memories are the memories written.
	Operations []Operation `json:"operations"`
}

// Binding fingerprints a canonical store: each file Apply's rollback covers
// (memories, tombstones, anything else under the root but Git's metadata, the
// lock and write temporaries), keyed by slash-separated path relative to the
// root. Raw bytes are hashed, so an edit that leaves a parsed memory unchanged
// (a comment, a key the schema drops) still counts: applying over it would
// discard it. A symlink is bound by its target and, when that is a file, the
// file's bytes; a symlink to a directory by its target alone, since neither
// discovery nor rollback looks inside.
type Binding struct {
	Files map[string]string `json:"files"`
}

// Proposer records which agent proposed the plan. It is informational; apply
// runs no agent.
type Proposer struct {
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

// ErrRootMismatch reports a plan made for another canonical root.
var ErrRootMismatch = errors.New("plan was made for another canonical root")

// StaleError reports a store that changed since its plan was made. Drift lists
// each path, relative to the root, that was added, removed or changed.
type StaleError struct {
	Drift []string
}

func (e *StaleError) Error() string {
	shown := e.Drift
	more := ""
	if len(shown) > 5 {
		shown, more = shown[:5], fmt.Sprintf(" and %d more", len(e.Drift)-5)
	}
	return fmt.Sprintf("canonical changed since the plan was made (%s%s); save and review a new plan", strings.Join(shown, ", "), more)
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Fingerprint binds the store at root as it is now. A missing root, or a root
// link that dangles, is an empty store, as discovery treats it. A file that
// cannot be read fails the fingerprint rather than being left out of it.
func Fingerprint(root string) (Binding, error) {
	b := Binding{Files: map[string]string{}}
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	err := walkTracked(root, func(rel, p string, d fs.DirEntry) error {
		h := sha256.New()
		content := true
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			info, err := os.Stat(p)
			if err != nil {
				return err
			}
			if info.IsDir() {
				// Discovery does not descend into a linked directory, and
				// rollback restores the link, not what it points at, so
				// the link is all the state there is.
				h.Write([]byte("dirlink\x00" + target))
				content = false
			} else {
				// Discovery reads a memory through its link, so both the
				// link and what it points at are state.
				h.Write([]byte("link\x00" + target + "\x00"))
			}
		} else {
			h.Write([]byte("file\x00"))
		}
		if content {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		b.Files[filepath.ToSlash(rel)] = "sha256:" + hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return b, err
}

// Drift lists, sorted, each path whose state differs between b and now.
func (b Binding) Drift(now Binding) []string {
	var out []string
	for p, d := range b.Files {
		if now.Files[p] != d {
			out = append(out, p)
		}
	}
	for p := range now.Files {
		if _, ok := b.Files[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// NewPlan builds the plan for ops, validated against the store at root while
// it held exactly binding.
func NewPlan(root string, binding Binding, proposer *Proposer, ops []Operation) (*Plan, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if ops == nil {
		ops = []Operation{}
	}
	return &Plan{
		Kind: PlanKind, Version: PlanVersion, CanonicalRoot: abs,
		Corpus: &binding, Proposer: proposer, Operations: ops,
	}, nil
}

// Marshal renders the plan file: indented JSON, memory bodies unescaped so a
// reviewer reads them as written, and a final newline.
func (p *Plan) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParsePlan reads a plan file strictly. Exactly one JSON object is accepted:
// no unknown field at any depth, no key given twice in one object (readers
// disagree on which copy wins, so the reviewed and the applied plan could
// differ), and nothing after it. The kind and version must be this engram's,
// and every required part must be present.
func ParsePlan(data []byte) (*Plan, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Plan
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("decode plan: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("plan file has data after the plan object")
	}
	switch {
	case p.Kind != PlanKind:
		return nil, fmt.Errorf("not a curate plan: kind is %q, want %q", p.Kind, PlanKind)
	case p.Version != PlanVersion:
		return nil, fmt.Errorf("unsupported plan version %d (this engram reads version %d)", p.Version, PlanVersion)
	case p.CanonicalRoot == "" || !filepath.IsAbs(p.CanonicalRoot):
		return nil, errors.New("plan canonical_root must be an absolute path")
	case p.Corpus == nil || p.Corpus.Files == nil:
		return nil, errors.New("plan has no corpus binding (corpus.files)")
	case p.Operations == nil:
		return nil, errors.New("plan has no operations array")
	}
	for rel, d := range p.Corpus.Files {
		if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, fmt.Errorf("plan corpus path %q is not a clean relative path", rel)
		}
		if !digestPattern.MatchString(d) {
			return nil, fmt.Errorf("plan corpus digest for %q is malformed", rel)
		}
	}
	return &p, nil
}

// Check reports whether the plan still applies to the store at root: the same
// root (ErrRootMismatch if not) holding the same files (a *StaleError if not).
func (p *Plan) Check(root string) error {
	if !sameRoot(p.CanonicalRoot, root) {
		return fmt.Errorf("%w: the plan names %s, the config %s", ErrRootMismatch, p.CanonicalRoot, root)
	}
	now, err := Fingerprint(root)
	if err != nil {
		return fmt.Errorf("fingerprint canonical: %w", err)
	}
	if drift := p.Corpus.Drift(now); len(drift) > 0 {
		return &StaleError{Drift: drift}
	}
	return nil
}

// ValidatePlan is Validate for a saved plan, whose operations must already be
// in the form Apply performs them. A memory whose provenance differs from the
// one Apply would keep is not the memory that would be written, so it is
// invalid rather than silently rewritten.
func ValidatePlan(ops []Operation, corpus []*schema.CanonicalMemory) []OpResult {
	results := Validate(ops, corpus)
	for i, r := range results {
		if r.Valid && ops[i].Memory != nil && r.Op.Memory.Provenance != ops[i].Memory.Provenance {
			results[i] = OpResult{
				Op:    ops[i],
				Error: fmt.Sprintf("memory %q carries provenance other than the stored memory's, which apply keeps", ops[i].Memory.Name),
			}
		}
	}
	return results
}

// sameRoot reports whether two spellings name one canonical root. Both are made
// absolute before their links are resolved, so a relative spelling resolves to
// the same path as an absolute one; a missing root matches only its own path.
func sameRoot(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	ba, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if aa == ba {
		return true
	}
	ar, err1 := filepath.EvalSymlinks(aa)
	br, err2 := filepath.EvalSymlinks(ba)
	return err1 == nil && err2 == nil && ar == br
}

// checkDuplicateKeys walks data as JSON tokens and refuses an object that
// repeats a key. Struct fields decode case-insensitively, so keys differing
// only in case are repeats too, except among corpus.files, whose keys are
// paths compared exactly.
func checkDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func(parent string) error
	walk = func(parent string) error {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("decode plan: %w", err)
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return fmt.Errorf("decode plan: %w", err)
				}
				key, _ := kt.(string)
				norm := key
				if parent != "files" {
					norm = strings.ToLower(key)
				}
				if seen[norm] {
					return fmt.Errorf("plan repeats the key %q in one object", key)
				}
				seen[norm] = true
				if err := walk(key); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(parent); err != nil {
					return err
				}
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("decode plan: %w", err)
		}
		return nil
	}
	return walk("")
}

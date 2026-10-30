package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
)

type storedVolume struct {
	Files int `json:"files"`
	Bytes int `json:"bytes"`
	Words int `json:"words"`
}

func (v *storedVolume) add(bytes, words int) {
	v.Files++
	v.Bytes += bytes
	v.Words += words
}

type scopeVolume struct {
	Scope string `json:"scope"`
	storedVolume
	BodyBytes int `json:"body_bytes"`
	BodyWords int `json:"body_words"`
}

type locationVolume struct {
	Harness  string `json:"harness"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	ScopeCwd string `json:"scope_cwd,omitempty"`
	storedVolume
}

type statsSource struct {
	Harness          string `json:"harness"`
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	Location         string `json:"location"`
	Name             string `json:"name,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Bytes            int    `json:"bytes"`
	Words            int    `json:"words"`
	ModifiedAt       string `json:"modified_at"`
	DeclaredCreated  string `json:"declared_created,omitempty"`
	DeclaredModified string `json:"declared_modified,omitempty"`
}

type repeatedRenderVolume struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
	storedVolume
}

type duplicateRenderVolume struct {
	SHA256      string   `json:"sha256"`
	Paths       []string `json:"paths"`
	ExtraCopies int      `json:"extra_copies"`
	ExtraBytes  int      `json:"extra_bytes"`
	ExtraWords  int      `json:"extra_words"`
}

type referenceIssue struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	State  string `json:"state"`
}

type inventoryStats struct {
	Complete             bool                    `json:"complete"`
	WordUnit             string                  `json:"word_unit"`
	Total                storedVolume            `json:"total"`
	Sources              []statsSource           `json:"sources"`
	CanonicalScopes      []scopeVolume           `json:"canonical_scopes"`
	Locations            []locationVolume        `json:"locations"`
	RepeatedRenders      []repeatedRenderVolume  `json:"repeated_renders"`
	ExactDuplicates      []duplicateRenderVolume `json:"exact_duplicate_renders"`
	ReferenceForms       []string                `json:"reference_forms"`
	ReferencesChecked    int                     `json:"references_checked"`
	UnresolvedReferences []referenceIssue        `json:"unresolved_references"`
	Diagnostics          []inventoryDiagnostic   `json:"diagnostics"`
	seenReferences       map[referenceIssue]bool
}

type statsJob struct {
	statsSource
	memory *schema.CanonicalMemory
	render bool
}

func (s *inventoryStats) issue(path string, err error) {
	s.Complete = false
	s.Diagnostics = append(s.Diagnostics, inventoryDiagnostic{path, err.Error()})
}

// Collect the actual stored file text, not a renderer's reconstruction. Physical
// paths count separately, including symlink aliases; this is text volume, not
// allocated disk space or an atomic cross-file snapshot.
func collectStats(root string, located []discover.Located, perrs []discover.ParseError, canonicalComplete bool,
	harnesses []*harnessInventory, contexts map[string]string,
) *inventoryStats {
	s := &inventoryStats{
		Complete: canonicalComplete, WordUnit: "unicode_whitespace_fields",
		Sources: []statsSource{}, CanonicalScopes: []scopeVolume{}, Locations: []locationVolume{},
		RepeatedRenders: []repeatedRenderVolume{}, ExactDuplicates: []duplicateRenderVolume{},
		UnresolvedReferences: []referenceIssue{}, Diagnostics: []inventoryDiagnostic{}, seenReferences: map[referenceIssue]bool{},
		ReferenceForms: []string{"canonical_related", "claude_index_file", "codex_rollout_file"},
	}
	jobs := map[string]statsJob{}
	names := map[string]bool{}
	for _, l := range located {
		m := l.Memory
		names[m.Name] = true
		jobs[l.Path] = statsJob{statsSource: statsSource{
			Harness: "canonical", Kind: "canonical_memory", Path: l.Path, Location: root,
			Name: m.Name, Scope: m.Scope, DeclaredCreated: m.Provenance.Created, DeclaredModified: m.Provenance.Modified,
		}, memory: m}
	}
	for _, p := range perrs {
		jobs[p.Path] = statsJob{statsSource: statsSource{Harness: "canonical", Kind: "canonical_unparsed", Path: p.Path, Location: root}}
	}
	locationKinds := map[string]string{root: "canonical"}
	for _, h := range harnesses {
		s.Complete = s.Complete && h.Complete
		renders, natives := map[string]string{}, map[string]string{}
		for name, paths := range h.renders {
			for _, path := range paths {
				renders[path] = name
			}
		}
		for name, paths := range h.native {
			for _, path := range paths {
				natives[path] = name
			}
		}
		for _, loc := range h.Locations {
			if loc.Kind != "claude_memory" && loc.Kind != "codex_notes" && loc.Kind != "codex_consolidated" {
				continue
			}
			if _, seen := locationKinds[loc.Path]; !seen {
				locationKinds[loc.Path] = loc.Kind
			}
			if loc.State == "missing" {
				continue
			}
			paths := []string{loc.Path}
			if loc.Kind != "codex_consolidated" {
				entries, err := os.ReadDir(loc.Path)
				if err != nil {
					s.issue(loc.Path, err)
					continue
				}
				paths = nil
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
						paths = append(paths, filepath.Join(loc.Path, entry.Name()))
					}
				}
			}
			for _, path := range paths {
				j := statsJob{statsSource: statsSource{Harness: h.Harness, Kind: loc.Kind, Path: path, Location: loc.Path}}
				switch {
				case renders[path] != "":
					j.Kind, j.Name, j.render = "managed_render", renders[path], true
				case loc.Kind == "claude_memory" && filepath.Base(path) == "MEMORY.md":
					j.Kind = "claude_index"
				case natives[path] != "":
					j.Kind, j.Name = "native_memory", natives[path]
				}
				if _, seen := jobs[path]; !seen {
					jobs[path] = j
				}
			}
		}
	}
	paths := make([]string, 0, len(jobs))
	for path := range jobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	scopes, locations := map[string]*scopeVolume{}, map[string]*locationVolume{}
	repeats := map[string]*repeatedRenderVolume{}
	duplicates := map[string][]statsSource{}
	for _, path := range paths {
		j := jobs[path]
		data, modified, err := readStatsFile(path)
		if err != nil {
			s.issue(path, err)
			continue
		}
		j.Bytes, j.Words, j.ModifiedAt = len(data), len(strings.Fields(string(data))), modified
		s.Sources = append(s.Sources, j.statsSource)
		s.Total.add(j.Bytes, j.Words)
		if locations[j.Location] == nil {
			locations[j.Location] = &locationVolume{Harness: j.Harness, Path: j.Location, Kind: locationKinds[j.Location], ScopeCwd: contexts[j.Location]}
		}
		locations[j.Location].add(j.Bytes, j.Words)
		if m := j.memory; m != nil {
			if scopes[m.Scope] == nil {
				scopes[m.Scope] = &scopeVolume{Scope: m.Scope}
			}
			v := scopes[m.Scope]
			v.add(j.Bytes, j.Words)
			v.BodyBytes += len(m.Body)
			v.BodyWords += len(strings.Fields(m.Body))
			for _, target := range m.Related {
				state := "present"
				if !names[target] {
					state = "missing"
					if !canonicalComplete {
						state = "unknown_incomplete_canonical"
					}
				}
				s.reference(referenceIssue{path, "canonical_related", target, state})
			}
		}
		if j.render {
			if repeats[j.Name] == nil {
				repeats[j.Name] = &repeatedRenderVolume{Name: j.Name, Paths: []string{}}
			}
			r := repeats[j.Name]
			r.Paths = append(r.Paths, path)
			r.add(j.Bytes, j.Words)
			hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
			duplicates[hash] = append(duplicates[hash], j.statsSource)
		}
		s.fileReferences(j, string(data))
	}
	for _, v := range scopes {
		s.CanonicalScopes = append(s.CanonicalScopes, *v)
	}
	for _, v := range locations {
		s.Locations = append(s.Locations, *v)
	}
	for _, v := range repeats {
		if v.Files > 1 {
			s.RepeatedRenders = append(s.RepeatedRenders, *v)
		}
	}
	for hash, copies := range duplicates {
		if len(copies) < 2 {
			continue
		}
		d := duplicateRenderVolume{SHA256: hash, Paths: []string{}, ExtraCopies: len(copies) - 1}
		for _, copy := range copies {
			d.Paths = append(d.Paths, copy.Path)
		}
		d.ExtraBytes, d.ExtraWords = d.ExtraCopies*copies[0].Bytes, d.ExtraCopies*copies[0].Words
		s.ExactDuplicates = append(s.ExactDuplicates, d)
	}
	sort.Slice(s.CanonicalScopes, func(i, j int) bool { return s.CanonicalScopes[i].Scope < s.CanonicalScopes[j].Scope })
	sort.Slice(s.Locations, func(i, j int) bool { return s.Locations[i].Path < s.Locations[j].Path })
	sort.Slice(s.RepeatedRenders, func(i, j int) bool { return s.RepeatedRenders[i].Name < s.RepeatedRenders[j].Name })
	sort.Slice(s.ExactDuplicates, func(i, j int) bool { return s.ExactDuplicates[i].Paths[0] < s.ExactDuplicates[j].Paths[0] })
	return s
}

func readStatsFile(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	data, readErr := io.ReadAll(f)
	info, statErr := f.Stat()
	closeErr := f.Close()
	if err := errors.Join(readErr, statErr, closeErr); err != nil {
		return nil, "", err
	}
	return data, info.ModTime().UTC().Format(time.RFC3339Nano), nil
}

func (s *inventoryStats) reference(r referenceIssue) {
	key := r
	key.State = ""
	if s.seenReferences[key] {
		return
	}
	s.seenReferences[key] = true
	s.ReferencesChecked++
	if r.State != "present" {
		s.UnresolvedReferences = append(s.UnresolvedReferences, r)
	}
}

var (
	indexFileLink   = regexp.MustCompile(`^\s*[-*]\s+\[[^]\n]*\]\((?:\./)?([A-Za-z0-9._-]+\.md)(?:#[^)]*)?\)`)
	rolloutFileLink = regexp.MustCompile(`(?:^|[\s("'\x60])rollout_summaries/([A-Za-z0-9][A-Za-z0-9._-]*\.md)`)
)

// Only known local reference forms are inspected. No URL fetches, arbitrary
// Markdown traversal, absolute paths, parent paths or recursive link following.
func (s *inventoryStats) fileReferences(j statsJob, text string) {
	if j.Kind != "claude_index" && j.Kind != "codex_consolidated" {
		return
	}
	fenceChar, fenceLen := byte(0), 0
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			n := 0
			for n < len(trim) && trim[n] == trim[0] {
				n++
			}
			if fenceLen == 0 {
				fenceChar, fenceLen = trim[0], n
			} else if fenceChar == trim[0] && n >= fenceLen && strings.TrimSpace(trim[n:]) == "" {
				fenceLen = 0
			}
			continue
		}
		if fenceLen > 0 {
			continue
		}
		re, kind, dir := indexFileLink, "claude_index_file", filepath.Dir(j.Path)
		if j.Kind == "codex_consolidated" {
			re, kind, dir = rolloutFileLink, "codex_rollout_file", filepath.Join(dir, "rollout_summaries")
		}
		for _, match := range re.FindAllStringSubmatchIndex(line, -1) {
			if j.Kind == "codex_consolidated" && match[1] < len(line) && !strings.ContainsRune(" \t\r)\"'`#", rune(line[match[1]])) {
				continue
			}
			target := filepath.Join(dir, line[match[2]:match[3]])
			state := "present"
			info, err := os.Stat(target)
			switch {
			case errors.Is(err, os.ErrNotExist):
				state = "missing"
			case err != nil:
				state = "error"
				s.issue(target, err)
			case !info.Mode().IsRegular():
				state = "not_file"
			}
			s.reference(referenceIssue{j.Path, kind, target, state})
		}
	}
}

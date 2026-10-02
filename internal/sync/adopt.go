package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
)

// ClaudeSharedAdopt retires the hand-authored Claude originals of memories that
// now render into the shared dir, so a session in the original's slug stops
// seeing each one twice. An original is adopted only when its content is
// identical to canonical's and the shared render already exists; anything else
// is reported and left byte-for-byte.
type ClaudeSharedAdopt struct {
	// ClaudeProjects is <claude home>/projects.
	ClaudeProjects string
	// SharedDir is the shared memory dir.
	SharedDir string
	// RulesFile is the rules file that imports SharedDir's index.
	RulesFile string
	// Memories are the shared, Claude-imported memories whose originals to
	// consider (the caller filters).
	Memories []*schema.CanonicalMemory
}

// original locates a memory's hand-authored original from its import source
// (<slug>/<file>): the slug's memory dir and the file's base name.
func (a ClaudeSharedAdopt) original(m *schema.CanonicalMemory) (dir, file string, ok bool) {
	slug, file, ok := strings.Cut(m.Provenance.ImportSource, "/")
	if !ok || slug == "" || file == "" || strings.Contains(file, "/") {
		return "", "", false
	}
	return filepath.Join(a.ClaudeProjects, slug, "memory"), file, true
}

// Plan classifies each memory's original without writing.
func (a ClaudeSharedAdopt) Plan() ([]MigrateAction, error) {
	var actions []MigrateAction
	for dir, ms := range a.byDir() {
		as, err := a.planDir(dir, ms)
		if err != nil {
			return nil, err
		}
		actions = append(actions, as...)
	}
	sortMigrateActions(actions)
	return actions, nil
}

// Apply removes each adoptable original and its index line, under its slug's
// lock and re-planned inside it, all while holding the shared dir's lock, so the
// shared render it relies on cannot change underneath it.
func (a ClaudeSharedAdopt) Apply() (MigrateResult, error) {
	unlock, err := acquireLock(a.SharedDir)
	if err != nil {
		return MigrateResult{}, err
	}
	defer unlock()
	var res MigrateResult
	dirs := a.byDir()
	keys := make([]string, 0, len(dirs))
	for d := range dirs {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	for _, dir := range keys {
		if err := a.applyDir(dir, dirs[dir], &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (a ClaudeSharedAdopt) applyDir(dir string, ms []*schema.CanonicalMemory, res *MigrateResult) error {
	if dir == "" {
		// No usable original to lock or remove: every one is a skip.
		actions, err := a.planDir(dir, ms)
		res.Skipped = append(res.Skipped, actions...)
		return err
	}
	unlock, err := acquireLock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	actions, err := a.planDir(dir, ms)
	if err != nil {
		return err
	}
	sortMigrateActions(actions)
	for _, act := range actions {
		switch act.Kind {
		case Adopt:
			if err := os.Remove(act.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := removeUnmarkedIndexLine(dir, act.Source); err != nil {
				return err
			}
			res.Adopted = append(res.Adopted, act)
		case Diverged:
			res.Diverged = append(res.Diverged, act)
		default:
			res.Skipped = append(res.Skipped, act)
		}
	}
	return nil
}

// byDir groups the memories by their original's slug dir; one with no usable
// import source is grouped under "".
func (a ClaudeSharedAdopt) byDir() map[string][]*schema.CanonicalMemory {
	out := map[string][]*schema.CanonicalMemory{}
	for _, m := range a.Memories {
		dir, _, _ := a.original(m)
		out[dir] = append(out[dir], m)
	}
	return out
}

func (a ClaudeSharedAdopt) planDir(dir string, ms []*schema.CanonicalMemory) ([]MigrateAction, error) {
	var hashes map[string]string // import source -> NativeHash of the original
	var actions []MigrateAction
	for _, m := range ms {
		_, file, ok := a.original(m)
		if !ok {
			actions = append(actions, MigrateAction{Kind: Skip, Name: m.Name, Reason: "no recorded original"})
			continue
		}
		path := filepath.Join(dir, file)
		act := MigrateAction{Name: m.Name, Source: strings.TrimSuffix(file, ".md"), Path: path}
		content, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			act.Kind, act.Reason = Skip, "original already gone"
		case err != nil:
			return nil, err
		case IsEngramOwned(content):
			act.Kind, act.Reason = Skip, "not hand-authored"
		case !a.sharedRendered(m.Name):
			act.Kind, act.Reason = Skip, "shared render not loadable yet (file, index line and rules import); run reconcile first"
		default:
			if hashes == nil {
				if hashes, err = originalHashes(dir); err != nil {
					return nil, err
				}
			}
			switch h, found := hashes[m.Provenance.ImportSource]; {
			case !found:
				act.Kind, act.Reason = Skip, "original does not import"
			case h == schema.NativeHash(m):
				act.Kind = Adopt
			default:
				act.Kind, act.Reason = Diverged, "original differs from canonical; reconcile imports it first"
			}
		}
		actions = append(actions, act)
	}
	return actions, nil
}

// sharedRendered reports whether Claude can load engram's shared render of
// name: the render is engram's, the shared index carries its line, and the rules
// file is engram's current one, importing that index. An interrupted sync can
// leave the render without the rest, and an original retired then would leave
// the memory loadable nowhere.
func (a ClaudeSharedAdopt) sharedRendered(name string) bool {
	content, err := os.ReadFile(filepath.Join(a.SharedDir, name+".md"))
	if err != nil || !IsEngramOwned(content) {
		return false
	}
	if _, ok := currentIndexLine(a.SharedDir, name); !ok {
		return false
	}
	rules, err := os.ReadFile(a.RulesFile)
	return err == nil && bytes.Equal(rules, SharedRulesContent(a.SharedDir))
}

// originalHashes imports a slug's memory dir as Claude import would and maps
// each original's import source to the NativeHash of its content, so identity is
// judged by exactly the parsing import uses.
func originalHashes(dir string) (map[string]string, error) {
	res, err := importer.ImportClaude(dir, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(res.Memories))
	for _, m := range res.Memories {
		out[m.Provenance.ImportSource] = schema.NativeHash(m)
	}
	return out, nil
}

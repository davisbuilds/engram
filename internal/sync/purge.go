package sync

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
)

// Purge removes the engram-owned renders of forgotten memories that engram can
// address without a session cwd: marked files and their marked index lines in
// every Claude project slug, and notes in the shared Codex notes directory. A
// hand-authored file or line is never touched. An empty location skips that
// harness.
type Purge struct {
	// ClaudeProjects is <claude home>/projects.
	ClaudeProjects string
	// SharedDir is the shared Claude memory dir (<claude home>/engram/memory).
	SharedDir string
	// CodexExtDir is engram's Codex extension directory.
	CodexExtDir string
	Names       []string
}

const purgeNote = "memory forgotten"

// Plan lists every render Apply would remove, as STALE actions.
func (p Purge) Plan() ([]Action, error) {
	var actions []Action
	dirs, err := p.claudeDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		as, err := p.planClaude(dir)
		if err != nil {
			return nil, err
		}
		actions = append(actions, as...)
	}
	as, err := p.planCodex()
	if err != nil {
		return nil, err
	}
	actions = append(actions, as...)
	sortActions(actions)
	return actions, nil
}

// Apply removes the planned renders, each directory under its own lock and
// re-planned inside it, so a render written since the dry-run is still seen.
func (p Purge) Apply() (Result, error) {
	var res Result
	dirs, err := p.claudeDirs()
	if err != nil {
		return res, err
	}
	for _, dir := range dirs {
		if err := p.applyLocked(dir, p.planClaude, &res, func(a Action) error {
			if a.Path != indexPath(dir) {
				if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			return removeIndexLine(dir, a.Name)
		}); err != nil {
			return res, err
		}
	}
	if p.CodexExtDir != "" {
		if err := p.applyLocked(p.CodexExtDir, func(string) ([]Action, error) { return p.planCodex() }, &res, func(a Action) error {
			if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}); err != nil {
			return res, err
		}
	}
	sortActions(res.Applied)
	return res, nil
}

func (p Purge) applyLocked(dir string, plan func(string) ([]Action, error), res *Result, remove func(Action) error) error {
	if !fileExists(dir) {
		return nil
	}
	unlock, err := acquireLock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	actions, err := plan(dir)
	if err != nil {
		return err
	}
	for _, a := range actions {
		if err := remove(a); err != nil {
			return err
		}
		res.Applied = append(res.Applied, a)
	}
	return nil
}

// claudeDirs lists every project slug's memory directory, then the shared
// memory dir when it exists.
func (p Purge) claudeDirs() ([]string, error) {
	dirs, err := p.slugDirs()
	if err != nil {
		return nil, err
	}
	if p.SharedDir != "" && fileExists(p.SharedDir) {
		dirs = append(dirs, p.SharedDir)
	}
	return dirs, nil
}

// slugDirs lists every project slug's memory directory.
func (p Purge) slugDirs() ([]string, error) {
	if p.ClaudeProjects == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(p.ClaudeProjects)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			if dir := filepath.Join(p.ClaudeProjects, e.Name(), "memory"); fileExists(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs, nil
}

// planClaude plans removing each named memory's owned file in dir, or, when
// only its marked index line is left, that line.
func (p Purge) planClaude(dir string) ([]Action, error) {
	owned, _, err := scanMemoryDir(dir)
	if err != nil {
		return nil, err
	}
	var actions []Action
	for _, name := range p.Names {
		if f, ok := owned[name]; ok {
			actions = append(actions, Action{Stale, name, f.path, purgeNote})
		} else if _, ok := currentIndexLine(dir, name); ok {
			actions = append(actions, Action{Stale, name, indexPath(dir), purgeNote + " (index line)"})
		}
	}
	return actions, nil
}

func (p Purge) planCodex() ([]Action, error) {
	if p.CodexExtDir == "" {
		return nil, nil
	}
	owned, err := scanCodexNotes(CodexTarget{ExtensionDir: p.CodexExtDir}.notesDir())
	if err != nil {
		return nil, err
	}
	var actions []Action
	for name, n := range owned {
		if slices.Contains(p.Names, name) {
			actions = append(actions, Action{Stale, name, n.path, purgeNote})
		}
	}
	return actions, nil
}

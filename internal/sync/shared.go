package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
)

// SharedHarness names the shared Claude target in command output, next to the
// per-project "claude-code" target.
const SharedHarness = "claude-code:shared"

// RulesActionName is the Action name for the rules file. A memory name is
// kebab-case and cannot start with "_", so it never collides with one.
const RulesActionName = "_rules"

// ClaudeSharedTarget renders the memories every Claude project shares into one
// memory dir outside any project slug, and keeps an engram-owned rules file
// that imports that dir's MEMORY.md, so Claude Code loads the shared index in
// every session. The dir is an ordinary Claude memory dir: its files, index,
// ownership and STALE handling are ClaudeTarget's.
type ClaudeSharedTarget struct {
	// Dir is <claude home>/engram/memory.
	Dir string
	// RulesFile is <claude home>/rules/engram-memory.md.
	RulesFile string
	Desired   []*schema.CanonicalMemory
	// KeepStale holds back removals, as for ClaudeTarget.
	KeepStale bool
	// Discard names edited shared renders to overwrite or remove anyway.
	Discard map[string]bool
}

// Harness identifies this target in command output.
func (ClaudeSharedTarget) Harness() string { return SharedHarness }

// DesiredMemories returns the shared memories.
func (t ClaudeSharedTarget) DesiredMemories() []*schema.CanonicalMemory { return t.Desired }

func (t ClaudeSharedTarget) dir() ClaudeTarget {
	return ClaudeTarget{MemoryDir: t.Dir, Desired: t.Desired, KeepStale: t.KeepStale, Stamp: true, Discard: t.Discard}
}

// Plan computes the actions without writing: the shared dir's, then the rules
// file's.
func (t ClaudeSharedTarget) Plan() ([]Action, error) {
	actions, err := t.dir().Plan()
	if err != nil {
		return nil, err
	}
	a, ok, err := t.planRules()
	if err != nil {
		return nil, err
	}
	if ok {
		actions = append(actions, a)
	}
	sortActions(actions)
	return actions, nil
}

// Apply reconciles the shared dir and then the rules file, both under the
// dir's lock, so the rules file is written only once the index it imports is.
func (t ClaudeSharedTarget) Apply() (Result, error) {
	unlock, err := acquireLock(t.Dir)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	res, err := t.dir().applyLocked()
	if err != nil {
		return res, err
	}
	a, ok, err := t.planRules()
	if err != nil || !ok {
		return res, err
	}
	switch a.Kind {
	case Conflict:
		res.Conflicts = append(res.Conflicts, a)
		return res, nil
	case Create, Update:
		err = atomicWrite(t.RulesFile, SharedRulesContent(t.Dir))
	case Stale:
		if err = os.Remove(t.RulesFile); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		return res, err
	}
	res.Applied = append(res.Applied, a)
	sortActions(res.Applied)
	return res, nil
}

// planRules decides the rules file's action: present while anything is shared,
// removed once nothing is (unless removals are held), and never touched when a
// hand-authored file holds the path.
func (t ClaudeSharedTarget) planRules() (Action, bool, error) {
	cur, err := os.ReadFile(t.RulesFile)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Action{}, false, err
	}
	want := len(t.Desired) > 0
	switch {
	case exists && !marker.IsSharedRules(cur):
		if want {
			return Action{Conflict, RulesActionName, t.RulesFile, "target exists and is not engram-owned"}, true, nil
		}
	case want && !exists:
		return Action{Create, RulesActionName, t.RulesFile, ""}, true, nil
	case want && !bytes.Equal(cur, SharedRulesContent(t.Dir)):
		return Action{Update, RulesActionName, t.RulesFile, ""}, true, nil
	case !want && exists && !t.KeepStale:
		return Action{Stale, RulesActionName, t.RulesFile, "nothing is shared"}, true, nil
	}
	return Action{}, false, nil
}

// SharedRulesContent is the rules file for a shared dir: the ownership marker,
// a short explanation naming the dir, and an absolute @ import of its index
// (a relative import would resolve against the rules dir). A space in the path
// is backslash-escaped, as Claude Code's import syntax requires.
func SharedRulesContent(dir string) []byte {
	index := strings.ReplaceAll(filepath.Join(dir, "MEMORY.md"), " ", `\ `)
	return []byte(marker.SharedRulesMarker + "\n" +
		"# Shared memory (engram)\n\n" +
		"Memories engram shares with every project on this machine. Each entry below\n" +
		"names a file in `" + dir + "`; read that file when the entry is relevant.\n" +
		"To revise one, edit its file in place: engram imports the edit on its next\n" +
		"reconcile.\n\n" +
		"@" + index + "\n")
}

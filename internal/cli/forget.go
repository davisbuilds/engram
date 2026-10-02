package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
	"github.com/davisbuilds/engram/internal/tombstone"
)

const forgetUsage = "usage: engram forget <name>... [--reason <text>] [--successor <name>] [--apply] | engram forget --restore <name>... [--apply]"

// cmdForget retires canonical memories: each is tombstoned (so import never
// re-creates it from a surviving native) and removed, and its engram-owned
// renders are removed from every harness location engram writes. Dry-run by
// default; --apply writes. --restore undoes a forget.
func cmdForget(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *forgetArgs)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	names, rerr := memoryNames(pa.pos, forgetUsage)
	if rerr == nil && pa.bools["--restore"] && (len(pa.vals["--reason"]) > 0 || len(pa.vals["--successor"]) > 0) {
		rerr = usageError("--restore takes no --reason or --successor")
	}
	if succ := pa.last("--successor"); rerr == nil && succ != "" && !schema.ValidName(succ) {
		rerr = usageError("--successor %q is not a memory name", succ)
	}
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	s, serr := e.newSession()
	if serr != nil {
		e.emit(name, false, nil, nil, serr, nil)
		return exitError
	}
	if pa.bools["--restore"] {
		return s.restore(e, name, names)
	}
	return s.forget(e, name, names, tombstone.Note{Reason: pa.last("--reason"), Successor: pa.last("--successor")})
}

// forgetCommand is the forget --apply invocation for names with note.
func forgetCommand(names []string, note tombstone.Note) string {
	cmd := "engram forget " + strings.Join(names, " ")
	if note.Successor != "" {
		cmd += " --successor " + note.Successor
	}
	if note.Reason != "" {
		cmd += " --reason " + agentexec.ShellQuote(note.Reason)
	}
	return cmd + " --apply"
}

// memoryNames validates and de-duplicates the names a command was given.
func memoryNames(pos []string, usage string) ([]string, *RespError) {
	if len(pos) == 0 {
		return nil, usageError("%s", usage)
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range pos {
		if !schema.ValidName(n) {
			return nil, usageError("%q is not a memory name (kebab-case)", n)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *session) forget(e *env, name string, names []string, note tombstone.Note) int {
	root := s.cfg.CanonicalRoot
	var mems []*schema.CanonicalMemory
	var paths []string
	var unknown []string
	for _, n := range names {
		m, path, found, err := store.Load(root, n)
		if err != nil {
			e.emit(name, false, nil, nil, &RespError{Code: "load", Message: err.Error()}, nil)
			return exitError
		}
		if !found {
			unknown = append(unknown, n)
			continue
		}
		mems = append(mems, m)
		paths = append(paths, path)
	}
	if len(unknown) > 0 {
		e.emit(name, false, nil, nil, &RespError{
			Code:    "unknown_memory",
			Message: "no canonical memory named " + strings.Join(unknown, ", ") + "; nothing was forgotten",
		}, nil)
		return exitUsage
	}

	var warns []string
	purge := sync.Purge{Names: names}
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		purge.ClaudeProjects = filepath.Join(h.Home, "projects")
		purge.SharedDir = sharedTarget(h.Home, nil, false).Dir
	} else {
		warns = append(warns, s.skippedNote(config.HarnessClaude))
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		purge.CodexExtDir = codexExtDir(h.Home)
	} else {
		warns = append(warns, s.skippedNote(config.HarnessCodex))
	}
	src, err := s.nativeSources()
	if err != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "import", Message: err.Error()}, nil)
		return exitError
	}

	items := make([]map[string]any, 0, len(mems))
	var next []NextStep
	for i, m := range mems {
		natives := src.of(m)
		items = append(items, map[string]any{
			"name": m.Name, "path": paths[i], "origin": m.Provenance.Origin,
			"tombstone": tombstone.Path(root, m.Name), "natives": natives,
		})
		if sourceHarness(m) != config.HarnessClaude {
			continue
		}
		for _, p := range natives {
			next = append(next, NextStep{
				Reason:  m.Name + "'s hand-authored source still loads in Claude Code; engram never deletes it",
				Command: "rm " + agentexec.ShellQuote(p),
			})
		}
	}
	data := map[string]any{"apply": e.apply, "memories": items}

	if !e.apply {
		actions, err := purge.Plan()
		if err != nil {
			e.emit(name, false, data, warns, &RespError{Code: "plan", Message: err.Error()}, nil)
			return exitError
		}
		data["renders"] = orEmpty(actions)
		next = append([]NextStep{{
			Reason:  "dry run: nothing was written",
			Command: forgetCommand(names, note),
		}}, next...)
		e.emit(name, true, data, warns, nil, next)
		return exitOK
	}

	release, lerr := canonLock(root)
	if lerr != nil {
		e.emit(name, false, data, warns, lerr, nil)
		return exitError
	}
	// Re-check under the lock, so the batch forgets whole or not at all.
	for _, m := range mems {
		if _, _, found, lerr := store.Load(root, m.Name); lerr != nil || !found {
			release()
			e.emit(name, false, data, warns, &RespError{Code: "changed", Message: m.Name + " changed since it was read; nothing was forgotten"}, nil)
			return exitError
		}
	}
	now := time.Now()
	for i, m := range mems {
		ts, ferr := tombstone.Forget(root, m.Name, note, now)
		if ferr != nil {
			release()
			e.emit(name, false, data, warns, &RespError{Code: "forget", Message: ferr.Error()}, nil)
			return exitError
		}
		items[i]["forgotten_at"] = ts.ForgottenAt
	}
	release()

	res, perr := purge.Apply()
	data["renders"] = orEmpty(res.Applied)
	if perr != nil {
		e.emit(name, false, data, warns, &RespError{Code: "purge", Message: "forgotten, but removing renders failed: " + perr.Error()}, next)
		return exitError
	}
	e.emit(name, true, data, warns, nil, next)
	return exitOK
}

func (s *session) restore(e *env, name string, names []string) int {
	root := s.cfg.CanonicalRoot
	release := func() {}
	if e.apply {
		// Check and write under one lock, so the batch restores whole or not at all.
		r, lerr := canonLock(root)
		if lerr != nil {
			e.emit(name, false, nil, nil, lerr, nil)
			return exitError
		}
		release = r
	}
	defer release()

	set, err := tombstone.Load(root)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "tombstones", Message: err.Error()}, nil)
		return exitError
	}
	var unknown []string
	items := make([]map[string]any, 0, len(names))
	taken := false
	for _, n := range names {
		ts, ok := set.Latest(n)
		if !ok {
			unknown = append(unknown, n)
			continue
		}
		item := map[string]any{"name": n, "path": filepath.Join(root, filepath.FromSlash(ts.Path)), "outcome": "restore"}
		_, _, found, lerr := store.Load(root, n)
		if found || errors.Is(lerr, store.ErrWithheld) {
			item["outcome"] = "taken"
			taken = true
		} else if lerr != nil {
			e.emit(name, false, nil, nil, &RespError{Code: "load", Message: lerr.Error()}, nil)
			return exitError
		}
		items = append(items, item)
	}
	if len(unknown) > 0 {
		e.emit(name, false, nil, nil, &RespError{
			Code:    "unknown_tombstone",
			Message: "no forgotten memory named " + strings.Join(unknown, ", ") + "; nothing was restored",
		}, nil)
		return exitUsage
	}
	data := map[string]any{"apply": e.apply, "memories": items}
	if taken {
		e.emit(name, false, data, nil, &RespError{
			Code:    "name_taken",
			Message: "a canonical memory already holds a name to restore; nothing was restored",
		}, nil)
		return exitConflicts
	}
	if !e.apply {
		e.emit(name, true, data, nil, nil, []NextStep{{
			Reason:  "dry run: nothing was written",
			Command: "engram forget --restore " + strings.Join(names, " ") + " --apply",
		}})
		return exitOK
	}
	for _, item := range items {
		path, rerr := tombstone.Restore(root, item["name"].(string))
		if rerr != nil {
			e.emit(name, false, data, nil, &RespError{Code: "restore", Message: rerr.Error()}, nil)
			return exitError
		}
		item["path"], item["outcome"] = path, "restored"
	}
	e.emit(name, true, data, nil, nil, []NextStep{{
		Reason:  "restored memories render again on the next sync",
		Command: "engram reconcile --apply",
	}})
	return exitOK
}

// nativeIndex is where forgotten memories' native sources still live, by the
// memory name each holds: Claude files across every project slug, and Codex
// Task Groups.
type nativeIndex struct {
	claude map[string][]string
	codex  map[string]string
}

func (s *session) nativeSources() (nativeIndex, error) {
	idx := nativeIndex{claude: map[string][]string{}, codex: map[string]string{}}
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		dirs, err := filepath.Glob(filepath.Join(h.Home, "projects", "*", "memory"))
		if err != nil {
			return idx, err
		}
		// Key each hand-authored file by the memory it holds (the name import
		// gives it), so a same-named file holding another memory never matches.
		for _, dir := range dirs {
			res, err := importer.ImportClaude(dir, "")
			if err != nil {
				return idx, err
			}
			for _, m := range res.Memories {
				idx.claude[m.Name] = append(idx.claude[m.Name], filepath.Join(dir, m.Provenance.Source))
			}
		}
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		file := filepath.Join(h.Home, "memories", "MEMORY.md")
		res, err := importer.ImportCodex(file)
		if err != nil {
			return idx, err
		}
		for _, m := range res.Memories {
			idx.codex[m.Name] = fmt.Sprintf("Task Group %q in %s", m.Description, file)
		}
	}
	return idx, nil
}

// of lists where m's native source still lives, if anywhere.
func (idx nativeIndex) of(m *schema.CanonicalMemory) []string {
	out := []string{}
	switch sourceHarness(m) {
	case config.HarnessClaude:
		out = append(out, idx.claude[m.Name]...)
	case config.HarnessCodex:
		if g, ok := idx.codex[m.Name]; ok {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

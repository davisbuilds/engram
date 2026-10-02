package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/gitroot"
	"github.com/davisbuilds/engram/internal/harness"
	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/slug"
	"github.com/davisbuilds/engram/internal/sync"
)

// canonLock takes the exclusive canonical-root lock shared by every canonical
// mutator (remember, share, import --apply, curate --apply), so no two writers
// interleave — closing the load-then-write race in store.Save between two
// concurrent same-name writes, and keeping a multi-file batch atomic against a
// single write. A held lock yields a retryable "locked" error rather than a
// silent second writer.
func canonLock(root string) (func(), *RespError) {
	release, err := lock.Acquire(root)
	if err != nil {
		return nil, &RespError{Code: "locked", Message: err.Error()}
	}
	return release, nil
}

// session resolves the cwd/host context and loads config once, shared by the
// sync/audit/list/discover commands.
type session struct {
	cfg *config.Config
	cwd string
	// claudeRoot is the directory Claude Code keys this cwd's auto memory by:
	// the main repository's root when cwd is inside one (a subdirectory and a
	// linked worktree alike), else cwd itself. The project slug target, and
	// what belongs in it, follow claudeRoot, not cwd.
	claudeRoot    string
	host          string
	agentOverride string
}

func (e *env) newSession() (*session, *RespError) {
	cfg, err := config.Load(e.config)
	if err != nil {
		return nil, &RespError{Code: "config_load", Message: err.Error()}
	}
	cwd := e.cwd
	if cwd == "" {
		wd, werr := os.Getwd()
		if werr != nil {
			return nil, &RespError{Code: "cwd", Message: werr.Error()}
		}
		cwd = wd
	}
	cwd, err = normalizeCwd(cwd)
	if err != nil {
		return nil, &RespError{Code: "cwd", Message: err.Error()}
	}
	claudeRoot := cwd
	if root, ok := gitroot.Main(cwd); ok {
		claudeRoot = root
	}
	return &session{cfg: cfg, cwd: cwd, claudeRoot: claudeRoot, host: e.resolveHost(cfg), agentOverride: e.agent}, nil
}

// agentFor returns the effective agent for scope filtering: an explicit --agent
// override wins, otherwise the harness's own native agent name.
func (s *session) agentFor(native string) string {
	if s.agentOverride != "" {
		return s.agentOverride
	}
	return native
}

// harnessFailure is the top-level error for a multi-harness command when any
// harness failed: its code is harness_failed and its message names each failed
// harness, so a consumer branching on error.code sees the failure without
// walking data (each entry keeps its own error too). nil when none failed.
func harnessFailure(entries []map[string]any) *RespError {
	var msgs []string
	for _, en := range entries {
		if err, ok := en["error"].(string); ok {
			msgs = append(msgs, fmt.Sprintf("%v: %s", en["harness"], err))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return &RespError{Code: "harness_failed", Message: strings.Join(msgs, "; ")}
}

// skippedNote is the warning for a disabled harness a command skipped, naming
// the reason when the config's harnesses: section simply left it out.
func (s *session) skippedNote(harnessName string) string {
	if s.cfg.Harnesses[harnessName].Unlisted {
		return harnessName + " disabled (not listed under harnesses: in the config); skipped"
	}
	return harnessName + " disabled; skipped"
}

// targets builds a reconcilable target for every enabled harness, filtering the
// discovered memories per harness (the agent axis differs by harness).
func (s *session) targets() ([]sync.Target, []string, *RespError) {
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		return nil, nil, &RespError{Code: "discover", Message: err.Error()}
	}
	warns := warnParseErrors(perrs)
	keepStale, hwarns := staleHold(s.cfg.CanonicalRoot, perrs)
	warns = append(warns, hwarns...)

	var targets []sync.Target
	if h := s.cfg.Harnesses[config.HarnessClaude]; h.Enabled() {
		agent := s.agentFor("claude")
		rel := withoutShared(scope.RelevantFor(mems, s.claudeRoot, agent, s.host), agent, s.host)
		targets = append(targets, sync.ClaudeTarget{
			MemoryDir: claudeMemoryDir(h.Home, s.claudeRoot), Desired: rel, KeepStale: keepStale,
		}, sharedTarget(h.Home, scope.Shared(mems, agent, s.host), keepStale))
		warns = append(warns, harnessWarnings(harness.CheckClaude(h.Home, true))...)
	} else {
		warns = append(warns, s.skippedNote(config.HarnessClaude))
	}
	if h := s.cfg.Harnesses[config.HarnessCodex]; h.Enabled() {
		rel := scope.RelevantFor(mems, s.cwd, s.agentFor("codex"), s.host)
		targets = append(targets, sync.CodexTarget{
			ExtensionDir: codexExtDir(h.Home), Desired: rel, Now: time.Now, KeepStale: keepStale,
			InView: codexInView(mems, s.cwd, s.agentFor("codex"), s.host),
		})
		warns = append(warns, harnessWarnings(harness.CheckCodex(h.Home, true))...)
	} else {
		warns = append(warns, s.skippedNote(config.HarnessCodex))
	}
	return targets, warns, nil
}

// codexInView reports, for an owned Codex note, whether it belongs to the session
// at cwd run by agent on host. Codex keeps one notes directory for every session,
// so only a note this session can see (its recorded scope, and its canonical
// memory's cwd, agent and host axes) may be removed as stale; notes other
// sessions rendered are left for them.
func codexInView(mems []*schema.CanonicalMemory, cwd, agent, host string) func(name, scope string) bool {
	byName := make(map[string]*schema.CanonicalMemory, len(mems))
	for _, m := range mems {
		byName[m.Name] = m
	}
	return func(name, renderScope string) bool {
		return scope.InView(renderScope, byName[name], cwd, agent, host)
	}
}

func cmdSync(e *env, name string, _ []string) int {
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	targets, warns, rerr := s.targets()
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}
	if len(targets) == 0 {
		e.emit(name, false, nil, warns, &RespError{Code: "no_harness", Message: "no enabled harness to sync"}, nil)
		return exitUsage
	}

	exit := exitOK
	var next []NextStep
	entries := make([]map[string]any, 0, len(targets))
	for _, tg := range targets {
		entry := map[string]any{"harness": tg.Harness()}
		if !e.apply {
			actions, err := tg.Plan()
			if err != nil {
				entry["error"] = err.Error()
				exit = worseExit(exit, exitError)
			} else {
				entry["actions"] = orEmpty(actions)
				exit = worseExit(exit, exitForActions(actions))
				next = append(next, conflictNextSteps(actions)...)
			}
		} else {
			res, err := tg.Apply()
			if err != nil {
				entry["error"] = err.Error()
				exit = worseExit(exit, exitError)
			} else {
				entry["result"] = res
				if len(res.Conflicts) > 0 {
					exit = worseExit(exit, exitConflicts)
					next = append(next, conflictNextSteps(res.Conflicts)...)
				}
			}
		}
		entries = append(entries, entry)
	}
	e.emit(name, exit == exitOK, map[string]any{
		"cwd": s.cwd, "host": s.host, "apply": e.apply, "harnesses": entries,
	}, warns, harnessFailure(entries), next)
	return exit
}

func cmdAudit(e *env, name string, _ []string) int {
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	targets, warns, rerr := s.targets()
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}

	exit := exitOK
	var next []NextStep
	entries := make([]map[string]any, 0, len(targets))
	for _, tg := range targets {
		entry := map[string]any{"harness": tg.Harness()}
		actions, err := tg.Plan()
		if err != nil {
			entry["error"] = err.Error()
			exit = worseExit(exit, exitError)
		} else {
			entry["actions"] = orEmpty(actions)
			exit = worseExit(exit, exitForActions(actions))
			next = append(next, conflictNextSteps(actions)...)
		}
		entries = append(entries, entry)
	}
	e.emit(name, exit == exitOK, map[string]any{
		"cwd": s.cwd, "host": s.host, "harnesses": entries,
	}, warns, harnessFailure(entries), next)
	return exit
}

func cmdList(e *env, name string, _ []string) int {
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	relevant := scope.RelevantFor(mems, s.claudeRoot, s.agentFor("claude"), s.host)
	e.emit(name, true, map[string]any{
		"cwd": s.cwd, "host": s.host, "memories": memoryItems(relevant),
	}, warnParseErrors(perrs), nil, nil)
	return exitOK
}

func cmdDiscover(e *env, name string, _ []string) int {
	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	mems, perrs, derr := discover.Discover(cfg.CanonicalRoot)
	if derr != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "discover", Message: derr.Error()}, nil)
		return exitError
	}
	e.emit(name, true, map[string]any{
		"canonical_root": cfg.CanonicalRoot, "count": len(mems), "memories": memoryItems(mems),
	}, warnParseErrors(perrs), nil, nil)
	return exitOK
}

// cmdDiff shows the full per-memory reconciliation status for each harness,
// including the memories already in sync — a superset of audit's pending actions.
func cmdDiff(e *env, name string, _ []string) int {
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	targets, warns, rerr := s.targets()
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}
	exit := exitOK
	entries := make([]map[string]any, 0, len(targets))
	for _, tg := range targets {
		entry := map[string]any{"harness": tg.Harness()}
		actions, err := tg.Plan()
		if err != nil {
			entry["error"] = err.Error()
			exit = worseExit(exit, exitError)
			entries = append(entries, entry)
			continue
		}
		byName := map[string]sync.ActionKind{}
		for _, a := range actions {
			byName[a.Name] = a.Kind
		}
		items := []map[string]string{}
		for _, m := range tg.DesiredMemories() {
			status := "in-sync"
			if k, ok := byName[m.Name]; ok {
				status = string(k)
				delete(byName, m.Name)
			}
			items = append(items, map[string]string{"name": m.Name, "status": status})
		}
		for nm, k := range byName {
			items = append(items, map[string]string{"name": nm, "status": string(k)})
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["name"] < items[j]["name"] })
		entry["memories"] = items
		exit = worseExit(exit, exitForActions(actions))
		entries = append(entries, entry)
	}
	e.emit(name, exit == exitOK, map[string]any{"cwd": s.cwd, "host": s.host, "harnesses": entries}, warns, harnessFailure(entries), nil)
	return exit
}

// cmdShow dumps a harness's engram-rendered memories. Reading a disabled harness
// is permissive: it proceeds with a warning (contrast import, which is strict).
func cmdShow(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *harnessArg)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	var harness string
	if len(pa.pos) == 1 {
		harness = pa.pos[0]
	}
	if harness == "" {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: "usage: engram show <claude-code|codex>"}, nil)
		return exitUsage
	}
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}

	var (
		warns []string
		items []map[string]string
	)
	switch harness {
	case config.HarnessClaude:
		h := s.cfg.Harnesses[config.HarnessClaude]
		if !h.Enabled() {
			warns = append(warns, "claude-code is disabled; showing anyway (read is permissive)")
		}
		items = showClaude(claudeMemoryDir(h.Home, s.claudeRoot))
	case config.HarnessCodex:
		h := s.cfg.Harnesses[config.HarnessCodex]
		if !h.Enabled() {
			warns = append(warns, "codex is disabled; showing anyway (read is permissive)")
		}
		items = showCodex(filepath.Join(codexExtDir(h.Home), "notes"))
	default:
		e.emit(name, false, nil, nil, &RespError{Code: "unknown_harness", Message: "harness must be claude-code or codex"}, nil)
		return exitUsage
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["name"] < items[j]["name"] })
	e.emit(name, true, map[string]any{"harness": harness, "count": len(items), "memories": items}, warns, nil, nil)
	return exitOK
}

func showClaude(dir string) []map[string]string {
	items := []map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return items
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if sync.IsEngramOwned(data) {
			items = append(items, map[string]string{"name": strings.TrimSuffix(e.Name(), ".md"), "path": path})
		}
	}
	return items
}

func showCodex(notesDir string) []map[string]string {
	items := []map[string]string{}
	entries, err := os.ReadDir(notesDir)
	if err != nil {
		return items
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(notesDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if n, scp, ok := marker.CodexNoteName(string(data)); ok {
			items = append(items, map[string]string{"name": n, "scope": scp, "path": path})
		}
	}
	return items
}

// resolveHost maps the current machine to its configured host label. An explicit
// --host wins; an unmapped hostname yields "" so host-scoped memories fail closed.
func (e *env) resolveHost(cfg *config.Config) string {
	if e.host != "" {
		return e.host
	}
	h, _ := os.Hostname()
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	label, _ := cfg.HostLabel(h)
	return label
}

func claudeMemoryDir(claudeHome, cwd string) string {
	return filepath.Join(claudeHome, "projects", slug.ForCwd(cwd), "memory")
}

// sharedTarget is the one Claude memory dir every project shares, loaded into
// each session through an engram-owned rules file that imports its index.
func sharedTarget(claudeHome string, shared []*schema.CanonicalMemory, keepStale bool) sync.ClaudeSharedTarget {
	return sync.ClaudeSharedTarget{
		Dir:       filepath.Join(claudeHome, "engram", "memory"),
		RulesFile: filepath.Join(claudeHome, "rules", "engram-memory.md"),
		Desired:   shared,
		KeepStale: keepStale,
	}
}

// withoutShared drops the memories the shared target renders, so a project
// slug holds only what is not shared with every project.
func withoutShared(mems []*schema.CanonicalMemory, agent, host string) []*schema.CanonicalMemory {
	out := mems[:0:0]
	for _, m := range mems {
		if !scope.IsShared(m, agent, host) {
			out = append(out, m)
		}
	}
	return out
}

func codexExtDir(codexHome string) string {
	return filepath.Join(codexHome, "memories", "extensions", marker.Extension)
}

func memoryItems(mems []*schema.CanonicalMemory) []map[string]string {
	items := make([]map[string]string, 0, len(mems))
	for _, m := range mems {
		items = append(items, map[string]string{"name": m.Name, "scope": m.Scope, "type": string(m.Type)})
	}
	return items
}

func warnParseErrors(perrs []discover.ParseError) []string {
	if len(perrs) == 0 {
		return nil
	}
	w := make([]string, 0, len(perrs))
	for _, p := range perrs {
		w = append(w, "unparseable canonical file "+p.Path+": "+p.Err.Error())
	}
	return w
}

func exitForActions(actions []sync.Action) int {
	for _, a := range actions {
		if a.Kind == sync.Conflict {
			return exitConflicts
		}
	}
	return exitOK
}

// orEmpty coerces a nil slice to an empty one so JSON consumers always see a
// list to iterate, never null. Agent-first: the envelope's iterated arrays are
// stable types.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// worseExit returns the more significant of two exit codes: a hard error
// outranks conflicts, which outrank success.
func worseExit(a, b int) int {
	prio := map[int]int{exitOK: 0, exitConflicts: 1, exitError: 2, exitUsage: 2}
	if prio[b] > prio[a] {
		return b
	}
	return a
}

// conflictNextSteps turns each CONFLICT into an agent-consumable lead.
func conflictNextSteps(actions []sync.Action) []NextStep {
	var steps []NextStep
	for _, a := range actions {
		if a.Kind == sync.Conflict {
			steps = append(steps, NextStep{
				Reason:  "unmarked file at " + a.Path + " blocks rendering " + a.Name,
				Command: "remove or rename the hand-authored file, then re-run engram sync",
			})
		}
	}
	return steps
}

// staleHold reports whether STALE removals must be held back this run. A missing
// canonical root or an unparseable canonical file means the desired set may omit
// memories that still exist, so pruning their renders would delete live lessons.
func staleHold(root string, perrs []discover.ParseError) (bool, []string) {
	reason := ""
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		reason = "canonical root " + root + " does not exist"
	} else if len(perrs) > 0 {
		reason = fmt.Sprintf("%d canonical file(s) failed to parse or validate", len(perrs))
	}
	if reason == "" {
		return false, nil
	}
	return true, []string{"stale renders kept, not removed: " + reason}
}

// normalizeCwd turns any spelling of a directory into the one absolute, clean
// path the Claude project slug is derived from: ~ is expanded, a relative path
// is resolved against the process cwd, and trailing slashes and dot segments are
// removed. Symlinks are left as given.
func normalizeCwd(cwd string) (string, error) {
	if cwd == "~" || strings.HasPrefix(cwd, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cwd = filepath.Join(home, strings.TrimPrefix(cwd, "~"))
	}
	return filepath.Abs(cwd)
}

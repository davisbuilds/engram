package cli

import (
	"fmt"
	"path/filepath"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
)

// cmdMigrate adopts hand-authored native memory that canonical provably
// supersedes, converting it to engram-owned in place so a later sync into the
// same slug neither duplicates nor conflicts. It is the only command permitted to
// modify or delete an unmarked (hand-authored) file, and only under --apply;
// matching is deterministic (provenance source id or slug-equality) and adoption
// is gated on body-identity, so a diverged or ambiguous file is reported and left
// byte-for-byte untouched.
func cmdMigrate(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *migrateArgs)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	var harness string
	if len(pa.pos) == 1 {
		harness = pa.pos[0]
	}
	if harness == "" {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: "usage: engram migrate claude-code [--shared] [--apply]"}, nil)
		return exitUsage
	}
	if harness == config.HarnessCodex {
		e.emit(name, false, nil, nil, &RespError{Code: "unsupported_harness", Message: "migrate supports claude-code only; codex keeps one consolidated MEMORY.md folded by its own consolidator (tracked in BACKLOG)"}, nil)
		return exitUsage
	}
	if harness != config.HarnessClaude {
		e.emit(name, false, nil, nil, &RespError{Code: "unknown_harness", Message: "harness must be claude-code"}, nil)
		return exitUsage
	}

	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	h := s.cfg.Harnesses[config.HarnessClaude]
	if !h.Enabled() {
		e.emit(name, false, nil, nil, &RespError{Code: "harness_disabled", Message: "claude-code is disabled; cannot migrate it"}, nil)
		return exitUsage
	}
	if pa.bools["--shared"] {
		return cmdMigrateShared(e, name, s, h.Home)
	}

	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	warns := warnParseErrors(perrs)
	rel := scope.RelevantFor(mems, s.claudeRoot, s.agentFor("claude"), s.host)
	tgt := sync.ClaudeMigrateTarget{MemoryDir: claudeMemoryDir(h.Home, s.claudeRoot), Desired: rel}

	base := map[string]any{"harness": harness, "apply": e.apply, "cwd": s.cwd}

	if !e.apply {
		actions, perr := tgt.Plan()
		if perr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "migrate", Message: perr.Error()}, nil)
			return exitError
		}
		base["actions"] = orEmpty(actions)
		warns, next := migrateNotes(actions, warns)
		e.emit(name, true, base, warns, nil, next)
		return exitOK
	}

	res, aerr := tgt.Apply()
	if aerr != nil {
		e.emit(name, false, base, warns, &RespError{Code: "migrate", Message: aerr.Error()}, nil)
		return exitError
	}
	base["result"] = res
	all := make([]sync.MigrateAction, 0, len(res.Diverged)+len(res.Ambiguous))
	all = append(all, res.Diverged...)
	all = append(all, res.Ambiguous...)
	warns, next := migrateNotes(all, warns)
	e.emit(name, true, base, warns, nil, next)
	return exitOK
}

// migrateNotes turns diverged/ambiguous classifications into non-fatal warnings
// and, for each diverged file, a runnable reconciliation lead — these are
// decisions engram deliberately does not make deterministically, so it surfaces
// them for the agent rather than acting. The lead supplies *both* versions (the
// native file path and the canonical name) so the agent can actually compare
// them, rather than pointing at curate, which only ever sees canonical.
func migrateNotes(actions []sync.MigrateAction, warns []string) ([]string, []NextStep) {
	var diverged, ambiguous int
	var next []NextStep
	for _, a := range actions {
		switch a.Kind {
		case sync.Diverged:
			diverged++
			next = append(next, NextStep{
				Reason:  "hand-authored " + a.Source + ".md diverged from canonical " + a.Name + "; reconcile the two versions",
				Command: "claude -p --allowedTools Read Edit -- \"The hand-authored Claude memory at " + a.Path + " has diverged from canonical memory " + a.Name + ". Compare them; if the native version carries a better lesson, update canonical (engram remember --from-json -), otherwise leave canonical as-is. Do not blindly overwrite.\"",
			})
		case sync.Ambiguous:
			ambiguous++
		}
	}
	if diverged > 0 {
		warns = append(warns, plural(diverged, "hand-authored file")+" diverged from canonical and were left untouched; see next_steps to reconcile")
	}
	if ambiguous > 0 {
		warns = append(warns, plural(ambiguous, "hand-authored file")+" had ambiguous matches and were left untouched")
	}
	return warns, next
}

func plural(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// cmdMigrateShared retires the hand-authored Claude originals of shared
// memories: each one whose content is identical to canonical's, and whose
// shared render already exists, is removed with its index line, and its
// canonical memory is detached, since the shared render is now where it lives
// (and where an edit to it is imported from). Diverged originals are reported
// and left byte-for-byte; reconcile imports their edits first.
func cmdMigrateShared(e *env, name string, s *session, claudeHome string) int {
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	warns := warnParseErrors(perrs)
	var originals []*schema.CanonicalMemory
	for _, m := range scope.Shared(mems, s.agentFor("claude"), s.host) {
		if originHarness(m) == config.HarnessClaude && m.Provenance.ImportSource != "" {
			originals = append(originals, m)
		}
	}
	adopt := sync.ClaudeSharedAdopt{
		ClaudeProjects: filepath.Join(claudeHome, "projects"),
		SharedDir:      sharedTarget(claudeHome, nil, false).Dir,
		Memories:       originals,
	}
	base := map[string]any{"harness": config.HarnessClaude, "shared": true, "apply": e.apply}
	if !e.apply {
		actions, perr := adopt.Plan()
		if perr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "migrate", Message: perr.Error()}, nil)
			return exitError
		}
		base["actions"] = orEmpty(actions)
		var next []NextStep
		for _, a := range actions {
			if a.Kind == sync.Adopt {
				next = append(next, NextStep{Reason: "dry run: nothing was written", Command: "engram migrate claude-code --shared --apply"})
				break
			}
		}
		e.emit(name, true, base, append(warns, sharedMigrateWarnings(actions)...), nil, next)
		return exitOK
	}
	res, aerr := adopt.Apply()
	base["result"] = res
	if aerr != nil {
		e.emit(name, false, base, warns, &RespError{Code: "migrate", Message: aerr.Error()}, nil)
		return exitError
	}
	// The originals are gone: detach each adopted memory so it is neither kept
	// out of Claude as Claude-imported nor reported as an orphan.
	release, lerr := canonLock(s.cfg.CanonicalRoot)
	if lerr != nil {
		e.emit(name, false, base, warns, lerr, nil)
		return exitError
	}
	defer release()
	for _, a := range res.Adopted {
		m, _, found, lerr := store.Load(s.cfg.CanonicalRoot, a.Name)
		if lerr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "load", Message: lerr.Error()}, nil)
			return exitError
		}
		if !found || originHarness(m) == "" {
			continue
		}
		m.Provenance.Origin = schema.DetachedPrefix + m.Provenance.Origin
		if _, err := store.Replace(s.cfg.CanonicalRoot, m); err != nil {
			e.emit(name, false, base, warns, &RespError{Code: "save", Message: err.Error()}, nil)
			return exitError
		}
	}
	all := append(append([]sync.MigrateAction{}, res.Diverged...), res.Skipped...)
	e.emit(name, true, base, append(warns, sharedMigrateWarnings(all)...), nil, nil)
	return exitOK
}

// sharedMigrateWarnings notes the originals left in place because they differ
// from canonical.
func sharedMigrateWarnings(actions []sync.MigrateAction) []string {
	n := 0
	for _, a := range actions {
		if a.Kind == sync.Diverged {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return []string{plural(n, "original") + " differ from canonical and were left untouched; run engram reconcile --apply to import them, then migrate again"}
}

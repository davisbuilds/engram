package cli

import (
	"fmt"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/scope"
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
	var harness string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && harness == "" {
			harness = a
		}
	}
	if harness == "" {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: "usage: engram migrate <claude-code> [--apply]"}, nil)
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

	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	warns := warnParseErrors(perrs)
	rel := scope.RelevantFor(mems, s.cwd, s.agentFor("claude"), s.host)
	tgt := sync.ClaudeMigrateTarget{MemoryDir: claudeMemoryDir(h.Home, s.cwd), Desired: rel}

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
// and a curate lead — these are decisions engram deliberately does not make
// deterministically, so it surfaces them for the agent rather than acting.
func migrateNotes(actions []sync.MigrateAction, warns []string) ([]string, []NextStep) {
	var diverged, ambiguous int
	for _, a := range actions {
		switch a.Kind {
		case sync.Diverged:
			diverged++
		case sync.Ambiguous:
			ambiguous++
		}
	}
	var next []NextStep
	if diverged > 0 {
		warns = append(warns, plural(diverged, "hand-authored file")+" diverged from canonical and were left untouched; see data for details")
		next = append(next, NextStep{
			Reason:  "diverged hand-authored files need a merge decision engram will not make automatically",
			Command: "engram curate --harness claude-code",
		})
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

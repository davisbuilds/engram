package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/slug"
	"github.com/davisbuilds/engram/internal/sync"
)

// session resolves the cwd/agent/host context and loads config once, shared by
// the sync/audit/list/discover commands.
type session struct {
	cfg   *config.Config
	cwd   string
	agent string
	host  string
}

func (e *env) session(defaultAgent string) (*session, *RespError) {
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
	agent := e.agent
	if agent == "" {
		agent = defaultAgent
	}
	return &session{cfg: cfg, cwd: cwd, agent: agent, host: e.resolveHost(cfg)}, nil
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

// claudeMemoryDir is the per-project memory directory for the given cwd.
func claudeMemoryDir(claudeHome, cwd string) string {
	return filepath.Join(claudeHome, "projects", slug.ForCwd(cwd), "memory")
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

// claudePlan gathers the scope-filtered target for a session, shared by sync's
// dry-run and audit.
func (s *session) claudeTarget() (sync.ClaudeTarget, []discover.ParseError, *RespError) {
	claude := s.cfg.Harnesses[config.HarnessClaude]
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		return sync.ClaudeTarget{}, nil, &RespError{Code: "discover", Message: err.Error()}
	}
	relevant := scope.RelevantFor(mems, s.cwd, s.agent, s.host)
	return sync.ClaudeTarget{
		MemoryDir: claudeMemoryDir(claude.Home, s.cwd),
		Desired:   relevant,
	}, perrs, nil
}

func cmdSync(e *env, name string, _ []string) int {
	s, rerr := e.session("claude")
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	claude := s.cfg.Harnesses[config.HarnessClaude]
	if e.apply && !claude.Enabled() {
		e.emit(name, false, nil, nil, &RespError{
			Code: "harness_disabled", Message: "claude-code is disabled; refusing to write",
		}, nil)
		return exitUsage
	}
	target, perrs, rerr := s.claudeTarget()
	if rerr != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), rerr, nil)
		return exitError
	}

	base := map[string]any{
		"harness": config.HarnessClaude, "cwd": s.cwd, "host": s.host,
		"memory_dir": target.MemoryDir, "relevant": len(target.Desired), "apply": e.apply,
	}

	if !e.apply {
		actions, err := target.Plan()
		if err != nil {
			e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "plan", Message: err.Error()}, nil)
			return exitError
		}
		base["actions"] = actions
		e.emit(name, true, base, warnParseErrors(perrs), nil, conflictNextSteps(actions))
		return exitForActions(actions)
	}

	res, err := target.Apply()
	if err != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "apply", Message: err.Error()}, nil)
		return exitError
	}
	base["result"] = res
	ok := len(res.Conflicts) == 0
	e.emit(name, ok, base, warnParseErrors(perrs), nil, conflictNextSteps(res.Conflicts))
	if !ok {
		return exitConflicts
	}
	return exitOK
}

func cmdAudit(e *env, name string, _ []string) int {
	s, rerr := e.session("claude")
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	target, perrs, rerr := s.claudeTarget()
	if rerr != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), rerr, nil)
		return exitError
	}
	actions, err := target.Plan()
	if err != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "plan", Message: err.Error()}, nil)
		return exitError
	}
	e.emit(name, true, map[string]any{
		"harness": config.HarnessClaude, "cwd": s.cwd, "host": s.host,
		"memory_dir": target.MemoryDir, "relevant": len(target.Desired), "actions": actions,
	}, warnParseErrors(perrs), nil, conflictNextSteps(actions))
	return exitForActions(actions)
}

func cmdList(e *env, name string, _ []string) int {
	s, rerr := e.session("claude")
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, warnParseErrors(perrs), &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	relevant := scope.RelevantFor(mems, s.cwd, s.agent, s.host)
	items := make([]map[string]string, 0, len(relevant))
	for _, m := range relevant {
		items = append(items, map[string]string{"name": m.Name, "scope": m.Scope, "type": string(m.Type)})
	}
	e.emit(name, true, map[string]any{
		"cwd": s.cwd, "agent": s.agent, "host": s.host, "memories": items,
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
	items := make([]map[string]string, 0, len(mems))
	for _, m := range mems {
		items = append(items, map[string]string{"name": m.Name, "scope": m.Scope, "type": string(m.Type)})
	}
	e.emit(name, true, map[string]any{
		"canonical_root": cfg.CanonicalRoot, "count": len(items), "memories": items,
	}, warnParseErrors(perrs), nil, nil)
	return exitOK
}

// conflictNextSteps turns each CONFLICT into an agent-consumable lead.
func conflictNextSteps(actions []sync.Action) []NextStep {
	var steps []NextStep
	for _, a := range actions {
		if a.Kind == sync.Conflict {
			steps = append(steps, NextStep{
				Reason:  "unmarked file at " + a.Path + " blocks rendering " + a.Name,
				Command: "resolve by removing or renaming the hand-authored file, then re-run engram sync",
			})
		}
	}
	return steps
}

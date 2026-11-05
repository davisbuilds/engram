package cli

import (
	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/review"
)

// cmdReview produces health leads over the canonical set and emits each as a
// next_step. It never mutates: the merge decision is the agent's. A
// near-duplicate lead points to the existing read-only curate proposer, which
// validates operations instead of inviting direct canonical edits. Scope judgment
// is left to curation, where an agent can reason about a memory's content.
func cmdReview(e *env, name string, _ []string) int {
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

	findings := review.Analyze(mems)
	items := make([]map[string]any, 0, len(findings))
	next := make([]NextStep, 0, len(findings))
	for _, f := range findings {
		items = append(items, map[string]any{
			"kind": f.Kind, "names": f.Names, "detail": f.Detail,
		})
		next = append(next, e.reviewLead(f))
	}

	e.emit(name, true, map[string]any{
		"canonical_root": cfg.CanonicalRoot,
		"count":          len(findings),
		"findings":       items,
	}, warnParseErrors(perrs), nil, next)
	return exitOK
}

// reviewLead keeps review and reconcile on the same validated preview path.
// One curate invocation considers the whole corpus, including every finding.
func (e *env) reviewLead(f review.Finding) NextStep {
	cmd := f.Suggested
	if f.Kind == "near-duplicate" {
		cmd = "engram curate --json"
		if e.config != "" {
			cmd += " --config " + agentexec.ShellQuote(e.config)
		}
	}
	return NextStep{Reason: f.Detail, Command: cmd}
}

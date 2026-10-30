package cli

import (
	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/curate"
)

// A supplied proposal is validated against the current corpus, not assumed to
// have been authored against it. Only saving and reviewing a bound plan makes
// it eligible for later application through the existing --plan path.
func cmdSuppliedProposal(e *env, name string, pa parsedArgs) int {
	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	out := pa.last("--save-plan")
	if out != "" {
		if _, rerr := checkPlanOut(out, cfg.CanonicalRoot); rerr != nil {
			e.emit(name, false, nil, nil, rerr, nil)
			return exitUsage
		}
	}
	path := pa.last("--proposal")
	raw, err := readInput(path)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "proposal_read", Message: err.Error()}, nil)
		return exitError
	}
	data := map[string]any{"proposal": planInfo(path, raw), "apply": false}
	p, err := curate.ParseProposalJSON(raw)
	if err != nil {
		e.emit(name, false, data, nil, &RespError{Code: "parse_proposal", Message: err.Error()}, nil)
		return exitUsage
	}
	captured, warns, rerr := e.captureCorpus(cfg.CanonicalRoot, out != "")
	if rerr == nil && captured.parseErrors > 0 {
		rerr = corpusParseError(captured.parseErrors)
	}
	if rerr != nil {
		e.emit(name, false, data, warns, rerr, nil)
		return exitError
	}
	results := curate.Validate(p.Operations, captured.mems)
	data["operations"], data["count"] = opResultItems(results), len(results)
	input := agentexec.ShellQuote(path)
	if path == "-" {
		input = "<proposal file>"
	}
	if !curate.AllValid(results) {
		steps := curateNextSteps(results)
		for i := range steps {
			steps[i].Command = "fix the supplied proposal, then engram curate --proposal " + input + " --json"
		}
		e.emit(name, false, data, warns, &RespError{Code: "invalid_proposal", Message: "the supplied batch contains invalid operations; nothing saved or applied"}, steps)
		return exitConflicts
	}
	if out != "" {
		return e.savePlan(name, out, cfg.CanonicalRoot, captured.binding, nil, results, data, warns)
	}
	e.emit(name, true, data, warns, nil, []NextStep{{
		Reason:  "validation kept no binding; save a new plan against current canonical, review it, then apply that plan",
		Command: "engram curate --proposal " + input + " --save-plan <plan file>, review it, then engram curate --plan <plan file> --apply",
	}})
	return exitOK
}

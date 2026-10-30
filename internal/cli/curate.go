package cli

import (
	"errors"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/curate"
	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/review"
)

// cmdCurate runs the proposer/applier loop: engram gathers the canonical corpus
// (deterministic), a headless agent proposes operations over it (judgment), and
// engram validates and — under --apply — applies them (deterministic). The agent
// never touches a file; engram is the sole mutator, and a batch with any invalid
// operation is refused whole (fail closed).
//
// --save-plan keeps a dry run's validated batch in a file for review, and
// --plan applies such a file later without running an agent (see
// cmdCuratePlan), so what is committed is exactly what was reviewed.
func cmdCurate(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *curateArgs)
	if rerr == nil {
		rerr = checkPlanFlags(e, pa)
	}
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	if planIn := pa.last("--plan"); planIn != "" {
		return cmdCuratePlan(e, name, planIn)
	}
	if pa.last("--proposal") != "" {
		return cmdSuppliedProposal(e, name, pa)
	}
	planOut := pa.last("--save-plan")
	harness := config.HarnessClaude
	if h := pa.last("--harness"); h != "" {
		harness = h
	}
	modelOverride, effortOverride := pa.last("--model"), pa.last("--effort")
	if harness != config.HarnessClaude && harness != config.HarnessCodex {
		e.emit(name, false, nil, nil, &RespError{Code: "unknown_harness", Message: "harness must be claude-code or codex"}, nil)
		return exitUsage
	}

	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	// A plan is bound to the store the agent is shown, so --save-plan checks
	// its output path and binds the store before the (slow) agent runs.
	if planOut != "" {
		if _, rerr := checkPlanOut(planOut, cfg.CanonicalRoot); rerr != nil {
			e.emit(name, false, nil, nil, rerr, nil)
			return exitUsage
		}
	}
	captured, warns, rerr := e.captureCorpus(cfg.CanonicalRoot, planOut != "")
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}
	mems, binding := captured.mems, captured.binding

	// Resolve the model/effort: config default, overridden per-run by flags.
	choice := cfg.CurateModel(harness)
	if modelOverride != "" {
		choice.Model = modelOverride
	}
	if effortOverride != "" {
		choice.Effort = effortOverride
	}
	opts := agentexec.Options{Model: choice.Model, Effort: choice.Effort}

	// Resolve the deadline: --timeout, else config, else the built-in default.
	timeout, terr := cfg.CurateTimeout()
	if t := pa.last("--timeout"); t != "" {
		timeout, terr = config.ParseTimeout(t, 0)
	}
	if terr != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "invalid_timeout", Message: terr.Error()}, nil)
		return exitUsage
	}

	prompt, err := curate.BuildPrompt(curate.Corpus{Memories: mems, Findings: review.Analyze(mems)})
	if err != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "build_prompt", Message: err.Error()}, nil)
		return exitError
	}

	var inv agentexec.Invocation
	switch harness {
	case config.HarnessClaude:
		inv = agentexec.ClaudeInvocation(prompt, opts)
	case config.HarnessCodex:
		inv = agentexec.CodexInvocation(prompt, opts)
	}
	invocation := map[string]any{
		"harness": harness, "model": choice.Model, "effort": choice.Effort, "corpus_size": len(mems),
		"timeout": timeout.String(),
	}

	stdout, err := e.runnerFor(timeout)(inv)
	if err != nil {
		e.emit(name, false, map[string]any{"invocation": invocation}, warns,
			&RespError{Code: "agent_run", Message: err.Error()}, nil)
		return exitError
	}

	var text string
	switch harness {
	case config.HarnessClaude:
		text, err = agentexec.ExtractClaudeText(stdout)
	case config.HarnessCodex:
		text, err = agentexec.ExtractCodexText(stdout)
	}
	if err != nil {
		e.emit(name, false, map[string]any{"invocation": invocation, "raw_output": string(stdout)}, warns,
			&RespError{Code: "agent_output", Message: err.Error()}, nil)
		return exitError
	}

	proposal, err := curate.ParseProposal(text)
	if err != nil {
		e.emit(name, false, map[string]any{"invocation": invocation, "agent_output": text}, warns,
			&RespError{Code: "parse_proposal", Message: err.Error()}, nil)
		return exitError
	}

	results := curate.Validate(proposal.Operations, mems)
	data := map[string]any{
		"invocation": invocation,
		"apply":      e.apply,
		"operations": opResultItems(results),
		"count":      len(results),
	}

	if !e.apply {
		if !curate.AllValid(results) {
			warns = append(warns, "some proposed operations are invalid; --apply would refuse the whole batch")
			if planOut != "" {
				warns = append(warns, "no plan was saved")
			}
			e.emit(name, false, data, warns, nil, curateNextSteps(results))
			return exitConflicts
		}
		if planOut != "" {
			proposer := &curate.Proposer{Harness: harness, Model: choice.Model, Effort: choice.Effort}
			return e.savePlan(name, planOut, cfg.CanonicalRoot, binding, proposer, results, data, warns)
		}
		if len(results) > 0 {
			// This run kept no plan, and the agent is not deterministic: no
			// later command applies exactly these operations.
			e.emit(name, true, data, warns, nil, []NextStep{{
				Reason: "the proposed operations validated cleanly, but this dry run kept no plan and another run can propose differently; " +
					"to apply a batch after reviewing it, save a new proposal as a plan, review the plan, then apply that file (no agent runs)",
				Command: "engram curate --harness " + harness + " --save-plan <plan file>, review it, then engram curate --plan <plan file> --apply",
			}})
			return exitOK
		}
		e.emit(name, true, data, warns, nil, nil)
		return exitOK
	}

	// --apply: fail closed unless the entire batch is valid.
	if !curate.AllValid(results) {
		e.emit(name, false, data, warns,
			&RespError{Code: "invalid_proposal", Message: "refusing to apply: the batch contains invalid operations"},
			curateNextSteps(results))
		return exitConflicts
	}
	applied, aerr := curate.Apply(cfg.CanonicalRoot, proposal.Operations)
	return e.emitApplied(name, data, warns, applied, aerr)
}

// emitApplied reports a curate batch's outcome, from an agent's proposal or a
// saved plan alike.
func (e *env) emitApplied(name string, data map[string]any, warns []string, applied []curate.Applied, aerr error) int {
	data["applied"] = orEmpty(applied)
	if aerr == nil {
		e.emit(name, true, data, warns, nil, nil)
		return exitOK
	}
	var stale *curate.StaleError
	if errors.As(aerr, &stale) {
		return e.emitStale(name, data, warns, stale)
	}
	var partial *curate.PartialApplyError
	if errors.As(aerr, &partial) {
		data["unrestored"] = orEmpty(partial.Unrestored)
	}
	e.emit(name, false, data, warns, &RespError{Code: applyErrorCode(aerr), Message: aerr.Error()}, nil)
	return exitError
}

// applyErrorCode tells a failed batch that was rolled back (apply: canonical is
// unchanged) from one that could not be (apply_partial: data.unrestored lists
// each file that still differs from before the batch), and both from a batch
// refused the canonical lock before it read anything (locked, as for every
// canonical writer) or refused under it, before any write, because the store's
// tombstone directory is a symlink (linked_history).
func applyErrorCode(err error) string {
	var partial *curate.PartialApplyError
	switch {
	case errors.As(err, &partial):
		return "apply_partial"
	case errors.Is(err, lock.ErrLocked):
		return "locked"
	case errors.Is(err, curate.ErrLinkedHistory):
		return "linked_history"
	}
	return "apply"
}

func opResultItems(results []curate.OpResult) []map[string]any {
	items := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{
			"op":     r.Op.Op,
			"valid":  r.Valid,
			"reason": r.Op.Reason,
		}
		if r.Op.Name != "" {
			item["name"] = r.Op.Name
		}
		if len(r.Op.Sources) > 0 {
			item["sources"] = r.Op.Sources
		}
		if r.Op.ToScope != "" {
			item["to_scope"] = r.Op.ToScope
		}
		if r.Op.Memory != nil {
			// Surface the full proposed memory so a dry-run reviewer can judge
			// the content an add/update/merge would write, not just its name.
			item["memory_name"] = r.Op.Memory.Name
			item["memory"] = r.Op.Memory
		}
		if r.Error != "" {
			item["error"] = r.Error
		}
		items = append(items, item)
	}
	return items
}

// curateNextSteps turns each invalid operation into an agent-consumable lead so a
// caller knows exactly which proposals blocked the batch and why.
func curateNextSteps(results []curate.OpResult) []NextStep {
	var steps []NextStep
	for _, r := range results {
		if r.Valid {
			continue
		}
		target := r.Op.Name
		if target == "" && r.Op.Memory != nil {
			target = r.Op.Memory.Name
		}
		steps = append(steps, NextStep{
			Reason:  "invalid " + r.Op.Op + " operation (" + target + "): " + r.Error,
			Command: "re-run engram curate after the corpus changes, or fix the proposal manually",
		})
	}
	return steps
}

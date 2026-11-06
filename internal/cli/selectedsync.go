package cli

import (
	"fmt"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/sync"
)

type syncSelection struct {
	names   []string
	harness string
}

func parseSyncSelection(args []string) (syncSelection, *RespError) {
	pa, rerr := parseArgs(args, *syncArgs)
	if rerr != nil {
		return syncSelection{}, rerr
	}
	if len(pa.vals) == 0 {
		return syncSelection{}, nil
	}
	if len(pa.vals["--harness"]) != 1 || len(pa.vals["--name"]) == 0 {
		return syncSelection{}, usageError("selected sync requires --name NAME (repeatable) and exactly one --harness claude-code|codex")
	}
	h := pa.last("--harness")
	if h != config.HarnessClaude && h != config.HarnessCodex {
		return syncSelection{}, usageError("unsupported delivery harness %q; use claude-code or codex", h)
	}
	names, rerr := memoryNames(pa.vals["--name"], "selected sync requires at least one memory name")
	return syncSelection{names: names, harness: h}, rerr
}

// targets validates the complete selection before any render writes. Desired
// remains the full scope-filtered corpus; selection is a separate write bound,
// never a partial canonical view that could imply STALE removals.
func (sel syncSelection) targets(v canonView, targets []sync.Target) ([]sync.Target, *RespError) {
	byName := map[string]*schema.CanonicalMemory{}
	for _, m := range v.mems {
		byName[m.Name] = m
	}
	withheld := withheldNames(v.perrs)
	relevant := map[string]bool{}
	for _, tg := range targets {
		for _, m := range tg.DesiredMemories() {
			relevant[m.Name] = true
		}
	}
	names := map[string]bool{}
	for _, name := range sel.names {
		if why, ok := withheld[name]; ok {
			return nil, &RespError{Code: "withheld_memory", Message: fmt.Sprintf("selected memory %q is withheld: %s; nothing was delivered", name, why)}
		}
		if byName[name] == nil {
			return nil, &RespError{Code: "unknown_memory", Message: fmt.Sprintf("no canonical memory named %q; nothing was delivered", name)}
		}
		if !relevant[name] {
			return nil, &RespError{Code: "out_of_scope", Message: fmt.Sprintf("selected memory %q does not render to %s in this cwd/agent/host context; nothing was delivered", name, sel.harness)}
		}
		names[name] = true
	}

	var out []sync.Target
	for _, tg := range targets {
		admitted := false
		for _, m := range tg.DesiredMemories() {
			admitted = admitted || names[m.Name]
		}
		if !admitted {
			continue
		}
		switch t := tg.(type) {
		case sync.ClaudeTarget:
			t.Names = names
			out = append(out, t)
		case sync.ClaudeSharedTarget:
			t.Names = names
			out = append(out, t)
		case sync.CodexTarget:
			t.Names = names
			out = append(out, t)
		}
	}
	return out, nil
}

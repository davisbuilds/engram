package cli

import (
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

// cmdDetach marks imported memories as no longer tracking their native source:
// their origin gains the detached: prefix, so orphan detection passes them by
// and reconcile treats them as ordinary canonical memories (rendered into every
// harness, their former source included). Dry-run by default; --apply writes.
func cmdDetach(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *detachArgs)
	var names []string
	if rerr == nil {
		names, rerr = memoryNames(pa.pos, "usage: engram detach <name>... [--apply]")
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
	root := s.cfg.CanonicalRoot
	if e.apply {
		release, lerr := canonLock(root)
		if lerr != nil {
			e.emit(name, false, nil, nil, lerr, nil)
			return exitError
		}
		defer release()
	}

	var unknown []string
	var todo []*schema.CanonicalMemory
	items := make([]map[string]any, 0, len(names))
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
		item := map[string]any{"name": n, "path": path, "origin": m.Provenance.Origin, "outcome": "unchanged"}
		// Only an imported memory tracks a source; one already detached, or authored
		// directly, has nothing to detach.
		if originHarness(m) != "" {
			m.Provenance.Origin = schema.DetachedPrefix + m.Provenance.Origin
			item["origin"], item["outcome"] = m.Provenance.Origin, "detached"
			todo = append(todo, m)
		}
		items = append(items, item)
	}
	if len(unknown) > 0 {
		e.emit(name, false, nil, nil, &RespError{
			Code:    "unknown_memory",
			Message: "no canonical memory named " + strings.Join(unknown, ", ") + "; nothing was detached",
		}, nil)
		return exitUsage
	}
	data := map[string]any{"apply": e.apply, "memories": items}
	if !e.apply {
		var next []NextStep
		if len(todo) > 0 {
			next = []NextStep{{Reason: "dry run: nothing was written", Command: "engram detach " + strings.Join(names, " ") + " --apply"}}
		}
		e.emit(name, true, data, nil, nil, next)
		return exitOK
	}
	for _, m := range todo {
		if _, err := store.Replace(root, m); err != nil {
			e.emit(name, false, data, nil, &RespError{Code: "save", Message: err.Error()}, nil)
			return exitError
		}
	}
	e.emit(name, true, data, nil, nil, nil)
	return exitOK
}

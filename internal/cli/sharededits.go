package cli

import (
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
)

// Outcomes of an edited shared render judged against canonical.
const (
	editUnchanged = "unchanged" // canonical already says what the edit says
	editUpdated   = "updated"   // canonical takes the edit
	editConflict  = "conflict"  // both moved since the render; held for --refresh or --keep
	editInvalid   = "invalid"   // the edit fails validation; held for --keep
	editKept      = "kept"      // --keep: canonical stays, the edit is discarded
)

// sharedEditPlan is one edited shared render and what importing it does.
type sharedEditPlan struct {
	edit    sync.SharedEdit
	outcome string
	differs string
	// memory is canonical after the edit, for editUpdated.
	memory *schema.CanonicalMemory
}

// planSharedEdits judges each edited shared render against canonical by the
// render's stamp, the content engram last wrote there: canonical unchanged
// since the stamp takes the edit (a fast-forward), as does a name in refresh;
// canonical that moved too is a conflict; canonical that already says what the
// edit says needs nothing. Only description, type and body come from the edit.
func planSharedEdits(edits []sync.SharedEdit, canon []*schema.CanonicalMemory, refresh map[string]bool) []sharedEditPlan {
	byName := make(map[string]*schema.CanonicalMemory, len(canon))
	for _, m := range canon {
		byName[m.Name] = m
	}
	plans := make([]sharedEditPlan, 0, len(edits))
	for _, e := range edits {
		p := sharedEditPlan{edit: e}
		m := byName[e.Name]
		switch {
		case m == nil:
			p.outcome, p.differs = editConflict, "canonical removed"
		case e.Hash() == schema.NativeHash(m):
			p.outcome = editUnchanged
		default:
			next := *m
			next.Description, next.Type, next.Body = e.Description, e.Type, e.Body
			p.differs = strings.Join(contentDiff(m, &next), ",")
			switch {
			case next.Validate() != nil:
				p.outcome = editInvalid
			case schema.NativeHash(m) == e.Base || refresh[e.Name]:
				p.outcome, p.memory = editUpdated, &next
			default:
				p.outcome = editConflict
			}
		}
		plans = append(plans, p)
	}
	return plans
}

// contentDiff names the native-authored fields (description, type, body) that
// differ between a and b.
func contentDiff(a, b *schema.CanonicalMemory) []string {
	var d []string
	for _, f := range store.Diff(a, b) {
		if f == "description" || f == "type" || f == "body" {
			d = append(d, f)
		}
	}
	return d
}

// sharedEditRows are the plans as import rows.
func sharedEditRows(plans []sharedEditPlan) []map[string]string {
	rows := make([]map[string]string, 0, len(plans))
	for _, p := range plans {
		row := map[string]string{"name": p.edit.Name, "outcome": p.outcome}
		if p.differs != "" {
			row["differs"] = p.differs
		}
		rows = append(rows, row)
	}
	return rows
}

// sharedEditNextSteps offers each held edit its resolutions: take the edit into
// canonical, or keep canonical and discard the edit.
func sharedEditNextSteps(plans []sharedEditPlan) []NextStep {
	var next []NextStep
	for _, p := range plans {
		n := p.edit.Name
		switch p.outcome {
		case editConflict:
			if p.differs != "canonical removed" {
				next = append(next, NextStep{
					Reason:  "the shared render of " + n + " was edited and canonical changed too (" + p.differs + "); take the edit",
					Command: "engram import claude-code --shared --refresh " + n + " --apply",
				})
			}
			next = append(next, NextStep{
				Reason:  "or keep canonical " + n + " and discard the edit to its shared render",
				Command: "engram import claude-code --shared --keep " + n + " --apply",
			})
		case editInvalid:
			next = append(next, NextStep{
				Reason:  "the edit to " + n + "'s shared render is not a valid memory; fix it in place, or discard it",
				Command: "engram import claude-code --shared --keep " + n + " --apply",
			})
		}
	}
	return next
}

// applySharedEdits saves the planned fast-forwards under the canonical lock the
// caller holds, re-checked against canonical as it is now: an edit planned
// against a canonical that has since moved is not saved, and is marked a
// conflict in plans so the caller reports it as held.
func applySharedEdits(root string, plans []sharedEditPlan, refresh map[string]bool) error {
	for i, p := range plans {
		if p.outcome != editUpdated {
			continue
		}
		cur, _, found, err := store.Load(root, p.edit.Name)
		if err != nil {
			return err
		}
		if !found || schema.NativeHash(cur) != p.edit.Base && !refresh[p.edit.Name] {
			plans[i].outcome, plans[i].memory = editConflict, nil
			plans[i].differs = "canonical changed during the run"
			if !found {
				plans[i].differs = "canonical removed"
			}
			continue
		}
		next := *cur
		next.Description, next.Type, next.Body = p.edit.Description, p.edit.Type, p.edit.Body
		if _, _, err := store.Save(root, &next, true); err != nil {
			return err
		}
	}
	return nil
}

// sharedEditPlans scans Claude's shared dir for edited renders and plans them
// against canon. Nothing to plan when Claude is disabled.
func (s *session) sharedEditPlans(canon []*schema.CanonicalMemory, refresh map[string]bool) ([]sharedEditPlan, *RespError) {
	h := s.cfg.Harnesses[config.HarnessClaude]
	if !h.Enabled() {
		return nil, nil
	}
	edits, err := sync.ScanSharedEdits(sharedTarget(h.Home, nil, false).Dir)
	if err != nil {
		return nil, &RespError{Code: "import", Message: err.Error()}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Name < edits[j].Name })
	return planSharedEdits(edits, canon, refresh), nil
}

// withSharedEdits replaces each memory a plan updates with its edited form.
func withSharedEdits(mems []*schema.CanonicalMemory, plans []sharedEditPlan) []*schema.CanonicalMemory {
	updated := map[string]*schema.CanonicalMemory{}
	for _, p := range plans {
		if p.outcome == editUpdated {
			updated[p.edit.Name] = p.memory
		}
	}
	out := make([]*schema.CanonicalMemory, 0, len(mems))
	for _, m := range mems {
		if u, ok := updated[m.Name]; ok {
			m = u
		}
		out = append(out, m)
	}
	return out
}

func countOutcome(plans []sharedEditPlan, outcome string) int {
	n := 0
	for _, p := range plans {
		if p.outcome == outcome {
			n++
		}
	}
	return n
}

// cmdImportShared imports edits made in place to Claude's shared renders: an
// edit whose canonical has not moved since the render is taken, --refresh takes
// a conflicting one anyway, and --keep discards one, keeping canonical. Under
// --apply it saves what it takes, then re-syncs the shared dir, so taken edits
// are restamped and kept ones are overwritten with canonical.
func cmdImportShared(e *env, name string, s *session, refresh, keep map[string]bool) int {
	h := s.cfg.Harnesses[config.HarnessClaude]
	if !h.Enabled() {
		e.emit(name, false, nil, nil, &RespError{Code: "harness_disabled", Message: "claude-code is disabled; cannot import from it"}, nil)
		return exitUsage
	}
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "discover", Message: err.Error()}, nil)
		return exitError
	}
	warns := warnParseErrors(perrs)
	plans, rerr := s.sharedEditPlans(mems, refresh)
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}
	edited := map[string]bool{}
	for i, p := range plans {
		edited[p.edit.Name] = true
		// A kept edit is never taken, even one canonical could fast-forward to.
		if keep[p.edit.Name] {
			plans[i].outcome, plans[i].memory = editKept, nil
		}
	}
	for _, set := range []map[string]bool{refresh, keep} {
		for n := range set {
			if !edited[n] {
				e.emit(name, false, nil, warns, &RespError{Code: "not_edited", Message: n + " has no edited shared render"}, nil)
				return exitUsage
			}
		}
	}
	data := map[string]any{"harness": sync.SharedHarness, "apply": e.apply}
	if e.apply {
		if e.beforeApplyLock != nil {
			e.beforeApplyLock()
		}
		release, lerr := canonLock(s.cfg.CanonicalRoot)
		if lerr != nil {
			e.emit(name, false, data, warns, lerr, nil)
			return exitError
		}
		serr := applySharedEdits(s.cfg.CanonicalRoot, plans, refresh)
		release()
		if serr != nil {
			e.emit(name, false, data, warns, &RespError{Code: "save", Message: serr.Error()}, nil)
			return exitError
		}
		now, perrs, derr := discover.Discover(s.cfg.CanonicalRoot)
		if derr != nil {
			e.emit(name, false, data, warns, &RespError{Code: "discover", Message: derr.Error()}, nil)
			return exitError
		}
		keepStale, hwarns := staleHold(s.cfg.CanonicalRoot, perrs)
		warns = append(warns, hwarns...)
		agent := s.agentFor("claude")
		tg := sharedTarget(h.Home, scope.Shared(now, agent, s.host), keepStale)
		tg.Discard = keep
		res, aerr := tg.Apply()
		if aerr != nil {
			e.emit(name, false, data, warns, &RespError{Code: "sync", Message: aerr.Error()}, nil)
			return exitError
		}
		data["sync"] = res
	}
	// Rows and held edits come after the apply, which marks an edit whose
	// fast-forward failed its recheck as a conflict.
	data["results"] = sharedEditRows(plans)
	var held []sharedEditPlan
	for _, p := range plans {
		if p.outcome == editConflict || p.outcome == editInvalid {
			held = append(held, p)
		}
	}
	exit := exitOK
	if len(held) > 0 {
		exit = exitConflicts
	}
	e.emit(name, exit == exitOK, data, warns, nil, sharedEditNextSteps(held))
	return exit
}

// fromCanonical replaces each memory with an edited shared render by its
// current canonical form, so propagation renders exactly what canonical holds
// after the apply, whether or not a fast-forward survived its recheck.
func fromCanonical(mems, present []*schema.CanonicalMemory, plans []sharedEditPlan) []*schema.CanonicalMemory {
	touched := map[string]bool{}
	for _, p := range plans {
		touched[p.edit.Name] = true
	}
	if len(touched) == 0 {
		return mems
	}
	now := map[string]*schema.CanonicalMemory{}
	for _, m := range present {
		if touched[m.Name] {
			now[m.Name] = m
		}
	}
	out := make([]*schema.CanonicalMemory, 0, len(mems))
	for _, m := range mems {
		if c, ok := now[m.Name]; ok {
			m = c
		}
		out = append(out, m)
	}
	return out
}

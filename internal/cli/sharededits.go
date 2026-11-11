package cli

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/sync"
	"github.com/davisbuilds/engram/internal/tombstone"
)

// Outcomes of an edited shared render judged against canonical, in the import
// row vocabulary (importoutcome.go).
const (
	editUnchanged = store.Unchanged // canonical already says what the edit says
	editUpdated   = store.Updated   // canonical takes the edit
	editConflict  = store.Conflict  // held: both moved, canonical is withheld, or canonical is removed
	editInvalid   = outcomeInvalid  // the edit fails validation; held for --keep
	editKept      = outcomeKept     // --keep: canonical stays, the edit is discarded
	editForgotten = outcomeForgotten
)

// Why a conflict has no canonical memory to compare with, when it has none.
const (
	// canonicalRemoved: canonical, read whole, has no memory by the name and
	// no tombstone for it.
	canonicalRemoved = "removed"
	// canonicalUnknown: canonical was not read whole (its root is missing, or a
	// file that does not load may be this memory), so its absence is unproven.
	canonicalUnknown = "unknown"
)

// sharedEditPlan is one edited shared render and what importing it does. A
// conflict says why in exactly one of differs (the fields both sides changed),
// withheld (a canonical file claims the name but does not load) or canonical
// (canonicalRemoved or canonicalUnknown).
type sharedEditPlan struct {
	edit      sync.SharedEdit
	outcome   store.Outcome
	differs   []string
	withheld  string
	canonical string
	// invalid is the validation error, for editInvalid.
	invalid error
	// memory is canonical after the edit, for editUpdated.
	memory *schema.CanonicalMemory
}

// held reports whether the edit still waits on the operator: its render stays
// in place (sync holds it as a CONFLICT) until it is taken or discarded. A
// forgotten memory's edit is not a conflict in canonical, but its render is
// held all the same while it exists (a forget's purge may have removed it).
func (p sharedEditPlan) held() bool {
	switch p.outcome {
	case editConflict, editInvalid:
		return true
	case editForgotten:
		_, err := os.Lstat(p.edit.Path)
		return err == nil
	}
	return false
}

// discardable reports whether --keep can settle the edit: there is a known
// canonical state to keep (its memory, its removal, or its tombstone). A
// withheld or unknown canonical has none until it loads again, so the edit is
// never discarded meanwhile.
func (p sharedEditPlan) discardable() bool {
	return p.withheld == "" && p.canonical != canonicalUnknown
}

// absentState is the outcome for an edit whose canonical memory is not in a
// read of canonical: forgotten if a tombstone holds the name, else a conflict
// that is withheld, unknown (the read was incomplete) or removed.
func (p *sharedEditPlan) absentState(withheld map[string]string, tombs tombstone.Set, incomplete bool) {
	p.memory, p.differs = nil, nil
	_, forgotten := tombs.Latest(p.edit.Name)
	switch {
	case forgotten:
		p.outcome = editForgotten
	case withheld[p.edit.Name] != "":
		p.outcome, p.withheld = editConflict, withheld[p.edit.Name]
	case incomplete:
		p.outcome, p.canonical = editConflict, canonicalUnknown
	default:
		p.outcome, p.canonical = editConflict, canonicalRemoved
	}
}

// planSharedEdits judges each edited shared render against canonical by the
// render's stamp, the content engram last wrote there. With no canonical
// memory by the name, the gates decide (absentState): a tombstone (forgotten),
// a withheld canonical file or an incomplete read of canonical (conflicts that
// neither --refresh nor --keep settles), else a removed conflict. Then canonical unchanged since the stamp takes the edit
// (a fast-forward), as does a name in refresh; canonical that moved too is a
// conflict; canonical that already says what the edit says needs nothing.
// Only description, type and body come from the edit.
func planSharedEdits(edits []sync.SharedEdit, canon []*schema.CanonicalMemory, withheld map[string]string, tombs tombstone.Set, incomplete bool, refresh map[string]bool) []sharedEditPlan {
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
			p.absentState(withheld, tombs, incomplete)
		case e.Hash() == schema.NativeHash(m):
			p.outcome = editUnchanged
		default:
			next := *m
			next.Description, next.Type, next.Body = e.Description, e.Type, e.Body
			p.differs = contentDiff(m, &next)
			switch verr := next.Validate(); {
			case verr != nil:
				p.outcome, p.invalid = editInvalid, verr
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
		row := map[string]string{"name": p.edit.Name, "outcome": string(p.outcome)}
		switch {
		case p.withheld != "":
			row["withheld"] = p.withheld
		case p.canonical != "":
			row["canonical"] = p.canonical
		case p.invalid != nil:
			row["error"] = p.invalid.Error()
		}
		if len(p.differs) > 0 {
			row["differs"] = strings.Join(p.differs, ",")
		}
		rows = append(rows, row)
	}
	return rows
}

// sharedEditNextSteps offers each held edit its resolutions: take the edit into
// canonical, keep canonical and discard the edit, or fix what blocks both.
func sharedEditNextSteps(plans []sharedEditPlan) []NextStep {
	var next []NextStep
	keep := func(n, reason string) {
		next = append(next, NextStep{Reason: reason, Command: "engram import claude-code --shared --keep " + n + " --apply"})
	}
	for _, p := range plans {
		n := p.edit.Name
		switch {
		case p.withheld != "":
			next = append(next, NextStep{
				Reason:  "the shared render of " + n + " was edited, but the canonical file claiming " + n + " does not load (" + p.withheld + "); fix that file, then settle the edit",
				Command: "engram import claude-code --shared",
			})
		case p.canonical == canonicalUnknown:
			next = append(next, NextStep{
				Reason:  "the shared render of " + n + " was edited, but canonical could not be read whole (its root is missing or a file does not load); restore it, then settle the edit",
				Command: "engram import claude-code --shared",
			})
		case p.canonical == canonicalRemoved:
			keep(n, "the shared render of "+n+" was edited, but canonical has no memory "+n+"; discard the edit (author it again with engram remember to keep it)")
		case p.outcome == editForgotten:
			keep(n, n+" was forgotten, but its shared render was edited since; discard the edit (engram forget --restore "+n+" brings the memory back instead)")
		case p.outcome == editConflict:
			next = append(next, NextStep{
				Reason:  "the shared render of " + n + " was edited and canonical changed too (" + strings.Join(p.differs, ",") + "); take the edit",
				Command: "engram import claude-code --shared --refresh " + n + " --apply",
			})
			keep(n, "or keep canonical "+n+" and discard the edit to its shared render")
		case p.outcome == editInvalid:
			keep(n, "the edit to "+n+"'s shared render is not a valid memory ("+p.invalid.Error()+"); fix it in place, or discard it")
		}
	}
	return next
}

// applySharedEdits saves the planned fast-forwards under the canonical lock the
// caller holds, re-checked against canonical as it is now: an edit planned
// against a canonical that has since moved, been removed, forgotten or stopped
// loading is not saved, and is re-planned so the caller reports it as it is.
// tombs must be read under the same lock; incomplete is whether the caller's
// read of canonical was.
func applySharedEdits(root string, plans []sharedEditPlan, tombs tombstone.Set, incomplete bool, refresh map[string]bool) error {
	for i, p := range plans {
		if p.outcome != editUpdated {
			continue
		}
		cur, _, found, err := store.Load(root, p.edit.Name)
		if errors.Is(err, store.ErrWithheld) {
			plans[i].absentState(map[string]string{p.edit.Name: err.Error()}, tombstone.Set{}, incomplete)
			continue
		}
		if err != nil {
			return err
		}
		if !found {
			plans[i].absentState(nil, tombs, incomplete)
			continue
		}
		next := *cur
		next.Description, next.Type, next.Body = p.edit.Description, p.edit.Type, p.edit.Body
		if schema.NativeHash(cur) == schema.NativeHash(&next) {
			// Canonical moved to what the edit says: nothing left to take.
			plans[i].outcome, plans[i].memory, plans[i].differs = editUnchanged, nil, nil
			continue
		}
		if schema.NativeHash(cur) != p.edit.Base && !refresh[p.edit.Name] {
			plans[i].outcome, plans[i].memory, plans[i].differs = editConflict, nil, contentDiff(cur, &next)
			continue
		}
		if _, _, err := store.Save(root, &next, true); err != nil {
			return err
		}
	}
	return nil
}

// sharedEditPlans scans Claude's shared dir for edited renders and plans them
// against a read of canonical (its memories, the files withheld from it, and
// whether it was read whole) and the tombstones. Nothing to plan when Claude
// is disabled.
func (s *session) sharedEditPlans(v canonView, tombs tombstone.Set, refresh map[string]bool) ([]sharedEditPlan, *RespError) {
	h := s.cfg.Harnesses[config.HarnessClaude]
	if !h.Enabled() {
		return nil, nil
	}
	edits, err := sync.ScanSharedEdits(sharedTarget(h.Home, nil, false).Dir)
	if err != nil {
		return nil, &RespError{Code: "import", Message: err.Error()}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Name < edits[j].Name })
	return planSharedEdits(edits, v.mems, withheldNames(v.perrs), tombs, v.keepStale, refresh), nil
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

func countOutcome(plans []sharedEditPlan, outcome store.Outcome) int {
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
// --apply it saves what it takes, then re-syncs the shared dir, both under one
// canonical lock, so taken edits are restamped and kept ones are overwritten
// with canonical.
func cmdImportShared(e *env, name string, s *session, refresh, keep map[string]bool) int {
	h := s.cfg.Harnesses[config.HarnessClaude]
	if !h.Enabled() {
		e.emit(name, false, nil, nil, &RespError{Code: "harness_disabled", Message: "claude-code is disabled; cannot import from it"}, nil)
		return exitUsage
	}
	pre, rerr := readCanon(s.cfg.CanonicalRoot, rootAbsent(s.cfg.CanonicalRoot))
	if rerr != nil {
		e.emit(name, false, nil, pre.warns, rerr, nil)
		return exitError
	}
	warns := pre.warns
	tombs, terr := tombstone.Load(s.cfg.CanonicalRoot)
	if terr != nil {
		e.emit(name, false, nil, warns, &RespError{Code: "tombstones", Message: terr.Error()}, nil)
		return exitError
	}
	plans, rerr := s.sharedEditPlans(pre, tombs, refresh)
	if rerr != nil {
		e.emit(name, false, nil, warns, rerr, nil)
		return exitError
	}
	edited := map[string]bool{}
	for i, p := range plans {
		edited[p.edit.Name] = true
		// A kept edit is never taken, even one canonical could fast-forward
		// to; one whose canonical is withheld is never discarded either.
		if keep[p.edit.Name] && p.discardable() {
			plans[i] = sharedEditPlan{edit: p.edit, outcome: editKept}
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
		root := s.cfg.CanonicalRoot
		absent := rootAbsent(root) // before the lock creates it
		release, lerr := canonLock(root)
		if lerr != nil {
			e.emit(name, false, data, warns, lerr, nil)
			return exitError
		}
		// Held through the re-sync, so it renders canonical as the saves left
		// it, with no writer landing between that read and the render.
		defer release()
		if tombs, terr = tombstone.Load(root); terr != nil {
			e.emit(name, false, data, warns, &RespError{Code: "tombstones", Message: terr.Error()}, nil)
			return exitError
		}
		if serr := applySharedEdits(root, plans, tombs, pre.keepStale || absent, refresh); serr != nil {
			e.emit(name, false, data, warns, &RespError{Code: "save", Message: serr.Error()}, nil)
			return exitError
		}
		now, rerr := readCanon(root, false)
		if rerr != nil {
			e.emit(name, false, data, append(warns, now.warns...), rerr, nil)
			return exitError
		}
		if absent {
			now.holdAbsent(root)
		}
		warns = now.warns
		_, tg := s.claudeSplit(h, now.mems, s.agentFor("claude"), now.keepStale)
		// Discard only what the locked read still vouches for: a kept edit
		// whose canonical memory this read lacks is re-planned, and is held
		// if that read was incomplete.
		live := map[string]bool{}
		for _, m := range now.mems {
			live[m.Name] = true
		}
		tg.Discard = map[string]bool{}
		for i, p := range plans {
			if p.outcome != editKept {
				continue
			}
			if !live[p.edit.Name] {
				plans[i].absentState(withheldNames(now.perrs), tombs, now.keepStale)
				if plans[i].discardable() {
					plans[i].outcome, plans[i].canonical = editKept, ""
				}
			}
			if plans[i].outcome == editKept {
				tg.Discard[p.edit.Name] = true
			}
		}
		e.at(hookSnapshot)
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
		if p.held() {
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

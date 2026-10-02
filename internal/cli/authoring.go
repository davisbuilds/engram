package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
	"github.com/davisbuilds/engram/internal/tombstone"
)

// multiFlag collects a repeatable string flag (e.g. --applies-cwd a --applies-cwd b).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func cmdRemember(e *env, name string, args []string) int {
	fs := flag.NewFlagSet("remember", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		nm       = fs.String("name", "", "memory name (kebab-case)")
		desc     = fs.String("description", "", "one-line description")
		typ      = fs.String("type", "", "memory type")
		scp      = fs.String("scope", "global", "scope tier")
		body     = fs.String("body", "", "markdown body")
		fromJSON = fs.String("from-json", "", "read a full memory as JSON from this path (- for stdin)")
		force    = fs.Bool("force", false, "overwrite a differing canonical memory of the same name")
	)
	var cwds, agents, hosts, related multiFlag
	fs.Var(&cwds, "applies-cwd", "cwd glob (repeatable)")
	fs.Var(&agents, "applies-agent", "agent filter (repeatable)")
	fs.Var(&hosts, "applies-host", "host label (repeatable)")
	fs.Var(&related, "related", "related memory name (repeatable)")
	if err := fs.Parse(args); err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: err.Error()}, nil)
		return exitUsage
	}
	if fs.NArg() > 0 {
		e.emit(name, false, nil, nil, usageError("unexpected argument %q", fs.Arg(0)), nil)
		return exitUsage
	}

	m, rerr := buildMemory(*fromJSON, *nm, *desc, *typ, *scp, *body, cwds, agents, hosts, related)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	if m.AppliesTo.Cwd, rerr = expandCwdGlobs(m.AppliesTo.Cwd); rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	if m.Provenance.Origin == "" {
		m.Provenance.Origin = "remember"
	}
	// The merge base is import's to record; an authored memory never carries
	// one, so remember cannot fast-forward past a differing memory (see
	// store.Plan) and still needs --force to overwrite it.
	m.Provenance.ImportHash, m.Provenance.ImportSource = "", ""
	if err := m.Validate(); err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "invalid_memory", Message: err.Error()}, nil)
		return exitUsage
	}

	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	release, lerr := canonLock(cfg.CanonicalRoot)
	if lerr != nil {
		e.emit(name, false, nil, nil, lerr, nil)
		return exitError
	}
	defer release()
	outcome, path, err := store.Save(cfg.CanonicalRoot, m, *force)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "save", Message: err.Error()}, nil)
		return exitError
	}
	data := map[string]any{"outcome": outcome, "name": m.Name, "scope": m.Scope, "path": path}
	if outcome == store.Conflict {
		e.emit(name, false, data, nil,
			&RespError{Code: "canonical_conflict", Message: "a different canonical memory named " + m.Name + " exists; pass --force to overwrite intentionally"},
			[]NextStep{{
				Reason:  "a differing canonical memory of this name already exists",
				Command: "re-run engram remember with --force to overwrite it intentionally",
			}})
		return exitConflicts
	}
	e.emit(name, true, data, nil, nil, nil)
	return exitOK
}

func cmdShare(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *shareArgs)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	memName, to := "", pa.last("--to")
	if len(pa.pos) == 1 {
		memName = pa.pos[0]
	}
	cwds, anyCwd := pa.vals["--applies-cwd"], pa.bools["--any-cwd"]
	if memName == "" || to == "" && len(cwds) == 0 && !anyCwd {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: "usage: engram share <name> [--to <scope>] [--applies-cwd <glob>]... [--any-cwd]"}, nil)
		return exitUsage
	}
	if len(cwds) > 0 && anyCwd {
		e.emit(name, false, nil, nil, usageError("--applies-cwd and --any-cwd are mutually exclusive"), nil)
		return exitUsage
	}
	cwds, rerr = expandCwdGlobs(cwds)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}

	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	// Lock across load-modify-save so a concurrent writer cannot slip between
	// reading the current scope and writing the moved one.
	release, lerr := canonLock(cfg.CanonicalRoot)
	if lerr != nil {
		e.emit(name, false, nil, nil, lerr, nil)
		return exitError
	}
	defer release()
	m, _, found, err := store.Load(cfg.CanonicalRoot, memName)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "load", Message: err.Error()}, nil)
		return exitError
	}
	if !found {
		e.emit(name, false, nil, nil, &RespError{Code: "not_found", Message: "no canonical memory named " + memName}, nil)
		return exitUsage
	}
	from, fromCwd := m.Scope, m.AppliesTo.Cwd
	if to != "" {
		m.Scope = to
	}
	switch {
	case anyCwd:
		m.AppliesTo.Cwd = nil
	case len(cwds) > 0:
		m.AppliesTo.Cwd = cwds
	}
	if err := m.Validate(); err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "invalid_scope", Message: err.Error()}, nil)
		return exitUsage
	}
	// Sharing is a deliberate edit, so it overwrites its own canonical file.
	outcome, path, err := store.Save(cfg.CanonicalRoot, m, true)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "save", Message: err.Error()}, nil)
		return exitError
	}
	e.emit(name, true, map[string]any{
		"outcome": outcome, "name": memName, "from_scope": from, "to_scope": m.Scope,
		"from_applies_cwd": orEmpty(fromCwd), "to_applies_cwd": orEmpty(m.AppliesTo.Cwd), "path": path,
	}, nil, nil, nil)
	return exitOK
}

func cmdImport(e *env, name string, args []string) int {
	pa, perr := parseArgs(args, *importArgs)
	if perr != nil {
		e.emit(name, false, nil, nil, perr, nil)
		return exitUsage
	}
	var harness string
	if len(pa.pos) == 1 {
		harness = pa.pos[0]
	}
	all := pa.bools["--all"]
	refresh, keep := map[string]bool{}, map[string]bool{}
	for _, v := range pa.vals["--refresh"] {
		addRefresh(refresh, v)
	}
	for _, v := range pa.vals["--keep"] {
		addRefresh(keep, v)
	}
	for n := range keep {
		if refresh[n] {
			e.emit(name, false, nil, nil, usageError("%s cannot be both --refresh and --keep", n), nil)
			return exitUsage
		}
	}
	if harness == "" {
		e.emit(name, false, nil, nil, &RespError{Code: "usage", Message: "usage: engram import <claude-code|codex> [--all] [--refresh <name>]… [--keep <name>]… [--apply]"}, nil)
		return exitUsage
	}
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}

	var (
		res importer.Result
		err error
	)
	switch harness {
	case config.HarnessClaude:
		h := s.cfg.Harnesses[config.HarnessClaude]
		if !h.Enabled() {
			e.emit(name, false, nil, nil, &RespError{Code: "harness_disabled", Message: "claude-code is disabled; cannot import from it"}, nil)
			return exitUsage
		}
		if all {
			// Sweep every project slug, not just the current cwd's.
			res, err = importer.ImportClaudeAll(h.Home)
		} else {
			res, err = importer.ImportClaude(claudeMemoryDir(h.Home, s.cwd), s.cwd)
		}
	case config.HarnessCodex:
		h := s.cfg.Harnesses[config.HarnessCodex]
		if !h.Enabled() {
			e.emit(name, false, nil, nil, &RespError{Code: "harness_disabled", Message: "codex is disabled; cannot import from it"}, nil)
			return exitUsage
		}
		// Codex keeps a single consolidated MEMORY.md, so --all imports the same
		// source as a plain import; it is accepted but has no additional effect.
		res, err = importer.ImportCodex(filepath.Join(h.Home, "memories", "MEMORY.md"))
	default:
		e.emit(name, false, nil, nil, &RespError{Code: "unknown_harness", Message: "harness must be claude-code or codex"}, nil)
		return exitUsage
	}
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "import", Message: err.Error()}, nil)
		return exitError
	}

	base := map[string]any{
		"harness": harness, "apply": e.apply,
		"skipped": orEmpty(res.Skipped), "dropped": orEmpty(res.Dropped),
		"would_import": len(res.Memories),
	}
	var warns []string
	if res.StaleWarning {
		base["stale_warning"] = true
		warns = append(warns, "Codex MEMORY.md is older than 30 days; the consolidator may be stalled and this import may lag reality")
	}
	if len(res.Dropped) > 0 {
		warns = append(warns, fmt.Sprintf("%d source(s) could not be imported and were dropped; see data.dropped", len(res.Dropped)))
	}

	// Orphan detection needs the whole native source: Codex always has it, Claude
	// only on the all-slug scan (one slug cannot tell a deleted native from one
	// that lives in another slug).
	var next []NextStep
	if harness == config.HarnessCodex || all {
		canon, _, derr := discover.Discover(s.cfg.CanonicalRoot)
		if derr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "discover", Message: derr.Error()}, nil)
			return exitError
		}
		orphans, owarn := findOrphans(canon, harness, res)
		base["orphaned"] = orphans
		if owarn != "" {
			warns = append(warns, owarn)
		}
		next = orphanNextSteps(harness, orphans)
	}

	// A --refresh name that matches no candidate is a typo or a stale plan; refusing
	// it beats silently refreshing nothing. Checked before any write.
	if missing := unmatchedRefresh(refresh, res.Memories); len(missing) > 0 {
		e.emit(name, false, base, warns, &RespError{
			Code:    "unknown_refresh",
			Message: "--refresh names no imported memory: " + strings.Join(missing, ", "),
		}, nil)
		return exitUsage
	}
	if missing := unmatchedRefresh(keep, res.Memories); len(missing) > 0 {
		e.emit(name, false, base, warns, &RespError{
			Code:    "unknown_keep",
			Message: "--keep names no imported memory: " + strings.Join(missing, ", "),
		}, nil)
		return exitUsage
	}

	if !e.apply {
		// Dry-run: resolve scope against current canonical (unlocked — this is a
		// projection, not a write) so the preview shows the scope apply will land on
		// and any preservation notes. See decideImportScope.
		items := make([]map[string]string, 0, len(res.Memories))
		// Apply saves candidates in order, so a later candidate sharing a name
		// meets what an earlier one wrote; the preview threads the same state.
		batch := map[string]*schema.CanonicalMemory{}
		tombs, terr := tombstone.Load(s.cfg.CanonicalRoot)
		if terr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "tombstones", Message: terr.Error()}, nil)
			return exitError
		}
		for _, m := range res.Memories {
			if tombs.Blocks(m) {
				items = append(items, forgottenRow(m.Name))
				continue
			}
			if row, ok := withheldRow(s.cfg.CanonicalRoot, m); ok {
				items = append(items, row)
				continue
			}
			force, note, derr := resolveImportScope(s.cfg.CanonicalRoot, m, res.ScopeAuthoritative)
			if derr != nil {
				e.emit(name, false, base, warns, &RespError{Code: "load", Message: derr.Error()}, nil)
				return exitError
			}
			if note != "" {
				warns = append(warns, note)
			}
			// Say what apply would do with each candidate, not just that it exists.
			// The row is built after scope resolution so it carries the scope apply
			// lands on, and `force` is honored so a scope-only revision that apply
			// forces is previewed as `updated`, not a conflict.
			stored, _, _, lerr := store.Load(s.cfg.CanonicalRoot, m.Name)
			if lerr != nil {
				e.emit(name, false, base, warns, &RespError{Code: "load", Message: lerr.Error()}, nil)
				return exitError
			}
			if prev, ok := batch[m.Name]; ok {
				stored = prev
			}
			outcome, planned := store.Plan(stored, m)
			overridable := outcome == store.Conflict || outcome == store.CanonicalAhead
			if outcome == store.Created || outcome == store.Updated || (overridable && (force || refresh[m.Name])) {
				if overridable {
					planned = m // a forced or refreshed save writes the candidate
				}
				batch[m.Name] = planned
			}
			entry := outcomeEntry(m.Name, outcome, stored, m)
			switch {
			case keep[m.Name] && outcome == store.Conflict && keepable(stored, m):
				entry = outcomeEntry(m.Name, store.CanonicalAhead, stored, m)
			case keep[m.Name] && outcome == store.Conflict:
				warns = append(warns, keepDeclined(m.Name))
			case refresh[m.Name] && overridable:
				entry = refreshedEntry(m.Name, stored, m)
			case force && overridable:
				entry = outcomeEntry(m.Name, store.Updated, stored, m)
			}
			item := memoryItems([]*schema.CanonicalMemory{m})[0]
			for k, v := range entry {
				if k != "name" {
					item[k] = v
				}
			}
			items = append(items, item)
		}
		base["memories"] = items
		e.emit(name, true, base, warns, nil, next)
		return exitOK
	}

	release, lerr := canonLock(s.cfg.CanonicalRoot)
	if lerr != nil {
		e.emit(name, false, base, warns, lerr, nil)
		return exitError
	}
	defer release()

	tombs, terr := tombstone.Load(s.cfg.CanonicalRoot)
	if terr != nil {
		e.emit(name, false, base, warns, &RespError{Code: "tombstones", Message: terr.Error()}, nil)
		return exitError
	}
	outcomes := make([]map[string]string, 0, len(res.Memories))
	conflicts := 0
	for _, m := range res.Memories {
		// A forgotten memory stays forgotten; --force and --refresh do not
		// override a tombstone (forget --restore does).
		if tombs.Blocks(m) {
			outcomes = append(outcomes, forgottenRow(m.Name))
			continue
		}
		if verr := m.Validate(); verr != nil {
			outcomes = append(outcomes, map[string]string{"name": m.Name, "outcome": "invalid", "error": verr.Error()})
			continue
		}
		if row, ok := withheldRow(s.cfg.CanonicalRoot, m); ok {
			outcomes = append(outcomes, row)
			conflicts++
			continue
		}
		// Resolve scope per candidate *inside* the lock so a forced scope-only
		// revision decides against the serialized current state, never a stale read a
		// concurrent writer has since superseded. Per-candidate (not name-keyed) so
		// two natives normalizing to one name don't cross force decisions.
		force, note, derr := resolveImportScope(s.cfg.CanonicalRoot, m, res.ScopeAuthoritative)
		if derr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "load", Message: derr.Error()}, nil)
			return exitError
		}
		if note != "" {
			warns = append(warns, note)
		}
		stored, _, _, lerr := store.Load(s.cfg.CanonicalRoot, m.Name)
		if lerr != nil {
			e.emit(name, false, base, warns, &RespError{Code: "load", Message: lerr.Error()}, nil)
			return exitError
		}
		// --refresh names memories whose canonical content the operator has chosen
		// to replace with the native's; only those are forced, and only if they
		// actually conflict (the plan is taken before the write to record why).
		// --keep settles a conflict for canonical: keep its content and take the
		// native's current hash as the base, so it reads as canonical_ahead.
		if keep[m.Name] {
			pre, _ := store.Plan(stored, m)
			if pre == store.Conflict && !keepable(stored, m) {
				warns = append(warns, keepDeclined(m.Name))
			} else if pre == store.Conflict {
				kept := *stored
				kept.Provenance.ImportHash = m.Provenance.ImportHash
				if m.Provenance.ImportSource != "" {
					kept.Provenance.ImportSource = m.Provenance.ImportSource
				}
				if _, rerr := store.Replace(s.cfg.CanonicalRoot, &kept); rerr != nil {
					e.emit(name, false, base, nil, &RespError{Code: "save", Message: rerr.Error()}, nil)
					return exitError
				}
				outcomes = append(outcomes, outcomeEntry(m.Name, store.CanonicalAhead, stored, m))
				continue
			}
		}
		wasConflict := false
		if refresh[m.Name] {
			pre, _ := store.Plan(stored, m)
			wasConflict = pre == store.Conflict || pre == store.CanonicalAhead
			force = force || wasConflict
		}
		outcome, _, serr := store.Save(s.cfg.CanonicalRoot, m, force)
		if serr != nil {
			e.emit(name, false, base, nil, &RespError{Code: "save", Message: serr.Error()}, nil)
			return exitError
		}
		if outcome == store.Conflict {
			conflicts++
		}
		if wasConflict && outcome == store.Updated {
			outcomes = append(outcomes, refreshedEntry(m.Name, stored, m))
			continue
		}
		outcomes = append(outcomes, outcomeEntry(m.Name, outcome, stored, m))
	}
	base["results"] = outcomes
	if conflicts > 0 {
		e.emit(name, false, base, warns, nil, next)
		return exitConflicts
	}
	e.emit(name, true, base, warns, nil, next)
	return exitOK
}

// keepable reports whether --keep can settle a conflict between stored and the
// candidate: both must be one native lineage, and the candidate must carry a
// base to record.
func keepable(stored, cand *schema.CanonicalMemory) bool {
	return cand.Provenance.ImportHash != "" && store.SameLineage(stored.Provenance, cand.Provenance)
}

func keepDeclined(name string) string {
	return "--keep " + name + " declined: canonical and this native are different sources (or the name is ambiguous in the import); resolve it with --refresh or curate"
}

// buildMemory assembles a memory from --from-json input, or from the individual
// flags when no JSON source is given.
func buildMemory(fromJSON, nm, desc, typ, scp, body string, cwds, agents, hosts, related multiFlag) (*schema.CanonicalMemory, *RespError) {
	if fromJSON != "" {
		data, err := readInput(fromJSON)
		if err != nil {
			return nil, &RespError{Code: "read_input", Message: err.Error()}
		}
		var m schema.CanonicalMemory
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, &RespError{Code: "invalid_json", Message: err.Error()}
		}
		return &m, nil
	}
	return &schema.CanonicalMemory{
		Name:        nm,
		Description: desc,
		Type:        schema.Type(typ),
		Scope:       scp,
		AppliesTo:   schema.AppliesTo{Cwd: cwds, Agents: agents, Hosts: hosts},
		Related:     related,
		Body:        body,
	}, nil
}

// readInput reads from stdin when spec is "-", otherwise from the named file.
func readInput(spec string) ([]byte, error) {
	if spec == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(spec)
}

// withheldRow reports a candidate whose name a withheld canonical file claims
// (unparseable, invalid or duplicated) as a conflict row. That file still owns
// the name, so the import neither writes it nor aborts the rest of the batch.
func withheldRow(root string, m *schema.CanonicalMemory) (map[string]string, bool) {
	_, _, _, err := store.Load(root, m.Name)
	if !errors.Is(err, store.ErrWithheld) {
		return nil, false
	}
	return map[string]string{"name": m.Name, "outcome": string(store.Conflict), "withheld": err.Error()}, true
}

// expandCwdGlobs spells a leading ~ in each applies_to.cwd glob as the home
// directory, the same expansion the global --cwd gets, so a glob written either
// way matches the absolute cwd it is compared against. Any other relative glob is
// left for schema validation to refuse.
func expandCwdGlobs(globs []string) ([]string, *RespError) {
	if len(globs) == 0 {
		return globs, nil
	}
	out := make([]string, len(globs))
	for i, g := range globs {
		if g != "~" && !strings.HasPrefix(g, "~/") {
			out[i] = g
			continue
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, &RespError{Code: "home", Message: "cannot expand ~ in " + g + ": " + err.Error()}
		}
		out[i] = home + strings.TrimPrefix(g, "~")
	}
	return out, nil
}

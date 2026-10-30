package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/scope"
)

type inventoryLocation struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Skipped   int    `json:"skipped"`
	Unmanaged int    `json:"unmanaged"`
}

type inventoryDiagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type inventoryMemory struct {
	Name          string   `json:"name"`
	NativeSources []string `json:"native_sources"`
	RenderedPaths []string `json:"rendered_paths"`
}

type inventoryAbsence struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type inventoryCounts struct {
	NativeEntries    int `json:"native_entries"`
	RenderedEntries  int `json:"rendered_entries"`
	NativeNames      int `json:"native_names"`
	RenderedNames    int `json:"rendered_names"`
	OverlappingNames int `json:"overlapping_names"`
	DistinctNames    int `json:"distinct_names"`
}

type harnessInventory struct {
	Harness         string                `json:"harness"`
	Enabled         bool                  `json:"enabled"`
	Complete        bool                  `json:"complete"`
	NativeUnit      string                `json:"native_unit"`
	ScopeCwds       []string              `json:"scope_cwds"`
	ScopeComplete   bool                  `json:"scope_complete"`
	UnresolvedSlugs []string              `json:"unresolved_slugs"`
	Locations       []*inventoryLocation  `json:"locations"`
	Diagnostics     []inventoryDiagnostic `json:"diagnostics"`
	Counts          inventoryCounts       `json:"counts"`
	Memories        []inventoryMemory     `json:"memories"`
	CanonicalAbsent []inventoryAbsence    `json:"canonical_absent"`
	native          map[string][]string
	renders         map[string][]string
}

func newHarnessInventory(name string, enabled bool, unit string) *harnessInventory {
	return &harnessInventory{
		Harness: name, Enabled: enabled, Complete: true, ScopeComplete: true, NativeUnit: unit,
		ScopeCwds: []string{}, UnresolvedSlugs: []string{}, Locations: []*inventoryLocation{},
		Diagnostics: []inventoryDiagnostic{}, Memories: []inventoryMemory{}, CanonicalAbsent: []inventoryAbsence{},
		native: map[string][]string{}, renders: map[string][]string{},
	}
}

func (h *harnessInventory) issue(path, message string) {
	h.Complete = false
	h.Diagnostics = append(h.Diagnostics, inventoryDiagnostic{path, message})
}

func (h *harnessInventory) location(path, kind string) *inventoryLocation {
	loc := &inventoryLocation{Path: path, Kind: kind, State: "present"}
	h.Locations = append(h.Locations, loc)
	return loc
}

// Missing paths are observed absence, not unreadable sources. Diagnostics make
// partial observations explicit; no call here creates a store or lock.
func (h *harnessInventory) readDir(loc *inventoryLocation) ([]os.DirEntry, bool) {
	entries, err := os.ReadDir(loc.Path)
	if errors.Is(err, os.ErrNotExist) {
		loc.State = "missing"
		return nil, false
	}
	if err != nil {
		loc.State = "error"
		h.issue(loc.Path, err.Error())
		return nil, false
	}
	if len(entries) == 0 {
		loc.State = "empty"
	}
	return entries, true
}

func (h *harnessInventory) addNative(res importer.Result, path string, codex bool) {
	for _, m := range res.Memories {
		source := filepath.Join(path, m.Provenance.Source)
		if codex {
			source = path + "#Task Group: " + m.Description
		}
		h.native[m.Name] = append(h.native[m.Name], source)
	}
	for _, d := range res.Dropped {
		h.issue(path, d.Source+": "+d.Reason)
	}
}

func (h *harnessInventory) scanClaude(dir, cwd string) {
	loc := h.location(dir, "claude_memory")
	entries, ok := h.readDir(loc)
	if !ok {
		return
	}
	res, err := importer.ImportClaude(dir, cwd)
	h.addNative(res, dir, false)
	loc.Skipped = len(res.Skipped)
	if err != nil {
		loc.State = "error"
		h.issue(dir, err.Error())
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			h.issue(path, rerr.Error())
			continue
		}
		text := string(data)
		if !strings.HasPrefix(text, "---\n") {
			continue
		}
		front, _, found := strings.Cut(text[4:], "\n---\n")
		if !found {
			continue
		}
		var n struct {
			Name     string `yaml:"name"`
			Metadata struct {
				Origin string `yaml:"origin"`
			} `yaml:"metadata"`
		}
		if yaml.Unmarshal([]byte(front), &n) != nil || n.Metadata.Origin != marker.Origin {
			continue
		}
		if !schema.ValidName(n.Name) {
			h.issue(path, "owned render has invalid canonical name")
			continue
		}
		h.renders[n.Name] = append(h.renders[n.Name], path)
	}
}

func (h *harnessInventory) scanCodex(home string) {
	path := filepath.Join(home, "memories", "MEMORY.md")
	loc := h.location(path, "codex_consolidated")
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		loc.State = "missing"
	case err != nil:
		loc.State = "error"
		h.issue(path, err.Error())
	default:
		if len(strings.TrimSpace(string(data))) == 0 {
			loc.State = "empty"
		}
		res, rerr := importer.ImportCodex(path)
		h.addNative(res, path, true)
		loc.Skipped = len(res.Skipped)
		if rerr != nil {
			loc.State = "error"
			h.issue(path, rerr.Error())
		}
		if loc.State != "empty" && len(res.Memories)+len(res.Skipped)+len(res.Dropped) == 0 {
			loc.State = "error"
			h.issue(path, "nonempty source has no recognized Task Group sections")
		}
	}
	loc = h.location(filepath.Join(codexExtDir(home), "notes"), "codex_notes")
	entries, ok := h.readDir(loc)
	if !ok {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(loc.Path, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			h.issue(path, err.Error())
			continue
		}
		name, _, owned := marker.CodexNoteName(string(data))
		if !owned {
			loc.Unmanaged++
			if strings.Contains(string(data), "extension=engram") {
				h.issue(path, "unrecognized owned note marker")
			}
			continue
		}
		if !schema.ValidName(name) {
			h.issue(path, "owned note has invalid canonical name")
			continue
		}
		h.renders[name] = append(h.renders[name], path)
	}
}

func (h *harnessInventory) finish(mems []*schema.CanonicalMemory, s *session, canonicalComplete bool) {
	names := map[string]bool{}
	for n, paths := range h.native {
		h.Counts.NativeEntries += len(paths)
		names[n] = true
		// Judge the merged scan, not each slug separately: two independent
		// project directories may normalize different native sources to one name.
		if len(paths) > 1 {
			sources := append([]string{}, paths...)
			sort.Strings(sources)
			h.issue(sources[0], fmt.Sprintf("normalized name collision %q across %d native entries; inspect native_sources", n, len(paths)))
		}
	}
	for n, paths := range h.renders {
		h.Counts.RenderedEntries += len(paths)
		names[n] = true
		if len(h.native[n]) > 0 {
			h.Counts.OverlappingNames++
		}
	}
	h.Counts.NativeNames = len(h.native)
	h.Counts.RenderedNames = len(h.renders)
	h.Counts.DistinctNames = len(names)
	for _, name := range sortedKeys(names) {
		native := append([]string{}, h.native[name]...)
		rendered := append([]string{}, h.renders[name]...)
		sort.Strings(native)
		sort.Strings(rendered)
		h.Memories = append(h.Memories, inventoryMemory{name, native, rendered})
	}
	for _, m := range mems {
		if !names[m.Name] {
			h.CanonicalAbsent = append(h.CanonicalAbsent, inventoryAbsence{m.Name, h.absenceReason(m, s, canonicalComplete)})
		}
	}
	sort.Slice(h.CanonicalAbsent, func(i, j int) bool { return h.CanonicalAbsent[i].Name < h.CanonicalAbsent[j].Name })
	sort.Strings(h.ScopeCwds)
	sort.Strings(h.UnresolvedSlugs)
	sort.Slice(h.Diagnostics, func(i, j int) bool {
		if h.Diagnostics[i].Path != h.Diagnostics[j].Path {
			return h.Diagnostics[i].Path < h.Diagnostics[j].Path
		}
		return h.Diagnostics[i].Message < h.Diagnostics[j].Message
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Reasons describe the writer's reconcile policy, not plain sync, and only the
// reported scope contexts. They never assert that a harness consumed a render.
func (h *harnessInventory) absenceReason(m *schema.CanonicalMemory, s *session, canonicalComplete bool) string {
	if !h.Complete || !canonicalComplete {
		return "unknown_incomplete_scan"
	}
	if !h.Enabled {
		return "harness_disabled"
	}
	if h.Harness == config.HarnessCodex && originHarness(m) == config.HarnessCodex {
		return "source_suppressed"
	}
	suppressed := false
	agent := "codex"
	if h.Harness == config.HarnessClaude {
		agent = "claude"
	}
	for _, cwd := range h.ScopeCwds {
		if len(scope.RelevantFor([]*schema.CanonicalMemory{m}, cwd, s.agentFor(agent), s.host)) == 0 {
			continue
		}
		if h.Harness == config.HarnessClaude {
			cfg := s.cfg.Harnesses[config.HarnessClaude]
			if cfg.SharesIndex() && scope.IsShared(m, s.agentFor(agent), s.host) {
				return "missing_render"
			}
			if len(excludeSourceSlug([]*schema.CanonicalMemory{m}, filepath.Base(filepath.Dir(claudeMemoryDir(cfg.Home, cwd))))) == 0 {
				suppressed = true
				continue
			}
		}
		return "missing_render"
	}
	if suppressed {
		return "source_suppressed"
	}
	if !h.ScopeComplete {
		return "unknown_scope"
	}
	return "scope_excluded"
}

func cmdStatus(e *env, name string, args []string) int {
	pa, rerr := parseArgs(args, *statusArgs)
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitUsage
	}
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(name, false, nil, nil, rerr, nil)
		return exitError
	}
	mems, perrs, err := discover.Discover(s.cfg.CanonicalRoot)
	canonicalState := "present"
	canonicalComplete := true
	if rootAbsent(s.cfg.CanonicalRoot) {
		canonicalState = "missing"
		canonicalComplete = false
	}
	if err != nil {
		canonicalState = "error"
		canonicalComplete = false
	}
	if len(perrs) > 0 {
		canonicalComplete = false
	}
	if canonicalState == "present" && canonicalComplete && len(mems) == 0 {
		canonicalState = "empty"
	}
	canonDiag := []inventoryDiagnostic{}
	for _, p := range perrs {
		canonDiag = append(canonDiag, inventoryDiagnostic{p.Path, p.Err.Error()})
	}
	if err != nil {
		canonDiag = append(canonDiag, inventoryDiagnostic{s.cfg.CanonicalRoot, err.Error()})
	}
	ch := s.cfg.Harnesses[config.HarnessClaude]
	co := s.cfg.Harnesses[config.HarnessCodex]
	claude := newHarnessInventory(config.HarnessClaude, ch.Enabled(), "memory_file")
	codex := newHarnessInventory(config.HarnessCodex, co.Enabled(), "task_group")
	claude.ScopeCwds = append(claude.ScopeCwds, s.claudeRoot)
	codex.ScopeCwds = append(codex.ScopeCwds, s.cwd)
	dirs := map[string]string{claudeMemoryDir(ch.Home, s.claudeRoot): s.claudeRoot}
	if pa.bools["--all-projects"] {
		loc := claude.location(filepath.Join(ch.Home, "projects"), "claude_projects")
		if entries, ok := claude.readDir(loc); ok {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				cwd, found := importer.ClaudeProjectCwd(entry.Name())
				if found {
					claude.ScopeCwds = appendUnique(claude.ScopeCwds, cwd)
					codex.ScopeCwds = appendUnique(codex.ScopeCwds, cwd)
				} else {
					claude.ScopeComplete = false
					codex.ScopeComplete = false
					claude.UnresolvedSlugs = append(claude.UnresolvedSlugs, entry.Name())
					codex.UnresolvedSlugs = append(codex.UnresolvedSlugs, entry.Name())
				}
				dirs[filepath.Join(loc.Path, entry.Name(), "memory")] = cwd
			}
		}
		if loc.State == "error" {
			claude.ScopeComplete = false
			codex.ScopeComplete = false
		}
	}
	dirNames := map[string]bool{}
	for dir := range dirs {
		dirNames[dir] = true
	}
	for _, dir := range sortedKeys(dirNames) {
		claude.scanClaude(dir, dirs[dir])
	}
	claude.scanClaude(filepath.Join(ch.Home, "engram", "memory"), s.claudeRoot)
	codex.scanCodex(co.Home)
	claude.finish(mems, s, canonicalComplete)
	codex.finish(mems, s, canonicalComplete)
	complete := canonicalComplete && claude.Complete && codex.Complete
	data := map[string]any{
		"cwd": s.cwd, "host": s.host, "all_projects": pa.bools["--all-projects"], "complete": complete,
		"canonical": map[string]any{"root": s.cfg.CanonicalRoot, "state": canonicalState, "count": len(mems), "complete": canonicalComplete, "diagnostics": canonDiag},
		"harnesses": []*harnessInventory{claude, codex},
	}
	var failure *RespError
	code := exitOK
	if len(canonDiag) > 0 || !claude.Complete || !codex.Complete {
		code = exitError
		failure = &RespError{Code: "inventory_incomplete", Message: "some sources could not be inventoried; counts are partial (see diagnostics)"}
	}
	if e.jsonMode {
		e.emit(name, code == exitOK, data, nil, failure, nil)
	} else {
		if !e.quiet {
			fmt.Print(inventoryText(data, []*harnessInventory{claude, codex}, canonDiag))
		}
		if failure != nil {
			e.emit(name, false, nil, nil, failure, nil)
		}
	}
	return code
}

// inventoryText deliberately walks values: fmt's generic map printer would
// expose addresses for both harness pointers and nested location pointers.
// Print partial evidence before the separate stderr diagnostic on failure.
func inventoryText(data map[string]any, harnesses []*harnessInventory, diagnostics []inventoryDiagnostic) string {
	var out strings.Builder
	canonical := data["canonical"].(map[string]any)
	fmt.Fprintf(&out, "Inventory: complete=%v all_projects=%v cwd=%q host=%q\n", data["complete"], data["all_projects"], data["cwd"], data["host"])
	fmt.Fprintf(&out, "Canonical: count=%v state=%v complete=%v root=%q\n", canonical["count"], canonical["state"], canonical["complete"], canonical["root"])
	for _, d := range diagnostics {
		fmt.Fprintf(&out, "  Diagnostic: %q: %s\n", d.Path, d.Message)
	}
	for _, h := range harnesses {
		fmt.Fprintf(&out, "\n%s: enabled=%t complete=%t scope_complete=%t\n", h.Harness, h.Enabled, h.Complete, h.ScopeComplete)
		c := h.Counts
		fmt.Fprintf(&out, "  native_entries=%d (%s) native_names=%d rendered_entries=%d rendered_names=%d overlapping_names=%d distinct_names=%d\n", c.NativeEntries, h.NativeUnit, c.NativeNames, c.RenderedEntries, c.RenderedNames, c.OverlappingNames, c.DistinctNames)
		fmt.Fprintf(&out, "  Scope cwds: %q; unresolved slugs: %q\n", h.ScopeCwds, h.UnresolvedSlugs)
		for _, loc := range h.Locations {
			fmt.Fprintf(&out, "  Scan: %s %s %q (skipped=%d unmanaged=%d)\n", loc.Kind, loc.State, loc.Path, loc.Skipped, loc.Unmanaged)
		}
		for _, m := range h.Memories {
			fmt.Fprintf(&out, "  %s: native=%q rendered=%q\n", m.Name, m.NativeSources, m.RenderedPaths)
		}
		for _, a := range h.CanonicalAbsent {
			fmt.Fprintf(&out, "  Absent: %s (%s)\n", a.Name, a.Reason)
		}
		for _, d := range h.Diagnostics {
			fmt.Fprintf(&out, "  Diagnostic: %q: %s\n", d.Path, d.Message)
		}
	}
	return out.String()
}

func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

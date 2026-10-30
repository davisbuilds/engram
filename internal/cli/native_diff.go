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
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
	"github.com/davisbuilds/engram/internal/store"
)

type nativeComparison struct {
	Source         string                  `json:"source"`
	Memory         *schema.CanonicalMemory `json:"memory"`
	Differs        []string                `json:"differs"`
	ContentDiffers []string                `json:"content_differs"`
	MergeBase      map[string]any          `json:"merge_base"`
	Relation       string                  `json:"relation"`
}

type nativeScan struct {
	Path        string                `json:"path"`
	State       string                `json:"state"`
	Skipped     []string              `json:"skipped"`
	Diagnostics []inventoryDiagnostic `json:"diagnostics"`
}

func cmdNativeDiff(e *env, command string, pa parsedArgs) int {
	harness, name := pa.last("--native"), pa.last("--name")
	sourceName := pa.last("--source-name")
	if sourceName == "" {
		sourceName = name
	}
	shared := pa.bools["--shared"]
	if (harness != config.HarnessClaude && harness != config.HarnessCodex) || !schema.ValidName(name) || !schema.ValidName(sourceName) || shared && harness != config.HarnessClaude {
		e.emit(command, false, nil, nil, &RespError{Code: "usage", Message: "use diff --native <claude-code|codex> --name <canonical-name> [--source-name <native-name>] [--shared (Claude only)]"}, nil)
		return exitUsage
	}
	for _, flag := range []string{"--native", "--name", "--source-name"} {
		if len(pa.vals[flag]) > 1 {
			e.emit(command, false, nil, nil, usageError("%s must not repeat", flag), nil)
			return exitUsage
		}
	}
	s, rerr := e.newSession()
	if rerr != nil {
		e.emit(command, false, nil, nil, rerr, nil)
		return exitError
	}
	canonical, path, found, cerr := store.Load(s.cfg.CanonicalRoot, name)
	canonicalView := map[string]any{"state": "missing", "path": path}
	diagnostics := []inventoryDiagnostic{}
	if found {
		canonicalView["state"] = "present"
		canonicalView["memory"] = canonical
	}
	if cerr != nil {
		canonicalView["state"] = "error"
		if errors.Is(cerr, store.ErrWithheld) {
			canonicalView["state"] = "withheld"
		}
		diagnosticPath := path
		if diagnosticPath == "" {
			diagnosticPath = s.cfg.CanonicalRoot
		}
		diagnostics = append(diagnostics, inventoryDiagnostic{diagnosticPath, cerr.Error()})
	}
	h := s.cfg.Harnesses[harness]
	res, scan, base := readComparisonSources(harness, h.Home, sourceName, shared)
	diagnostics = append(diagnostics, scan.Diagnostics...)
	matches := []*schema.CanonicalMemory{}
	for _, m := range res.Memories {
		if m.Name == sourceName {
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return comparisonSource(harness, h.Home, matches[i], shared) < comparisonSource(harness, h.Home, matches[j], shared)
	})
	rows := []nativeComparison{}
	for _, m := range matches {
		fields, content := []string{}, []string{}
		if canonical != nil {
			fields = append(fields, store.Diff(canonical, m)...)
			content = append(content, contentDiff(canonical, m)...)
		}
		mergeBase := comparisonBase(canonical, m, len(matches) > 1, shared, base)
		relation := "different_content"
		switch {
		case cerr != nil:
			relation = "canonical_unavailable"
			mergeBase = map[string]any{"available": false, "reason": "canonical_unavailable"}
		case canonical == nil:
			relation = "canonical_missing"
		case len(content) == 0:
			relation = "same_content"
		case mergeBase["available"] == true:
			switch {
			case mergeBase["canonical_matches"] == true:
				relation = "native_changed"
			case mergeBase["native_matches"] == true:
				relation = "canonical_changed"
			default:
				relation = "both_changed"
			}
		}
		rows = append(rows, nativeComparison{comparisonSource(harness, h.Home, m, shared), m, fields, content, mergeBase, relation})
	}
	complete := cerr == nil && len(scan.Diagnostics) == 0
	data := map[string]any{"mode": "native", "harness": harness, "name": name, "source_name": sourceName, "shared": shared, "enabled": h.Enabled(), "complete": complete, "canonical": canonicalView, "scan": scan, "native": rows, "ambiguous": len(matches) > 1, "diagnostics": diagnostics}
	var failure *RespError
	code := exitOK
	if !complete {
		code = exitError
		failure = &RespError{Code: "comparison_incomplete", Message: "comparison is incomplete; inspect diagnostics before inferring absence"}
	} else if len(matches) > 1 {
		code = exitConflicts
		failure = &RespError{Code: "ambiguous_native", Message: "multiple native entries claim this normalized name; all are shown without binding a merge base"}
	}
	if e.jsonMode {
		e.emit(command, code == exitOK, data, nil, failure, nil)
	} else {
		if !e.quiet {
			fmt.Print(nativeDiffText(data, canonical, path, rows, diagnostics))
		}
		if failure != nil {
			e.emit(command, false, nil, nil, failure, nil)
		}
	}
	return code
}

func comparisonSource(harness, home string, m *schema.CanonicalMemory, shared bool) string {
	if shared {
		return filepath.Join(home, "engram", "memory", m.Name+".md")
	}
	if harness == config.HarnessCodex {
		return filepath.Join(home, "memories", "MEMORY.md") + "#Task Group: " + m.Description
	}
	slug, _, _ := strings.Cut(m.Provenance.ImportSource, "/")
	return filepath.Join(home, "projects", slug, "memory", m.Provenance.Source)
}

// Base hashes describe observed native-authored fields, not historical base text
// or semantic coverage. Never borrow a base across sources, names or collisions.
func comparisonBase(canonical, native *schema.CanonicalMemory, ambiguous, shared bool, stamp string) map[string]any {
	unavailable := func(reason string) map[string]any { return map[string]any{"available": false, "reason": reason} }
	if canonical == nil {
		return unavailable("canonical_missing")
	}
	if ambiguous {
		return unavailable("ambiguous_source")
	}
	if canonical.Name != native.Name {
		return unavailable("cross_name_comparison")
	}
	hash := canonical.Provenance.ImportHash
	if shared {
		hash = stamp
	}
	if hash == "" {
		return unavailable("no_recorded_base")
	}
	if !shared && !store.SameLineage(canonical.Provenance, native.Provenance) {
		return unavailable("lineage_mismatch")
	}
	return map[string]any{"available": true, "hash": hash, "native_matches": schema.NativeHash(native) == hash, "canonical_matches": schema.NativeHash(canonical) == hash}
}

func readComparisonSources(harness, home, name string, shared bool) (importer.Result, nativeScan, string) {
	path := filepath.Join(home, "projects")
	if harness == config.HarnessCodex {
		path = filepath.Join(home, "memories", "MEMORY.md")
	}
	if shared {
		path = filepath.Join(home, "engram", "memory", name+".md")
	}
	scan := nativeScan{Path: path, State: "present", Skipped: []string{}, Diagnostics: []inventoryDiagnostic{}}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		scan.State = "missing"
		return importer.Result{}, scan, ""
	}
	if err != nil {
		scan.State = "error"
		scan.Diagnostics = append(scan.Diagnostics, inventoryDiagnostic{path, err.Error()})
		return importer.Result{}, scan, ""
	}
	var res importer.Result
	var base string
	switch {
	case shared:
		var m *schema.CanonicalMemory
		m, base, err = readSharedComparison(path, name)
		if m != nil {
			res.Memories = append(res.Memories, m)
		}
	case harness == config.HarnessCodex:
		res, err = importer.ImportCodex(path)
		if err == nil && len(res.Memories)+len(res.Skipped)+len(res.Dropped) == 0 {
			var raw []byte
			raw, err = os.ReadFile(path)
			if err == nil {
				if strings.TrimSpace(string(raw)) == "" {
					scan.State = "empty"
				} else {
					err = fmt.Errorf("nonempty source has no recognized Task Group sections")
				}
			}
		}
	case !info.IsDir():
		err = fmt.Errorf("projects path for claude-code is not a directory")
	default:
		res, err = importer.ImportClaudeAll(home)
	}
	if err != nil {
		scan.State = "error"
		scan.Diagnostics = append(scan.Diagnostics, inventoryDiagnostic{path, err.Error()})
	}
	scan.Skipped = append(scan.Skipped, res.Skipped...)
	sort.Strings(scan.Skipped)
	for _, d := range res.Dropped {
		scan.Diagnostics = append(scan.Diagnostics, inventoryDiagnostic{path, d.Source + ": " + d.Reason})
	}
	return res, scan, base
}

// Shared renders are an explicitly selected editable source. Do not treat
// ordinary managed project renders as native memory or import their echoes.
func readSharedComparison(path, name string) (*schema.CanonicalMemory, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(raw), "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", fmt.Errorf("shared source has no frontmatter")
	}
	front, body, ok := strings.Cut(text[4:], "\n---\n")
	if !ok {
		return nil, "", fmt.Errorf("shared source has malformed frontmatter")
	}
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Metadata    struct {
			Type   string `yaml:"type"`
			Origin string `yaml:"origin"`
			Base   string `yaml:"engram_base"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return nil, "", err
	}
	if fm.Metadata.Origin != marker.Origin || fm.Name != name {
		return nil, "", fmt.Errorf("shared source ownership or name does not match requested memory")
	}
	m := &schema.CanonicalMemory{Name: fm.Name, Description: fm.Description, Type: schema.Type(fm.Metadata.Type), Scope: "global", Body: body, Provenance: schema.Provenance{Origin: "shared:claude-code"}}
	if err := m.Validate(); err != nil {
		return nil, "", err
	}
	return m, fm.Metadata.Base, nil
}

func nativeDiffText(data map[string]any, canonical *schema.CanonicalMemory, path string, rows []nativeComparison, diagnostics []inventoryDiagnostic) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Native comparison: %s canonical=%s source=%s complete=%v ambiguous=%v shared=%v enabled=%v\n", data["harness"], data["name"], data["source_name"], data["complete"], data["ambiguous"], data["shared"], data["enabled"])
	scan := data["scan"].(nativeScan)
	fmt.Fprintf(&out, "Scan: %s %q; skipped=%q\n", scan.State, scan.Path, scan.Skipped)
	fmt.Fprintf(&out, "--- canonical: %q (%v)\n", path, data["canonical"].(map[string]any)["state"])
	if canonical != nil {
		body, _ := canonical.Render()
		fmt.Fprintf(&out, "%s\n", body)
	}
	if len(rows) == 0 {
		fmt.Fprintln(&out, "No matching native source found in the reported scan.")
	}
	for _, r := range rows {
		fmt.Fprintf(&out, "+++ normalized source: %q\nrelation=%s differs=%v content_differs=%v merge_base=%v\n", r.Source, r.Relation, r.Differs, r.ContentDiffers, r.MergeBase)
		body, _ := r.Memory.Render()
		fmt.Fprintf(&out, "%s\n", body)
	}
	for _, d := range diagnostics {
		fmt.Fprintf(&out, "Diagnostic: %q: %s\n", d.Path, d.Message)
	}
	return out.String()
}

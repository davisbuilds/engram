package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
)

func TestNativeDiffShowsCanonicalAndNativeBodies(t *testing.T) {
	_, _, args := orphanFixture(t)
	code, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "old-group", "--source-name", "new-group"}, args...)...)
	if code != exitOK {
		t.Fatalf("comparison: %v", env)
	}
	data := env["data"].(map[string]any)
	rows := data["native"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected the named native source, got %v", rows)
	}
	if data["canonical"].(map[string]any)["memory"].(map[string]any)["body"] == "" {
		t.Error("canonical content was not exposed for comparison")
	}
}

func comparisonFixture(t *testing.T) (string, string, []string, *schema.CanonicalMemory) {
	t.Helper()
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	codex := filepath.Join(dir, "codex")
	file := filepath.Join(codex, "memories", "MEMORY.md")
	writeFile(t, file, "# Task Group: Example\n\nold body\n")
	res, err := importer.ImportCodex(file)
	if err != nil || len(res.Memories) != 1 {
		t.Fatal(res, err)
	}
	m := res.Memories[0]
	saveComparisonMemory(t, canon, m)
	cfg := filepath.Join(dir, "config.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  codex:\n    home: "+codex+"\n")
	return canon, codex, []string{"--config", cfg, "--cwd", "/work/x"}, m
}

func saveComparisonMemory(t *testing.T, canon string, m *schema.CanonicalMemory) {
	t.Helper()
	raw, err := m.Render()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(canon, m.Name+".md"), string(raw))
}

func TestNativeDiffReportsRecordedBaseWithoutInventingBaseText(t *testing.T) {
	cases := []struct {
		name                                              string
		native, canonical, base, origin, relation, reason string
		available                                         bool
	}{
		{name: "unchanged", relation: "same_content", available: true},
		{name: "native", native: "native edit", relation: "native_changed", available: true},
		{name: "canonical", canonical: "curated edit", relation: "canonical_changed", available: true},
		{name: "both", native: "native edit", canonical: "curated edit", relation: "both_changed", available: true},
		{name: "agreed edit", native: "agreed edit", canonical: "agreed edit", relation: "same_content", available: true},
		{name: "legacy", native: "native edit", base: "clear", relation: "different_content", reason: "no_recorded_base"},
		{name: "different lineage", native: "native edit", origin: "import:claude-code", relation: "different_content", reason: "lineage_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canon, codex, args, m := comparisonFixture(t)
			if tc.native != "" {
				writeFile(t, filepath.Join(codex, "memories", "MEMORY.md"), "# Task Group: Example\n\n"+tc.native+"\n")
			}
			if tc.canonical != "" {
				m.Body = "\n" + tc.canonical + "\n"
			}
			if tc.base == "clear" {
				m.Provenance.ImportHash = ""
			}
			if tc.origin != "" {
				m.Provenance.Origin = tc.origin
			}
			saveComparisonMemory(t, canon, m)
			before := inventorySnapshot(t, filepath.Dir(canon))
			code, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "example", "--apply"}, args...)...)
			if code != exitOK {
				t.Fatal(env)
			}
			row := env["data"].(map[string]any)["native"].([]any)[0].(map[string]any)
			base := row["merge_base"].(map[string]any)
			if base["available"] != tc.available || row["relation"] != tc.relation {
				t.Errorf("base/relation: %v", row)
			}
			if !tc.available && base["reason"] != tc.reason {
				t.Errorf("base reason: %v", base)
			}
			if tc.available {
				wantNative := tc.native == ""
				wantCanonical := tc.canonical == ""
				if base["native_matches"] != wantNative || base["canonical_matches"] != wantCanonical {
					t.Errorf("recorded-base comparison: %v", base)
				}
			}
			if _, exists := base["body"]; exists {
				t.Error("base hash must not pretend to contain historical text")
			}
			if !reflect.DeepEqual(before, inventorySnapshot(t, filepath.Dir(canon))) {
				t.Error("comparison wrote fixture files")
			}
		})
	}
}

func TestNativeDiffCrossNameCannotBorrowBase(t *testing.T) {
	_, _, args := orphanFixture(t)
	_, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "old-group", "--source-name", "new-group"}, args...)...)
	row := env["data"].(map[string]any)["native"].([]any)[0].(map[string]any)
	if row["merge_base"].(map[string]any)["reason"] != "cross_name_comparison" {
		t.Fatal(row)
	}
	if !strings.Contains(row["source"].(string), "#Task Group: New Group") {
		t.Fatal(row)
	}
	fields := row["differs"].([]any)
	if len(fields) == 0 || fields[0] != "name" {
		t.Errorf("missing name/metadata comparison: %v", fields)
	}
}

func TestNativeDiffDoesNotChooseAnAmbiguousSource(t *testing.T) {
	_, codex, args, _ := comparisonFixture(t)
	writeFile(t, filepath.Join(codex, "memories", "MEMORY.md"), "# Task Group: Example\n\none\n# Task Group: Example!\n\ntwo\n")
	code, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "example"}, args...)...)
	data := env["data"].(map[string]any)
	rows := data["native"].([]any)
	if code != exitConflicts || data["ambiguous"] != true || len(rows) != 2 {
		t.Fatal(env)
	}
	for _, row := range rows {
		if row.(map[string]any)["merge_base"].(map[string]any)["reason"] != "ambiguous_source" {
			t.Fatal(row)
		}
	}
}

func TestNativeDiffMissingAndMalformedSourcesStayDistinct(t *testing.T) {
	for _, kind := range []string{"missing_native", "unrecognized", "read_error", "withheld", "missing_canonical", "missing_both"} {
		t.Run(kind, func(t *testing.T) {
			canon, codex, args, _ := comparisonFixture(t)
			file := filepath.Join(codex, "memories", "MEMORY.md")
			switch kind {
			case "missing_native", "missing_both":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "unrecognized":
				writeFile(t, file, "unknown native format")
			case "read_error":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(file, 0o755); err != nil {
					t.Fatal(err)
				}
			case "withheld":
				writeFile(t, filepath.Join(canon, "example.md"), "broken canonical")
			}
			if kind == "missing_canonical" || kind == "missing_both" {
				if err := os.Remove(filepath.Join(canon, "example.md")); err != nil {
					t.Fatal(err)
				}
			}
			code, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "example"}, args...)...)
			data := env["data"].(map[string]any)
			failed := kind == "unrecognized" || kind == "read_error" || kind == "withheld"
			if failed {
				if code != exitError || data["complete"] != false {
					t.Fatal(env)
				}
			} else if code != exitOK {
				t.Fatal(env)
			}
			if kind == "missing_native" || kind == "missing_both" {
				if data["scan"].(map[string]any)["state"] != "missing" || len(data["native"].([]any)) != 0 {
					t.Fatal(data)
				}
			}
			if kind == "withheld" && data["canonical"].(map[string]any)["state"] != "withheld" {
				t.Fatal(data)
			}
			if kind == "withheld" {
				row := data["native"].([]any)[0].(map[string]any)
				if row["relation"] != "canonical_unavailable" || row["merge_base"].(map[string]any)["reason"] != "canonical_unavailable" {
					t.Fatal(row)
				}
			}
		})
	}
}

func TestNativeDiffClaudeSourcesAndExplicitSharedRender(t *testing.T) {
	canon, _, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	_, env := runEnvelope(t, append([]string{"diff", "--native", "claude-code", "--name", "live-lesson"}, args...)...)
	rows := env["data"].(map[string]any)["native"].([]any)
	if len(rows) != 1 || !strings.Contains(rows[0].(map[string]any)["source"].(string), "-work-x/memory/live.md") {
		t.Fatal(env)
	}
	m := &schema.CanonicalMemory{Name: "shared-lesson", Description: "d", Type: schema.TypeReference, Scope: "global", Body: "old\n"}
	saveComparisonMemory(t, canon, m)
	shared := filepath.Join(claude, "engram", "memory", "shared-lesson.md")
	writeFile(t, shared, "---\nname: shared-lesson\ndescription: d\nmetadata:\n  origin: "+marker.Origin+"\n  type: reference\n  engram_base: "+schema.NativeHash(m)+"\n---\nedited\n")
	code, env := runEnvelope(t, append([]string{"diff", "--native", "claude-code", "--shared", "--name", "shared-lesson"}, args...)...)
	if code != exitOK {
		t.Fatal(env)
	}
	row := env["data"].(map[string]any)["native"].([]any)[0].(map[string]any)
	if row["relation"] != "native_changed" || row["merge_base"].(map[string]any)["available"] != true {
		t.Fatal(row)
	}
	writeFile(t, shared, "---\nname: shared-lesson\ndescription: d\n---\nunowned\n")
	code, env = runEnvelope(t, append([]string{"diff", "--native", "claude-code", "--shared", "--name", "shared-lesson"}, args...)...)
	if code != exitError {
		t.Fatalf("unowned shared file treated as a source: %v", env)
	}
}

func TestNativeDiffArgumentValidationAndPlainOutput(t *testing.T) {
	_, _, args, _ := comparisonFixture(t)
	for _, argv := range [][]string{
		{"diff", "--name", "example"},
		{"diff", "--native", "codex"},
		{"diff", "--native", "invalid", "--name", "example"},
		{"diff", "--native", "codex", "--name", "../escape"},
		{"diff", "--native", "codex", "--name", "example", "--shared"},
		{"diff", "--native", "codex", "--name", "example", "--name", "other"},
	} {
		code, _ := runEnvelope(t, append(argv, args...)...)
		if code != exitUsage {
			t.Errorf("%v exit %d", argv, code)
		}
	}
	code := 0
	out := captureStdout(t, func() {
		code = Run(append([]string{"diff", "--native", "codex", "--name", "example", "--plain"}, args...))
	})
	if code != exitOK || !strings.Contains(out, "--- canonical:") || !strings.Contains(out, "+++ normalized source:") || strings.Count(out, "old body") != 2 {
		t.Fatalf("plain comparison hides content:\n%s", out)
	}
}

func TestNativeDiffDisplaysScopeAndProvenanceDifferencesWithoutRescoping(t *testing.T) {
	canon, _, args, m := comparisonFixture(t)
	m.Scope = "project:unavailable"
	m.AppliesTo.Agents = []string{"claude"}
	m.Provenance.Author = "curator"
	saveComparisonMemory(t, canon, m)
	code, env := runEnvelope(t, append([]string{"diff", "--native", "codex", "--name", "example"}, args...)...)
	if code != exitOK {
		t.Fatal(env)
	}
	row := env["data"].(map[string]any)["native"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(row["differs"], []any{"scope", "applies_to", "provenance"}) || len(row["content_differs"].([]any)) != 0 || row["relation"] != "same_content" {
		t.Fatal(row)
	}
	stored, err := os.ReadFile(filepath.Join(canon, "example.md"))
	if err != nil || !strings.Contains(string(stored), "scope: project:unavailable") {
		t.Fatalf("scope changed: %s %v", stored, err)
	}
}

func TestNativeDiffClaudeScanFailureRetainsCanonicalEvidence(t *testing.T) {
	canon, _, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	writeFile(t, filepath.Join(claude, "projects", "-work-x", "memory", "broken.md"), "---\nname: broken\n")
	code, env := runEnvelope(t, append([]string{"diff", "--native", "claude-code", "--name", "live-lesson"}, args...)...)
	data := env["data"].(map[string]any)
	if code != exitError || data["complete"] != false || len(data["native"].([]any)) != 1 || data["canonical"].(map[string]any)["state"] != "present" {
		t.Fatal(env)
	}
	var plainCode int
	out := captureStdout(t, func() {
		plainCode = Run(append([]string{"diff", "--native", "claude-code", "--name", "live-lesson", "--plain"}, args...))
	})
	if plainCode != exitError || !strings.Contains(out, "--- canonical:") || !strings.Contains(out, "+++ normalized source:") || !strings.Contains(out, "malformed frontmatter") {
		t.Fatalf("partial comparison hidden:\n%s", out)
	}
}

func TestNativeDiffSharedUnchangedAndUnstampedInputs(t *testing.T) {
	canon, _, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	m := &schema.CanonicalMemory{Name: "shared-lesson", Description: "d", Type: schema.TypeReference, Scope: "global", Body: "same\n"}
	saveComparisonMemory(t, canon, m)
	for _, base := range []string{schema.NativeHash(m), ""} {
		stamp := ""
		if base != "" {
			stamp = "  engram_base: " + base + "\n"
		}
		writeFile(t, filepath.Join(claude, "engram", "memory", "shared-lesson.md"), "---\nname: shared-lesson\ndescription: d\nmetadata:\n  origin: "+marker.Origin+"\n  type: reference\n"+stamp+"---\nsame\n")
		code, env := runEnvelope(t, append([]string{"diff", "--native", "claude-code", "--shared", "--name", "shared-lesson"}, args...)...)
		if code != exitOK {
			t.Fatal(env)
		}
		row := env["data"].(map[string]any)["native"].([]any)[0].(map[string]any)
		if row["relation"] != "same_content" || row["merge_base"].(map[string]any)["available"] != (base != "") {
			t.Fatal(row)
		}
	}
}

func TestNativeDiffPreservesBareDiffAndInspectsDisabledHarness(t *testing.T) {
	canon, codex, args, _ := comparisonFixture(t)
	_, env := runEnvelope(t, append([]string{"diff"}, args...)...)
	data := env["data"].(map[string]any)
	if _, ok := data["harnesses"]; !ok {
		t.Fatalf("bare diff contract changed: %v", env)
	}
	cfg := filepath.Join(filepath.Dir(canon), "disabled.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  codex:\n    home: "+codex+"\n    disabled: true\n")
	code, env := runEnvelope(t, "diff", "--native", "codex", "--name", "example", "--config", cfg, "--cwd", "/work/x")
	data = env["data"].(map[string]any)
	if code != exitOK || data["enabled"] != false || len(data["native"].([]any)) != 1 {
		t.Fatal(env)
	}
}

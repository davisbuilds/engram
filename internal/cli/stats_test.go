package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func statusStats(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	s, ok := respData(resp)["stats"].(map[string]any)
	if !ok {
		t.Fatal("status --stats must expose measured volume and freshness")
	}
	return s
}

func TestStatsMeasuresStoredTextAndDeclaredScope(t *testing.T) {
	canon, _, args := orphanFixture(t)
	path := filepath.Join(canon, "measured.md")
	body := "one two\n三\n"
	writeFile(t, path, "---\nname: measured\ndescription: d\ntype: lesson\nscope: project:sample\nprovenance:\n  created: 2020-01-01\n  modified: 2021-02-03\nrelated: [live-lesson, absent-target]\n---\n"+body)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(filepath.Dir(canon), "claude")
	native := filepath.Join(claude, "projects", "-work-x", "memory", "unicode.md")
	raw := "# No frontmatter\n\nhéllo\tworld\n三\n"
	writeFile(t, native, raw)
	before := inventorySnapshot(t, filepath.Dir(canon))
	code, resp := runEnvelope(t, append([]string{"status", "--stats", "--apply"}, args...)...)
	if code != exitOK {
		t.Fatalf("stats: %d %v", code, resp)
	}
	s := statusStats(t, resp)
	if s["word_unit"] != "unicode_whitespace_fields" || s["complete"] != true {
		t.Fatalf("measurement contract: %v", s)
	}
	if len(s["reference_forms"].([]any)) != 3 {
		t.Error("statistics must expose the reference detector boundaries")
	}
	found := map[string]bool{}
	for _, value := range s["sources"].([]any) {
		v := value.(map[string]any)
		if v["path"] == native {
			found["native"] = true
			if v["bytes"] != float64(len(raw)) || v["words"] != float64(6) {
				t.Errorf("stored UTF-8/whitespace measurement: %v", v)
			}
		}
		if v["path"] == path {
			found["canonical"] = true
			if v["modified_at"] != old.Format(time.RFC3339Nano) || v["declared_modified"] != "2021-02-03" {
				t.Errorf("mtime must be distinct from declared provenance: %v", v)
			}
		}
	}
	for _, value := range s["canonical_scopes"].([]any) {
		v := value.(map[string]any)
		if v["scope"] == "project:sample" {
			found["scope"] = true
			if v["body_bytes"] != float64(len(body)) || v["body_words"] != float64(3) || v["files"] != float64(1) {
				t.Errorf("declared-scope body measurement: %v", v)
			}
		}
	}
	if len(found) != 3 || s["references_checked"] != float64(2) || len(s["unresolved_references"].([]any)) != 1 {
		t.Errorf("missing measurements or reference positive/negative controls: %v %v", found, s)
	}
	total := s["total"].(map[string]any)
	var sourceBytes, locationBytes float64
	for _, row := range s["sources"].([]any) {
		sourceBytes += row.(map[string]any)["bytes"].(float64)
	}
	for _, row := range s["locations"].([]any) {
		locationBytes += row.(map[string]any)["bytes"].(float64)
	}
	if sourceBytes != total["bytes"] || locationBytes != total["bytes"] || total["files"] != float64(len(s["sources"].([]any))) {
		t.Error("per-source, per-location and total stored measurements must agree")
	}
	sameStore(t, before, inventorySnapshot(t, filepath.Dir(canon)))
	blob, _ := json.Marshal(resp)
	if strings.Contains(string(blob), "one two") || strings.Contains(string(blob), "héllo") {
		t.Error("stats must not emit stored bodies")
	}
}

func TestStatsIsOptIn(t *testing.T) {
	_, _, args := orphanFixture(t)
	_, resp := runEnvelope(t, append([]string{"status"}, args...)...)
	if _, ok := respData(resp)["stats"]; ok {
		t.Error("ordinary inventory should not collect or emit optional statistics")
	}
}

func TestStatsSeparatesRepeatedNamesFromExactCopies(t *testing.T) {
	canon, _, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	render := "---\nname: copied\nmetadata:\n  origin: engram-sync\n---\nprivate duplicated body\n"
	for _, dir := range []string{filepath.Join(claude, "projects", "-work-x", "memory"), filepath.Join(claude, "engram", "memory")} {
		writeFile(t, filepath.Join(dir, "copied.md"), render)
	}
	writeFile(t, filepath.Join(claude, "projects", "-work-y", "memory", "copied.md"), render+"different\n")
	for _, all := range []bool{false, true} {
		argv := []string{"status", "--stats"}
		if all {
			argv = append(argv, "--all-projects")
		}
		code, resp := runEnvelope(t, append(argv, args...)...)
		if code != exitOK {
			t.Fatalf("stats: %d %v", code, resp)
		}
		s := statusStats(t, resp)
		repeated := s["repeated_renders"].([]any)
		exact := s["exact_duplicate_renders"].([]any)
		wantFiles := float64(2)
		if all {
			wantFiles = 3
		}
		if len(repeated) != 1 || repeated[0].(map[string]any)["files"] != wantFiles {
			t.Fatalf("same name does not imply same bytes: %v", repeated)
		}
		if len(exact) != 1 || exact[0].(map[string]any)["extra_copies"] != float64(1) || exact[0].(map[string]any)["extra_bytes"] != float64(len(render)) {
			t.Errorf("exact duplicate volume: %v", exact)
		}
		_, again := runEnvelope(t, append(argv, args...)...)
		if !reflect.DeepEqual(s, statusStats(t, again)) {
			t.Error("statistics order or measurements changed on unchanged files")
		}
	}
}

func TestStatsReferencesAreBoundedAndHavePositiveControls(t *testing.T) {
	canon, codex, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	index := filepath.Join(claude, "projects", "-work-x", "memory", "MEMORY.md")
	writeFile(t, index, "- [present](live.md)\n- [missing](missing.md)\n- [repeat](missing.md)\n- [outside](../outside.md)\n- [remote](https://example.invalid/no.md)\n````md\n```\n- [example](fenced.md)\n````\n    - [example](indented.md)\n")
	rollouts := filepath.Join(codex, "memories", "rollout_summaries")
	writeFile(t, filepath.Join(rollouts, "present.md"), "evidence\n")
	if err := os.Mkdir(filepath.Join(rollouts, "directory.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(codex, "memories", "MEMORY.md")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, string(raw)+"\nrollout_summaries/present.md rollout_summaries/absent.md rollout_summaries/absent.md\nrollout_summaries/directory.md\n/other/rollout_summaries/outside.md\nrollout_summaries/prefix.md.txt\n```\nrollout_summaries/fenced.md\n```\n")
	code, resp := runEnvelope(t, append([]string{"status", "--stats"}, args...)...)
	if code != exitOK {
		t.Fatalf("stats: %d %v", code, resp)
	}
	s := statusStats(t, resp)
	issues := s["unresolved_references"].([]any)
	if s["references_checked"] != float64(5) || len(issues) != 3 {
		t.Fatalf("reference controls/dedup/grammar: %v", s)
	}
	states := map[string]int{}
	for _, issue := range issues {
		states[issue.(map[string]any)["state"].(string)]++
	}
	if states["missing"] != 2 || states["not_file"] != 1 {
		t.Errorf("unresolved states: %v", issues)
	}
}

func TestStatsReferenceErrorsArePartialAndDistinctFromMissing(t *testing.T) {
	_, codex, args := orphanFixture(t)
	source := filepath.Join(codex, "memories", "MEMORY.md")
	writeFile(t, source, "# Task Group: Valid\n\nrollout_summaries/loop.md\n")
	loop := filepath.Join(codex, "memories", "rollout_summaries", "loop.md")
	if err := os.MkdirAll(filepath.Dir(loop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop.md", loop); err != nil {
		t.Fatal(err)
	}
	code, _ := runEnvelope(t, append([]string{"status"}, args...)...)
	if code != exitOK {
		t.Fatal("control: ordinary inventory should not probe optional references")
	}
	code, resp := runEnvelope(t, append([]string{"status", "--stats"}, args...)...)
	s := statusStats(t, resp)
	if code != exitError || s["complete"] != false || len(s["diagnostics"].([]any)) != 1 {
		t.Fatalf("reference error must not look like clean absence: %d %v", code, resp)
	}
	if s["unresolved_references"].([]any)[0].(map[string]any)["state"] != "error" {
		t.Errorf("unresolved error state: %v", s)
	}
}

func TestStatsIncompleteCanonicalDoesNotProveMissingRelated(t *testing.T) {
	canon, _, args := orphanFixture(t)
	writeFile(t, filepath.Join(canon, "broken.md"), "broken\n")
	writeFile(t, filepath.Join(canon, "related.md"), "---\nname: related\ndescription: d\ntype: lesson\nscope: global\nrelated: [unseen]\n---\nbody\n")
	code, resp := runEnvelope(t, append([]string{"status", "--stats"}, args...)...)
	s := statusStats(t, resp)
	if code != exitError || s["complete"] != false || s["unresolved_references"].([]any)[0].(map[string]any)["state"] != "unknown_incomplete_canonical" {
		t.Fatalf("incomplete corpus is not proof of absence: %d %v", code, resp)
	}
	for _, source := range s["sources"].([]any) {
		v := source.(map[string]any)
		if v["kind"] == "canonical_unparsed" && v["bytes"] == float64(7) {
			return
		}
	}
	t.Error("raw malformed-file text should remain visible without counting as a valid scoped memory")
}

func TestStatsMissingStoresRemainMissingAndPlainOutputIsReadable(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "missing")
	cfg := filepath.Join(dir, "config.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+filepath.Join(dir, "claude")+"\n  codex:\n    home: "+filepath.Join(dir, "codex")+"\n")
	code, resp := runEnvelope(t, "status", "--stats", "--apply", "--config", cfg)
	s := statusStats(t, resp)
	if code != exitOK || s["complete"] != false || s["total"].(map[string]any)["files"] != float64(0) {
		t.Fatalf("missing is not a complete empty corpus: %d %v", code, resp)
	}
	if _, err := os.Stat(canon); !os.IsNotExist(err) {
		t.Error("stats created the missing canonical root")
	}
	out := captureStdout(t, func() { Run([]string{"status", "--stats", "--plain", "--config", cfg}) })
	if !strings.Contains(out, "Stored text statistics: complete=false") || !strings.Contains(out, "References: checked=0 unresolved=0 (supported forms only)") || strings.Contains(out, "0xc0") {
		t.Errorf("plain stats: %s", out)
	}
	if err := os.Mkdir(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	code, resp = runEnvelope(t, "status", "--stats", "--config", cfg)
	s = statusStats(t, resp)
	if code != exitOK || s["complete"] != true || s["total"].(map[string]any)["files"] != float64(0) {
		t.Errorf("an observed empty canonical store differs from a missing one: %d %v", code, resp)
	}
}

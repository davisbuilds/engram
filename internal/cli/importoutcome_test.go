package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
)

func claudeImport(cand *schema.CanonicalMemory) []importGather {
	return []importGather{{
		harness: "claude-code",
		result:  importer.Result{Memories: []*schema.CanonicalMemory{cand}, ScopeAuthoritative: true},
	}}
}

// Canonical written before the importer began stamping provenance.source must not
// conflict forever with every later import of an unchanged native. The dry-run
// has to report the same outcome apply will produce (updated, not conflict).
func TestMergeImportsProvenanceBackfillIsNotAConflict(t *testing.T) {
	stored := mem("global", "d", "b")
	stored.Provenance = schema.Provenance{Origin: "import:claude-code"}
	cand := mem("global", "d", "b")
	cand.Provenance = schema.Provenance{Origin: "import:claude-code", Source: "m.md"}

	merged, entries, hadConflict, _ := mergeImports([]*schema.CanonicalMemory{stored}, claudeImport(cand))

	if hadConflict {
		t.Error("a provenance-only difference must not register as a conflict")
	}
	got := entries[0]["results"].([]map[string]string)
	if got[0]["outcome"] != "updated" {
		t.Errorf("outcome = %v, want updated (provenance backfill)", got)
	}
	if _, has := got[0]["differs"]; has {
		t.Errorf("a non-conflict outcome should not carry a differs list: %v", got)
	}
	if merged[0].Provenance.Source != "m.md" {
		t.Errorf("merged provenance.source = %q, want backfilled m.md", merged[0].Provenance.Source)
	}
}

// A real content change must still conflict, and say which fields differ so the
// operator does not have to diff files by hand to find out why.
func TestMergeImportsConflictNamesTheDifferingFields(t *testing.T) {
	stored := mem("global", "d", "old body")
	stored.Provenance = schema.Provenance{Origin: "import:claude-code"}
	cand := mem("global", "d", "new body")
	cand.Provenance = schema.Provenance{Origin: "import:claude-code", Source: "m.md"}

	_, entries, hadConflict, _ := mergeImports([]*schema.CanonicalMemory{stored}, claudeImport(cand))

	if !hadConflict {
		t.Fatal("a body change must conflict")
	}
	got := entries[0]["results"].([]map[string]string)
	if got[0]["outcome"] != "conflict" || got[0]["differs"] != "provenance,body" {
		t.Errorf("result = %v, want conflict differing in provenance,body", got)
	}
}

// End to end against the real filesystem, reproducing the field failure: a
// canonical file stored without provenance.source, then re-imported.
func TestImportApplyBackfillsProvenanceThenSettles(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	defer silenceStdout(t)()
	file := filepath.Join(canon, "claude-lesson.md")

	if code := Run(append([]string{"import", "claude-code", "--apply"}, args...)); code != exitOK {
		t.Fatalf("first import exit = %d, want %d", code, exitOK)
	}
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// Age the canonical file: strip the source stamp an older importer never wrote.
	var aged []string
	for _, ln := range strings.Split(string(written), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(ln), "source:") {
			aged = append(aged, ln)
		}
	}
	if len(aged) == len(strings.Split(string(written), "\n")) {
		t.Fatalf("fixture invalid: importer wrote no provenance.source to strip:\n%s", written)
	}
	writeFile(t, file, strings.Join(aged, "\n"))

	if code := Run(append([]string{"import", "claude-code", "--apply"}, args...)); code != exitOK {
		t.Fatalf("re-import of an aged canonical exit = %d, want %d (provenance-only diff is not a conflict)", code, exitOK)
	}
	healed, _ := os.ReadFile(file)
	if string(healed) != string(written) {
		t.Errorf("provenance was not backfilled to the importer's shape:\nwant:\n%s\ngot:\n%s", written, healed)
	}
	_ = claudeMem

	// Settled: another pass changes nothing.
	if code := Run(append([]string{"import", "claude-code", "--apply"}, args...)); code != exitOK {
		t.Errorf("third import exit = %d, want %d", code, exitOK)
	}
	again, _ := os.ReadFile(file)
	if string(again) != string(healed) {
		t.Error("import is not idempotent after backfill")
	}
}

// A native edited after import is a genuine conflict: exit 3, canonical left
// untouched, and the result names the field so the cause is visible.
func TestImportApplyBodyDriftConflictsAndReportsDiffers(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	file := filepath.Join(canon, "claude-lesson.md")

	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"import", "claude-code", "--apply"}, args...)); code != exitOK {
			t.Fatalf("first import exit = %d", code)
		}
	}()
	before, _ := os.ReadFile(file)

	writeFile(t, filepath.Join(claudeMem, "lesson-a.md"),
		"---\nname: claude-lesson\ndescription: a claude lesson\nmetadata:\n  type: lesson\n---\nclaude body, edited later\n")

	var code int
	out := captureStdout(t, func() {
		code = Run(append([]string{"import", "claude-code", "--apply"}, args...))
	})
	if code != exitConflicts {
		t.Fatalf("exit = %d, want %d (native drift is a conflict)", code, exitConflicts)
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Error("a conflicting import must leave canonical untouched")
	}
	var env struct {
		Data struct {
			Results []map[string]string `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope: %v\n%s", err, out)
	}
	if len(env.Data.Results) != 1 || env.Data.Results[0]["outcome"] != "conflict" || env.Data.Results[0]["differs"] != "body" {
		t.Errorf("results = %v, want one conflict differing in body", env.Data.Results)
	}
}

// The dry-run must say what apply would do with each candidate, not merely list
// them: created for a new name, unchanged once it is imported.
func TestImportDryRunReportsPerMemoryOutcome(t *testing.T) {
	_, _, _, args := setupTwoHarnesses(t)

	outcomeOf := func() string {
		out := captureStdout(t, func() {
			if code := Run(append([]string{"import", "claude-code"}, args...)); code != exitOK {
				t.Fatalf("dry-run exit = %d", code)
			}
		})
		var env struct {
			Data struct {
				Memories []map[string]string `json:"memories"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Data.Memories) != 1 {
			t.Fatalf("envelope: err=%v\n%s", err, out)
		}
		return env.Data.Memories[0]["outcome"]
	}

	if got := outcomeOf(); got != "created" {
		t.Errorf("before import: outcome = %q, want created", got)
	}
	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"import", "claude-code", "--apply"}, args...)); code != exitOK {
			t.Fatalf("apply exit = %d", code)
		}
	}()
	if got := outcomeOf(); got != "unchanged" {
		t.Errorf("after import: outcome = %q, want unchanged", got)
	}
}

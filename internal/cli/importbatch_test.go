package cli

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
)

// importOutcomes runs one import and returns every row's outcome, sorted, so
// two candidates sharing a name are both visible.
func importOutcomes(t *testing.T, args ...string) []string {
	t.Helper()
	out := captureStdout(t, func() { Run(args) })
	var env struct {
		Data struct {
			Memories []map[string]string `json:"memories"`
			Results  []map[string]string `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope: %v\n%s", err, out)
	}
	var got []string
	for _, r := range append(env.Data.Memories, env.Data.Results...) {
		got = append(got, r["name"]+"="+r["outcome"])
	}
	sort.Strings(got)
	return got
}

// Two natives that normalize to one name: apply saves them in order, so the
// second meets the first. The dry-run must predict exactly that.
func TestImportDryRunMatchesApplyForSameNameCandidates(t *testing.T) {
	_, claudeMem, _, args := setupTwoHarnesses(t)
	writeFile(t, filepath.Join(claudeMem, "lesson-b.md"),
		"---\nname: claude-lesson\ndescription: same name, other text\nmetadata:\n  type: lesson\n---\nother body\n")

	dry := importOutcomes(t, append([]string{"import", "claude-code"}, args...)...)
	applied := importOutcomes(t, append([]string{"import", "claude-code", "--apply"}, args...)...)
	if len(dry) != 2 || len(applied) != 2 {
		t.Fatalf("rows: dry %v, apply %v", dry, applied)
	}
	for i := range dry {
		if dry[i] != applied[i] {
			t.Errorf("dry-run %v, apply %v", dry, applied)
			break
		}
	}
}

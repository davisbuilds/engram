package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A canonical file discovery withholds (here, unparseable) still owns its name:
// import must report that one memory as a conflict, leave the file untouched and
// import the rest, never abort the batch or overwrite the file.
func TestImportTreatsAWithheldCanonicalAsAConflict(t *testing.T) {
	canon, _, args := twoNatives(t)
	broken := filepath.Join(canon, "claude-lesson.md")
	const body = "---\nname: claude-lesson\n"
	writeFile(t, broken, body)

	for _, mode := range [][]string{{"import", "claude-code"}, {"import", "claude-code", "--apply"}} {
		code, rows := importRows(t, append(mode, args...)...)
		if code == exitError {
			t.Fatalf("%v aborted (exit %d)", mode, code)
		}
		if got := rows["claude-lesson"]["outcome"]; got != "conflict" {
			t.Errorf("%v: claude-lesson outcome = %q, want conflict", mode, got)
		}
		if got := rows["other-lesson"]["outcome"]; got != "created" {
			t.Errorf("%v: other-lesson outcome = %q, want created", mode, got)
		}
	}
	if b, err := os.ReadFile(broken); err != nil || string(b) != body {
		t.Errorf("withheld canonical changed: %q, %v", b, err)
	}
}

// reconcile's preview must say the same: a withheld name is a conflict, not a
// memory it would create.
func TestReconcilePreviewsAWithheldCanonicalAsAConflict(t *testing.T) {
	canon, _, _, args := setupTwoHarnesses(t)
	writeFile(t, filepath.Join(canon, "claude-lesson.md"), "---\nname: claude-lesson\n")
	out := captureStdout(t, func() { Run(append([]string{"reconcile"}, args...)) })
	var env struct {
		Data struct {
			Import []struct {
				Results []map[string]string `json:"results"`
			} `json:"import"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope: %v\n%s", err, out)
	}
	for _, h := range env.Data.Import {
		for _, r := range h.Results {
			if r["name"] == "claude-lesson" && r["outcome"] != "conflict" {
				t.Errorf("claude-lesson previewed as %q, want conflict", r["outcome"])
			}
		}
	}
}

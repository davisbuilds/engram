package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runEnvelope runs argv with --json and decodes the response envelope.
func runEnvelope(t *testing.T, argv ...string) (int, map[string]any) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = Run(append(argv, "--json")) })
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("%v: stdout is not one JSON envelope: %v\n%s", argv, err, out)
	}
	return code, env
}

// nullPaths lists every JSON path under v whose value is null.
func nullPaths(prefix string, v any) []string {
	var out []string
	switch x := v.(type) {
	case nil:
		out = append(out, prefix)
	case map[string]any:
		for k, c := range x {
			out = append(out, nullPaths(prefix+"."+k, c)...)
		}
	case []any:
		for _, c := range x {
			out = append(out, nullPaths(prefix+"[]", c)...)
		}
	}
	return out
}

// A list in data is always a JSON array, never null, so a consumer can iterate
// without a null check; the one null in an envelope is error on success.
func TestDataNeverCarriesNull(t *testing.T) {
	dir := t.TempDir()
	cfg := scratchConfig(t, dir)
	base := []string{"--config", cfg, "--cwd", dir}
	cmds := [][]string{
		{"sync"},
		{"sync", "--apply"},
		{"sync", "--apply"},
		{"audit"},
		{"diff"},
		{"list"},
		{"discover"},
		{"review"},
		{"config"},
		{"reconcile"},
		{"reconcile", "--apply"},
		{"show", "claude-code"},
		{"show", "codex"},
		{"import", "claude-code"},
		{"import", "codex"},
		{"migrate", "claude-code"},
		{"migrate", "claude-code", "--apply"},
	}
	for _, c := range cmds {
		_, env := runEnvelope(t, append(c, base...)...)
		if env["schema_version"] != float64(schemaVersion) {
			t.Errorf("%v: schema_version = %v, want %d", c, env["schema_version"], schemaVersion)
		}
		if nulls := nullPaths("data", env["data"]); len(nulls) > 0 {
			t.Errorf("%v: null in data at %s", c, strings.Join(nulls, ", "))
		}
	}
}

// When one harness fails, the top-level error carries it (exit 1), so a
// consumer branching on error.code sees the failure without walking data.
func TestHarnessFailureSetsTopLevelError(t *testing.T) {
	dir := t.TempDir()
	cfg := scratchConfig(t, dir)
	// A file where the Codex notes directory belongs makes the Codex plan fail.
	writeFile(t, filepath.Join(dir, "codex", "memories", "extensions", "engram", "notes"), "not a dir\n")
	for _, c := range [][]string{{"sync"}, {"sync", "--apply"}, {"audit"}, {"diff"}, {"reconcile"}} {
		code, env := runEnvelope(t, append(c, "--config", cfg, "--cwd", dir)...)
		if code != exitError {
			t.Errorf("%v: exit = %d, want %d", c, code, exitError)
		}
		e, _ := env["error"].(map[string]any)
		if e == nil || e["code"] != "harness_failed" || !strings.Contains(e["message"].(string), "codex") {
			t.Errorf("%v: error = %v, want code harness_failed naming codex", c, env["error"])
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "claude")); err != nil {
		t.Errorf("the healthy claude-code harness should still sync: %v", err)
	}
}

package cli

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/agentexec"
)

// Following a review lead must reach validated, read-only curation, even when
// the agent proposes a destructive operation. No live model is needed here.
func TestReviewLeadsUseReadOnlyCuration(t *testing.T) {
	for _, entrypoint := range []string{"review", "reconcile"} {
		t.Run(entrypoint, func(t *testing.T) {
			dir := t.TempDir()
			canon := filepath.Join(dir, "canonical")
			seedCanon(t, canon, "shared-memory-lesson", "shared-memory-lesson-copy")
			cfg := filepath.Join(dir, "config 'quoted'.yaml")
			writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    disabled: true\n  codex:\n    disabled: true\n")
			before := storeTree(t, canon)

			code, resp := runEnvelope(t, entrypoint, "--config", cfg, "--json")
			if code != exitOK {
				t.Fatalf("review: %d %v", code, resp)
			}
			leads, _ := resp["next_steps"].([]any)
			if len(leads) != 1 {
				t.Fatalf("expected a known-positive near-duplicate lead: %v", resp)
			}
			command := leads[0].(map[string]any)["command"].(string)
			if !strings.HasPrefix(command, "engram ") {
				t.Fatalf("lead bypasses engram's validated proposer: %q", command)
			}
			// Parse the actual emitted shell command without running an agent.
			out, err := exec.Command("sh", "-c", "printf '%s\\000'"+strings.TrimPrefix(command, "engram")).Output()
			if err != nil {
				t.Fatal(err)
			}
			argv := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
			if len(argv) != 4 || argv[0] != "curate" || argv[1] != "--json" || argv[2] != "--config" || argv[3] != cfg {
				t.Fatalf("lead must preview curation against the inspected config: %q", argv)
			}

			called := false
			runner := func(inv agentexec.Invocation) ([]byte, error) {
				called = true
				toolsDisabled := false
				for i := 0; i+1 < len(inv.Argv); i++ {
					if inv.Argv[i] == "--tools" && inv.Argv[i+1] == "" {
						toolsDisabled = true
					}
				}
				if !toolsDisabled || !strings.Contains(inv.Stdin, "shared-memory-lesson-copy") {
					t.Errorf("lead must use the no-tools proposer with the canonical corpus: %v", inv.Argv)
				}
				proposal := map[string]any{"operations": []map[string]any{{"op": "remove", "name": "shared-memory-lesson", "reason": "test"}}}
				raw, _ := json.Marshal(proposal)
				return fakeClaudeRunner(string(raw))(inv)
			}
			code, resp = runCurate(t, curateEnv(argv[3], false, runner))
			if code != exitOK || !called {
				t.Fatalf("curation lead: %d called=%v %v", code, called, resp)
			}
			sameStore(t, before, storeTree(t, canon))
		})
	}
}

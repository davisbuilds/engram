package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/curate"
	"github.com/davisbuilds/engram/internal/lock"
)

// fakeClaudeRunner returns a claude `--output-format json` envelope whose result
// carries the given assistant text (a fenced JSON proposal in these tests), so
// the curate loop runs end-to-end without spawning a real model.
func fakeClaudeRunner(assistantText string) agentexec.Runner {
	return func(agentexec.Invocation) ([]byte, error) {
		env := map[string]any{"type": "result", "is_error": false, "result": assistantText}
		b, _ := json.Marshal(env)
		return b, nil
	}
}

func curateEnv(cfg string, apply bool, runner agentexec.Runner) *env {
	return &env{jsonMode: true, apply: apply, config: cfg, runnerFor: func(time.Duration) agentexec.Runner { return runner }}
}

func seedCanon(t *testing.T, canon string, names ...string) {
	t.Helper()
	for _, n := range names {
		writeFile(t, filepath.Join(canon, n+".md"),
			"---\nname: "+n+"\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	}
}

func TestCurateRequestedModelsAndOverridesReachRunner(t *testing.T) {
	for _, harness := range []struct{ name, model string }{
		{"claude-code", "claude-sonnet-5-5"}, {"codex", "gpt-6.1-terra"},
	} {
		for _, mode := range []string{"default", "config", "flags"} {
			t.Run(harness.name+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				canon := filepath.Join(dir, "canonical")
				seedCanon(t, canon, "model-lesson")
				cfg := filepath.Join(dir, "config.yaml")
				text := "canonical_root: " + canon + "\n"
				wantModel, wantEffort := harness.model, "high"
				args := []string{"--harness", harness.name}
				if mode != "default" {
					text += "curate:\n  models:\n    " + harness.name + ":\n      model: configured-model\n      effort: medium\n"
					wantModel, wantEffort = "configured-model", "medium"
				}
				if mode == "flags" {
					args = append(args, "--model", "flag-model", "--effort", "low")
					wantModel, wantEffort = "flag-model", "low"
				}
				writeFile(t, cfg, text)
				calls := 0
				runner := func(inv agentexec.Invocation) ([]byte, error) {
					calls++
					value := func(flag string) string {
						for i := 0; i+1 < len(inv.Argv); i++ {
							if inv.Argv[i] == flag {
								return inv.Argv[i+1]
							}
						}
						return ""
					}
					if got := value("--model"); got != wantModel {
						t.Errorf("requested model = %q, want %q", got, wantModel)
					}
					if harness.name == "claude-code" {
						if value("--effort") != wantEffort {
							t.Errorf("requested Claude effort: %v", inv.Argv)
						}
						return fakeClaudeRunner(`{"operations":[]}`)(inv)
					}
					if value("-c") != "model_reasoning_effort="+wantEffort {
						t.Errorf("requested Codex effort: %v", inv.Argv)
					}
					return []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{\\\"operations\\\":[]}\"}}\n"), nil
				}
				code, resp := runCurate(t, curateEnv(cfg, false, runner), args...)
				if code != exitOK || calls != 1 {
					t.Fatalf("curate: code=%d calls=%d response=%v", code, calls, resp)
				}
			})
		}
	}
}

// TestCurateApplyAppliesValidProposal pins the proposer/applier loop: a fake
// agent proposes a merge + a remove, and --apply mutates canonical accordingly.
func TestCurateApplyAppliesValidProposal(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "dup-a", "dup-b", "stale")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")

	proposal := "```json\n" + `{"operations":[
	  {"op":"merge","sources":["dup-a","dup-b"],"memory":{"name":"dup","description":"d","type":"lesson","scope":"global","body":"b\n"},"reason":"same"},
	  {"op":"remove","name":"stale","reason":"old"}
	]}` + "\n```"

	defer silenceStdout(t)()
	code := cmdCurate(curateEnv(cfg, true, fakeClaudeRunner(proposal)), "curate", []string{"--harness", "claude-code"})
	if code != exitOK {
		t.Fatalf("apply exit = %d, want %d", code, exitOK)
	}
	if _, err := os.Stat(filepath.Join(canon, "dup.md")); err != nil {
		t.Errorf("merged memory dup.md missing: %v", err)
	}
	for _, gone := range []string{"dup-a", "dup-b", "stale"} {
		if _, err := os.Stat(filepath.Join(canon, gone+".md")); !os.IsNotExist(err) {
			t.Errorf("%s should have been deleted", gone)
		}
	}
}

// TestCurateFailsClosedOnInvalidOp pins the safety property: a batch with any
// invalid operation is refused whole under --apply (exit 3, nothing mutated).
func TestCurateFailsClosedOnInvalidOp(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "real")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")

	// One valid remove, one remove of a nonexistent memory.
	proposal := "```json\n" + `{"operations":[
	  {"op":"remove","name":"real","reason":"ok"},
	  {"op":"remove","name":"ghost","reason":"nope"}
	]}` + "\n```"

	defer silenceStdout(t)()
	code := cmdCurate(curateEnv(cfg, true, fakeClaudeRunner(proposal)), "curate", []string{"--harness", "claude-code"})
	if code != exitConflicts {
		t.Fatalf("invalid-batch apply exit = %d, want %d", code, exitConflicts)
	}
	if _, err := os.Stat(filepath.Join(canon, "real.md")); err != nil {
		t.Errorf("real.md must survive a fail-closed batch: %v", err)
	}
}

// TestCurateDryRunDoesNotMutate pins that a dry run reports a plan without
// touching canonical, even for a fully-valid proposal.
func TestCurateDryRunDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "victim")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")

	proposal := "```json\n" + `{"operations":[{"op":"remove","name":"victim","reason":"x"}]}` + "\n```"
	defer silenceStdout(t)()
	code := cmdCurate(curateEnv(cfg, false, fakeClaudeRunner(proposal)), "curate", []string{"--harness", "claude-code"})
	if code != exitOK {
		t.Fatalf("dry-run exit = %d, want %d", code, exitOK)
	}
	if _, err := os.Stat(filepath.Join(canon, "victim.md")); err != nil {
		t.Errorf("dry run must not delete victim.md: %v", err)
	}
}

// A plain dry run keeps no plan, and a plain --apply asks the agent again, so
// the dry run's lead points at the saved-plan workflow, says the plan it saves
// is a new proposal, and never claims an --apply would commit the operations
// shown. It takes no lock either: not even the lock file is written.
func TestCurateDryRunLeadsToANewSavedPlan(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "victim")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")
	proposal := "```json\n" + `{"operations":[{"op":"remove","name":"victim","reason":"x"}]}` + "\n```"
	code, resp := runCurate(t, curateEnv(cfg, false, fakeClaudeRunner(proposal)), "--harness", "claude-code")
	if code != exitOK {
		t.Fatalf("dry-run exit = %d: %v", code, resp["error"])
	}
	next, _ := resp["next_steps"].([]any)
	if len(next) != 1 {
		t.Fatalf("next_steps = %v, want one lead", next)
	}
	lead, _ := next[0].(map[string]any)
	reason, _ := lead["reason"].(string)
	command, _ := lead["command"].(string)
	for _, want := range []string{"engram curate --harness claude-code --save-plan", "engram curate --plan", "--apply"} {
		if !strings.Contains(command, want) {
			t.Errorf("lead command %q lacks %q", command, want)
		}
	}
	if !strings.Contains(reason, "new proposal") || strings.Contains(command+reason, "commit them") {
		t.Errorf("lead must name the saved plan as a new proposal and not promise these operations: %v", lead)
	}
	if exists(filepath.Join(canon, lock.Name)) {
		t.Error("a curate dry run wrote the lock file")
	}
}

// TestCurateAgentRunFailureSurfaces pins that a runner error becomes an exit-1
// agent_run error rather than a crash or a silent success.
func TestCurateAgentRunFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "m")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")

	failing := func(agentexec.Invocation) ([]byte, error) { return nil, os.ErrPermission }
	defer silenceStdout(t)()
	code := cmdCurate(curateEnv(cfg, true, failing), "curate", []string{"--harness", "claude-code"})
	if code != exitError {
		t.Fatalf("agent run failure exit = %d, want %d", code, exitError)
	}
}

// The curate deadline reaches the runner: --timeout wins over curate.timeout in
// config, which wins over the default; an invalid value is a usage error and no
// agent runs.
func TestCurateTimeoutReachesTheRunner(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\ncurate:\n  timeout: 7m\n")
	defer silenceStdout(t)()
	run := func(args ...string) (int, time.Duration, bool) {
		var got time.Duration
		called := false
		e := &env{jsonMode: true, config: cfg, runnerFor: func(d time.Duration) agentexec.Runner {
			got, called = d, true
			return fakeClaudeRunner("```json\n{\"operations\": []}\n```")
		}}
		return cmdCurate(e, "curate", args), got, called
	}
	if _, d, _ := run(); d != 7*time.Minute {
		t.Errorf("config timeout: runner got %v, want 7m", d)
	}
	if _, d, _ := run("--timeout", "3m"); d != 3*time.Minute {
		t.Errorf("--timeout 3m: runner got %v, want 3m", d)
	}
	if code, _, called := run("--timeout", "soon"); code != exitUsage || called {
		t.Errorf("--timeout soon: exit %d, agent ran %v; want %d and no run", code, called, exitUsage)
	}
}

// The corpus reaches the agent on stdin, never in argv: a store past the OS
// argument limit would otherwise fail at exec.
func TestCurateSendsTheCorpusOnStdin(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	writeFile(t, filepath.Join(canon, "big.md"),
		"---\nname: big\ndescription: d\ntype: lesson\nscope: global\n---\ncorpus-marker body\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")
	defer silenceStdout(t)()
	for _, harness := range []string{"claude-code", "codex"} {
		var got agentexec.Invocation
		runner := func(inv agentexec.Invocation) ([]byte, error) {
			got = inv
			return nil, errors.New("stop after capturing the invocation")
		}
		cmdCurate(curateEnv(cfg, false, runner), "curate", []string{"--harness", harness})
		if !strings.Contains(got.Stdin, "corpus-marker") {
			t.Errorf("%s: the corpus is not on stdin", harness)
		}
		for _, a := range got.Argv {
			if strings.Contains(a, "corpus-marker") {
				t.Errorf("%s: the corpus is in argv", harness)
			}
		}
	}
}

// A batch that fails midway is rolled back and reported as apply, with nothing
// applied; one whose rollback also fails is apply_partial.
func TestCurateApplyReportsAFailedBatch(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	seedCanon(t, canon, "dup-a", "dup-b")
	writeFile(t, filepath.Join(canon, ".forgotten"), "in the way\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")
	proposal := "```json\n" + `{"operations":[
	  {"op":"merge","sources":["dup-a","dup-b"],"memory":{"name":"dup","description":"d","type":"lesson","scope":"global","body":"b\n"},"reason":"same"}
	]}` + "\n```"
	var code int
	out := captureStdout(t, func() {
		code = cmdCurate(curateEnv(cfg, true, fakeClaudeRunner(proposal)), "curate", []string{"--harness", "claude-code"})
	})
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out)
	}
	if code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if e, _ := env["error"].(map[string]any); e["code"] != "apply" {
		t.Errorf("error = %v, want code apply", env["error"])
	}
	if data, _ := env["data"].(map[string]any); data["applied"] == nil || len(data["applied"].([]any)) != 0 {
		t.Errorf("data.applied = %v, want empty after a rollback", data["applied"])
	}
	if exists(filepath.Join(canon, "dup.md")) || !exists(filepath.Join(canon, "dup-a.md")) {
		t.Error("canonical was not rolled back")
	}
	partial := &curate.PartialApplyError{Err: errors.New("apply merge: disk full"), Rollback: errors.New("not restored: a.md")}
	if got := applyErrorCode(fmt.Errorf("wrapped: %w", partial)); got != "apply_partial" {
		t.Errorf("applyErrorCode(partial) = %q, want apply_partial", got)
	}
}

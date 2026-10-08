package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/agentexec"
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

package agentexec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeArgvSeparatorAlwaysBeforePrompt(t *testing.T) {
	cases := [][]string{
		{}, // no tools
		{"Read"},
		{"Read", "Edit"},
	}
	for _, tools := range cases {
		argv := ClaudeArgv("the prompt", tools...)
		if len(argv) < 4 {
			t.Fatalf("argv too short: %v", argv)
		}
		if argv[0] != "claude" || argv[1] != "-p" {
			t.Errorf("argv should start with claude -p: %v", argv)
		}
		if argv[len(argv)-1] != "the prompt" {
			t.Errorf("prompt must be the last arg: %v", argv)
		}
		if argv[len(argv)-2] != "--" {
			t.Errorf("`--` must immediately precede the prompt: %v", argv)
		}
		// The prompt must never be adjacent to a tool flag value.
		for i, a := range argv[:len(argv)-1] {
			if a == "the prompt" {
				t.Errorf("prompt appeared before the separator at index %d: %v", i, argv)
			}
		}
	}
}

func TestClaudeCommandQuotesPromptAfterSeparator(t *testing.T) {
	cmd := ClaudeCommand("compare a and b", "Read")
	if !strings.Contains(cmd, `-- "compare a and b"`) {
		t.Errorf("command must place a quoted prompt after --: %q", cmd)
	}
}

func TestCodexArgv(t *testing.T) {
	argv := CodexArgv("do the thing")
	if len(argv) != 3 || argv[0] != "codex" || argv[1] != "exec" || argv[2] != "do the thing" {
		t.Errorf("unexpected codex argv: %v", argv)
	}
}

func TestClaudeArgvOptsCarriesModelEffortAndJSON(t *testing.T) {
	argv := ClaudeArgvOpts("prompt here", Options{Model: "claude-sonnet-5", Effort: "high"})
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--output-format json", "--model claude-sonnet-5", "--effort high",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
	if argv[len(argv)-2] != "--" || argv[len(argv)-1] != "prompt here" {
		t.Errorf("prompt must follow -- as the last arg: %v", argv)
	}
	// The proposer must run with all tools explicitly disabled (--tools ""): the
	// corpus is untrusted, and an absent --allowedTools does not disable tools.
	found := false
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--tools" && argv[i+1] == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("curate argv must disable all tools via --tools \"\": %v", argv)
	}
}

func TestCodexArgvOptsCarriesModelAndEffort(t *testing.T) {
	argv := CodexArgvOpts("prompt here", Options{Model: "gpt-5.6-terra", Effort: "high"})
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--model gpt-5.6-terra") {
		t.Errorf("argv missing model: %v", argv)
	}
	if !strings.Contains(joined, "model_reasoning_effort=high") {
		t.Errorf("codex effort must go through -c model_reasoning_effort: %v", argv)
	}
	if argv[len(argv)-1] != "prompt here" {
		t.Errorf("prompt must be the last arg: %v", argv)
	}
}

// The curate codex run must pin the structured-output and sandbox flags: --json
// (so the proposal is read from an event, not raw stdout), a read-only sandbox
// (untrusted corpus, engram stays the sole mutator), and the run-anywhere /
// no-session-litter flags.
func TestCodexArgvOptsPinsHardeningFlags(t *testing.T) {
	argv := CodexArgvOpts("prompt here", Options{})
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--json", "--sandbox read-only", "--skip-git-repo-check", "--ephemeral",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("codex curate argv missing %q: %v", want, argv)
		}
	}
	if argv[len(argv)-2] != "--" || argv[len(argv)-1] != "prompt here" {
		t.Errorf("prompt must follow -- as the last arg: %v", argv)
	}
}

// codexLine builds one `codex exec --json` item.completed event line with the
// given item type, matching the real stream shape captured from codex 0.148.0.
func codexItemLine(t *testing.T, itemType, text string) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type": "item.completed",
		"item": map[string]any{"id": "item_x", "type": itemType, "text": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

func TestExtractCodexTextReturnsFinalAgentMessage(t *testing.T) {
	proposal := "```json\n{\"operations\":[{\"op\":\"remove\",\"name\":\"x\"}]}\n```"
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		codexItemLine(t, "agent_message", proposal),
		`{"type":"turn.completed","usage":{"output_tokens":5}}`,
	}, "\n")
	got, err := ExtractCodexText([]byte(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != proposal {
		t.Errorf("got %q, want the agent_message text %q", got, proposal)
	}
}

// The regression that motivated parsing the event stream: a ```json block inside
// a non-agent_message item (reasoning, a command log) must never be mistaken for
// the proposal. The old raw-stdout path would have surfaced this decoy.
func TestExtractCodexTextExcludesDecoyOutsideAgentMessage(t *testing.T) {
	decoy := "here is my thinking ```json\n{\"operations\":[{\"op\":\"remove\",\"name\":\"WRONG\"}]}\n``` end"
	real := "```json\n{\"operations\":[{\"op\":\"remove\",\"name\":\"RIGHT\"}]}\n```"
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t1"}`,
		codexItemLine(t, "reasoning", decoy),
		codexItemLine(t, "agent_message", real),
		`{"type":"turn.completed"}`,
	}, "\n")
	got, err := ExtractCodexText([]byte(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "WRONG") {
		t.Errorf("decoy from a non-agent_message item leaked into the result: %q", got)
	}
	if got != real {
		t.Errorf("got %q, want the agent_message text %q", got, real)
	}
}

func TestExtractCodexTextTakesLastAgentMessage(t *testing.T) {
	stream := strings.Join([]string{
		codexItemLine(t, "agent_message", "first"),
		codexItemLine(t, "agent_message", "final"),
	}, "\n")
	got, err := ExtractCodexText([]byte(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "final" {
		t.Errorf("got %q, want the last agent_message %q", got, "final")
	}
}

func TestExtractCodexTextErrorsWithoutAgentMessage(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		codexItemLine(t, "reasoning", "thinking but never answered"),
		`{"type":"turn.completed"}`,
	}, "\n")
	if _, err := ExtractCodexText([]byte(stream)); err == nil {
		t.Error("expected an error when no agent_message is present")
	}
}

// A JSON-looking line that fails to parse is corruption and must fail closed:
// otherwise a valid *earlier* agent_message would survive as the result and
// curate --apply could execute a superseded proposal (Codex PR #2 P1).
func TestExtractCodexTextFailsClosedOnMalformedEvent(t *testing.T) {
	stream := strings.Join([]string{
		codexItemLine(t, "agent_message", "superseded proposal"),
		`{"type":"item.completed","item":{"id":"item_1","type":"agent_mess`, // truncated final line
	}, "\n")
	if _, err := ExtractCodexText([]byte(stream)); err == nil {
		t.Error("a malformed JSON event after a valid message must fail closed, not return the earlier message")
	}
}

// Genuinely non-JSON lines (a blank line, a banner) are not events and cannot be
// the final message, so they are skipped rather than treated as corruption.
func TestExtractCodexTextSkipsNonJSONNoise(t *testing.T) {
	stream := strings.Join([]string{
		"",
		"some non-json banner line",
		codexItemLine(t, "agent_message", "answer"),
		"",
	}, "\n")
	got, err := ExtractCodexText([]byte(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "answer" {
		t.Errorf("got %q, want %q", got, "answer")
	}
}

// A proposal over the full corpus can exceed bufio.Scanner's 64KB default line
// cap; the raised buffer must carry it intact.
func TestExtractCodexTextHandlesLargeMessage(t *testing.T) {
	big := strings.Repeat("x", 200*1024)
	stream := codexItemLine(t, "agent_message", big)
	got, err := ExtractCodexText([]byte(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != big {
		t.Errorf("large agent_message was truncated: got %d bytes, want %d", len(got), len(big))
	}
}

func TestExtractClaudeTextReturnsResult(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":false,"result":"the answer","total_cost_usd":0.01}`)
	got, err := ExtractClaudeText(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the answer" {
		t.Errorf("got %q, want %q", got, "the answer")
	}
}

func TestExtractClaudeTextSurfacesError(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":true,"result":"model refused"}`)
	if _, err := ExtractClaudeText(stdout); err == nil {
		t.Error("expected an error when is_error is true")
	}
}

func TestExtractClaudeTextRejectsGarbage(t *testing.T) {
	if _, err := ExtractClaudeText([]byte("not json at all")); err == nil {
		t.Error("expected an error on non-JSON stdout")
	}
}

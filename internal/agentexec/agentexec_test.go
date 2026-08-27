package agentexec

import (
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

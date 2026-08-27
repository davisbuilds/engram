// Package agentexec builds argv for headless agent invocations. Its one job is
// to make the `claude -p` `--` separator structurally impossible to omit: the
// positional prompt always follows a `--`, so variadic tool flags
// (--allowedTools / --disallowedTools) can never swallow it. This is the footgun
// the spec calls out; encoding it here means it cannot be reintroduced by hand.
package agentexec

import "strings"

// ClaudeArgv builds the argv for a headless `claude -p` run. Any tool flags come
// first, then `--`, then the positional prompt — always in that order.
func ClaudeArgv(prompt string, allowedTools ...string) []string {
	argv := []string{"claude", "-p"}
	if len(allowedTools) > 0 {
		argv = append(argv, "--allowedTools")
		argv = append(argv, allowedTools...)
	}
	return append(argv, "--", prompt)
}

// CodexArgv builds the argv for a headless `codex exec` run.
func CodexArgv(prompt string) []string {
	return []string{"codex", "exec", prompt}
}

// ClaudeCommand renders ClaudeArgv as a display string with the prompt quoted,
// suitable for a next_step lead a human or agent can copy and run.
func ClaudeCommand(prompt string, allowedTools ...string) string {
	argv := ClaudeArgv(prompt, allowedTools...)
	parts := make([]string, len(argv))
	for i, a := range argv {
		if i == len(argv)-1 {
			parts[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// Package agentexec builds argv for headless agent invocations and extracts the
// assistant's final message from each harness's structured output.
//
// Building argv here makes the `claude -p` `--` separator structurally
// impossible to omit — the positional prompt always follows a `--`, so variadic
// tool flags (--allowedTools / --disallowedTools) can never swallow it — and it
// pins the hardening flags each headless run needs: no tools / a read-only
// sandbox against an untrusted corpus, and structured output (`claude`'s JSON
// envelope, `codex`'s JSONL event stream) so the final message is recovered
// deterministically rather than by scanning raw stdout for a fenced block.
package agentexec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Options carries the model and reasoning-effort knobs for a headless run. Both
// are optional; an empty field omits its flag so the harness's own default
// applies. Neither is compiled in as a default here — the caller resolves
// defaults (from config) before building argv.
type Options struct {
	Model  string
	Effort string
}

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

// ClaudeArgvOpts builds a headless `claude -p` run for curate: JSON output,
// model/effort passthrough, and — critically — all tools explicitly disabled via
// `--tools ""`. The corpus is untrusted (prompt-injected memory content is
// possible), so the proposer must have no filesystem access: an *absent*
// --allowedTools does NOT disable tools (the session's own settings may still
// permit Edit/Bash), whereas `--tools ""` is the documented no-tools mode. This
// keeps engram the sole mutator even against a hostile corpus. The `--` separator
// still guards the positional prompt.
func ClaudeArgvOpts(prompt string, opts Options, allowedTools ...string) []string {
	argv := []string{"claude", "-p", "--output-format", "json", "--tools", ""}
	if opts.Model != "" {
		argv = append(argv, "--model", opts.Model)
	}
	if opts.Effort != "" {
		argv = append(argv, "--effort", opts.Effort)
	}
	if len(allowedTools) > 0 {
		argv = append(argv, "--allowedTools")
		argv = append(argv, allowedTools...)
	}
	return append(argv, "--", prompt)
}

// CodexArgvOpts builds a headless `codex exec` run for curate. Beyond model and
// effort it pins four flags, each load-bearing:
//   - --json emits the structured JSONL event stream, so the proposal is read
//     from the final `agent_message` event (see ExtractCodexText) rather than by
//     scanning interleaved session logs for a fenced block.
//   - --sandbox read-only denies the proposer write access. The corpus is
//     untrusted (prompt injection is possible), so this keeps engram the sole
//     mutator — the same invariant the Claude path enforces with `--tools ""`.
//   - --skip-git-repo-check lets curate run from any cwd; the proposer needs no repo.
//   - --ephemeral keeps a one-shot proposer from persisting session files to disk.
//
// Codex has no `--effort` flag; reasoning effort is a config override, hence
// `-c model_reasoning_effort=<level>`. The `--` guards the positional prompt.
func CodexArgvOpts(prompt string, opts Options) []string {
	argv := []string{
		"codex", "exec",
		"--json",
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--ephemeral",
	}
	if opts.Model != "" {
		argv = append(argv, "--model", opts.Model)
	}
	if opts.Effort != "" {
		argv = append(argv, "-c", "model_reasoning_effort="+opts.Effort)
	}
	return append(argv, "--", prompt)
}

// Runner executes an argv and returns the process's stdout. It is injected so
// tests exercise the curate loop without ever spawning a real, paid model.
type Runner func(argv []string) ([]byte, error)

// ExecRunner is the production Runner: it runs the argv as a subprocess and
// returns stdout, folding stderr into the error on failure.
func ExecRunner(argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // argv is engram-built, not user-injected
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), fmt.Errorf("%s failed: %s", argv[0], msg)
	}
	return out.Bytes(), nil
}

// claudeResult is the shape of `claude -p --output-format json` stdout: a single
// envelope whose result field holds the assistant's final message.
type claudeResult struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// ExtractClaudeText pulls the assistant's final message out of the
// `--output-format json` envelope, surfacing an is_error run as a Go error.
func ExtractClaudeText(stdout []byte) (string, error) {
	var r claudeResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &r); err != nil {
		return "", fmt.Errorf("parse claude json envelope: %w", err)
	}
	if r.IsError {
		return "", fmt.Errorf("claude reported an error result: %s", r.Result)
	}
	return r.Result, nil
}

// codexEvent is one line of the `codex exec --json` JSONL event stream. Only the
// fields engram needs are decoded; every other event type and field is ignored.
type codexEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

// ExtractCodexText parses the `codex exec --json` event stream and returns the
// text of the agent's final message — the last `item.completed` event whose item
// is an `agent_message`. Isolating that one event is what hardens Codex curate:
// reasoning items, command logs, and any stray ```json block in the session
// preamble are structurally excluded, so ParseProposal only ever sees the model's
// actual answer. An exit-0 run that emitted no assistant message is an error, not
// an empty proposal.
func ExtractCodexText(stdout []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	// A proposal over the full corpus can exceed the 64KB default line cap.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var last string
	found := false
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue // defensively skip any non-JSON line
		}
		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // a single malformed event line is not fatal to the stream
		}
		if ev.Type == "item.completed" && ev.Item != nil && ev.Item.Type == "agent_message" {
			last = ev.Item.Text
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("scan codex json stream: %w", err)
	}
	if !found {
		return "", fmt.Errorf("no agent message in codex json output")
	}
	return last, nil
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

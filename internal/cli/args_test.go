package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// scratchConfig writes a config whose canonical root and both harness homes
// live under dir, so a command that wrongly proceeds writes only scratch state.
func scratchConfig(t *testing.T, dir string) string {
	t.Helper()
	canon := filepath.Join(dir, "canonical")
	writeFile(t, filepath.Join(canon, "a-mem.md"),
		"---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+
		filepath.Join(dir, "claude")+"\n  codex:\n    home: "+filepath.Join(dir, "codex")+"\n")
	return cfg
}

// A mistyped or malformed argument is a usage error (exit 2) before anything
// runs; the command must never proceed as if the argument were absent.
func TestMalformedArgumentsAreUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"mistyped --cwd with --apply": {"reconcile", "--cdw", "/elsewhere", "--apply"},
		"--cwd with no value":         {"reconcile", "--apply", "--cwd"},
		"--cwd followed by a flag":    {"sync", "--cwd", "--apply"},
		"stray positional":            {"sync", "extra", "--apply"},
		"mistyped curate flag":        {"curate", "--harnes", "codex"},
		"curate flag prefix":          {"curate", "--harnessX", "codex"},
		"share flag prefix":           {"share", "a-mem", "--tomato", "global"},
		"dangling --refresh":          {"import", "claude-code", "--refresh"},
		"second import harness":       {"import", "claude-code", "codex"},
		"unknown show flag":           {"show", "claude-code", "--verbose"},
		"remember stray positional":   {"remember", "--name", "x-mem", "--description", "d", "--type", "lesson", "stray"},
		"hook unknown flag":           {"hook", "print", "--apply-now"},
		"--config=":                   {"discover", "--config="},
	}
	for label, argv := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			cfg := scratchConfig(t, dir)
			defer silenceStdout(t)()
			args := append([]string{"--json"}, argv...)
			if label != "--config=" {
				args = append(args, "--config", cfg)
			}
			if code := Run(args); code != exitUsage {
				t.Errorf("exit = %d, want %d (usage)", code, exitUsage)
			}
			for _, p := range []string{"claude", "codex"} {
				if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
					t.Errorf("a rejected invocation wrote under the %s home", p)
				}
			}
		})
	}
}

// Well-formed arguments still parse: both value forms, repeats, and booleans.
func TestParseArgsAcceptsWellFormedArguments(t *testing.T) {
	spec := argSpec{positionals: 1, values: []string{"--refresh"}, bools: []string{"--all"}}
	p, rerr := parseArgs([]string{"codex", "--refresh", "a", "--refresh=b,c", "--all"}, spec)
	if rerr != nil {
		t.Fatalf("parseArgs: %v", rerr)
	}
	if len(p.pos) != 1 || p.pos[0] != "codex" {
		t.Errorf("positionals = %v, want [codex]", p.pos)
	}
	if got := p.vals["--refresh"]; len(got) != 2 || got[0] != "a" || got[1] != "b,c" {
		t.Errorf("--refresh values = %v, want [a b,c]", got)
	}
	if !p.bools["--all"] {
		t.Error("--all not recorded")
	}
}

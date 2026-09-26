package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// --help anywhere means "show help", never "run the command": an agent that
// appends --help to learn about sync --apply must not apply it.
func TestHelpAfterSubcommandRunsNothing(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	writeFile(t, filepath.Join(canon, "m.md"),
		"---\nname: h-mem\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+
		"\n  codex:\n    home: "+filepath.Join(dir, "codex")+"\n")
	defer silenceStdout(t)()

	for _, h := range []string{"--help", "-h"} {
		code := Run([]string{"sync", "--apply", h, "--config", cfg, "--cwd", "/work/x", "--json"})
		if code != exitOK {
			t.Errorf("%s exit = %d, want %d", h, code, exitOK)
		}
		if _, err := os.Stat(filepath.Join(claude, "projects")); err == nil {
			t.Fatalf("sync --apply %s wrote renders", h)
		}
	}
}

// A value flag matches only its own name, not every flag sharing its prefix:
// --configure must not swallow the next argument as a --config path.
func TestValueFlagDoesNotSwallowAPrefixedFlag(t *testing.T) {
	defer silenceStdout(t)()
	if code := Run([]string{"--configure", "no-such-command", "--json"}); code != exitUsage {
		t.Errorf("exit = %d, want %d (unknown command); --configure swallowed it", code, exitUsage)
	}
}

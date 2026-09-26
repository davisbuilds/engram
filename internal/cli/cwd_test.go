package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davisbuilds/engram/internal/slug"
)

// Spellings of one directory must land in the one project slug Claude Code
// reads; a trailing slash or an unexpanded ~ would render into a slug no
// session ever loads.
func TestCwdSpellingsResolveToOneSlug(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	writeFile(t, filepath.Join(canon, "m.md"),
		"---\nname: cwd-mem\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+
		"\n  codex:\n    disabled: true\n")
	defer silenceStdout(t)()

	proj := filepath.Join(home, "proj")
	for _, spelling := range []string{proj + "/", "~/proj", filepath.Join(home, "x", "..", "proj")} {
		Run([]string{"sync", "--apply", "--config", cfg, "--cwd", spelling, "--json"})
	}
	entries, err := os.ReadDir(filepath.Join(claude, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	want := slug.ForCwd(proj)
	for _, e := range entries {
		if e.Name() != want {
			t.Errorf("rendered into slug %q, want only %q", e.Name(), want)
		}
	}
}

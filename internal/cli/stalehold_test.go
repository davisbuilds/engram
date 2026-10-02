package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// staleHoldFixture renders one memory into temp Claude and Codex homes and
// returns the config args plus the rendered Claude file.
func staleHoldFixture(t *testing.T) (canon string, base []string, rendered string) {
	t.Helper()
	dir := t.TempDir()
	canon = filepath.Join(dir, "canonical")
	writeFile(t, filepath.Join(canon, "keep-me.md"),
		"---\nname: keep-me\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+
		filepath.Join(dir, "claude")+"\n  codex:\n    home: "+filepath.Join(dir, "codex")+"\n")
	base = []string{"--config", cfg, "--cwd", "/work/x", "--json"}
	defer silenceStdout(t)()
	if code := Run(append([]string{"sync", "--apply"}, base...)); code != exitOK {
		t.Fatalf("initial apply exit = %d", code)
	}
	// A global memory renders into the shared dir, which the hold covers too.
	rendered = filepath.Join(dir, "claude", "engram", "memory", "keep-me.md")
	if _, err := os.Stat(rendered); err != nil {
		t.Fatal(err)
	}
	return canon, base, rendered
}

// A canonical file that stops parsing (a crashed editor save, a bad merge) has
// not been removed; its renders must survive until it parses again.
func TestSyncKeepsRendersOfAnUnparseableCanonical(t *testing.T) {
	canon, base, rendered := staleHoldFixture(t)
	writeFile(t, filepath.Join(canon, "keep-me.md"), "---\nname: keep-me\n")
	defer silenceStdout(t)()
	Run(append([]string{"sync", "--apply"}, base...))
	if _, err := os.Stat(rendered); err != nil {
		t.Errorf("render deleted while its canonical was unparseable: %v", err)
	}
}

// A missing canonical root (typo'd config, unmounted volume) is not an empty
// store; nothing it rendered may be pruned.
func TestSyncKeepsRendersWhenCanonicalRootIsMissing(t *testing.T) {
	canon, base, rendered := staleHoldFixture(t)
	if err := os.RemoveAll(canon); err != nil {
		t.Fatal(err)
	}
	defer silenceStdout(t)()
	Run(append([]string{"sync", "--apply"}, base...))
	if _, err := os.Stat(rendered); err != nil {
		t.Errorf("render deleted while the canonical root was missing: %v", err)
	}
}

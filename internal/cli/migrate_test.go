package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunMigrateRoundTrip is the end-to-end dogfood in miniature: a hand-authored
// Claude memory with a free-text name is imported (normalized to kebab), and then
// migrate adopts the still-present original in place so a subsequent sync into the
// same slug produces no CONFLICT and no duplicate. This exercises the real
// import → migrate → sync pipeline, including provenance.source flowing through.
func TestRunMigrateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	slugMem := filepath.Join(claude, "projects", "-work-x", "memory")
	// Hand-authored: free-text name, filename differs from the canonical name.
	writeFile(t, filepath.Join(slugMem, "obsidian_vault.md"),
		"---\nname: Obsidian vault location\ndescription: where\nmetadata:\n  type: reference\n---\nvault body\n")
	// A hand-authored MEMORY.md index line pointing at the original file.
	writeFile(t, filepath.Join(slugMem, "MEMORY.md"),
		"- [Obsidian vault location](obsidian_vault.md) — where\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n")
	defer silenceStdout(t)()
	c := []string{"--config", cfg, "--cwd", "/work/x", "--json"}

	// 1) Import → canonical obsidian-vault-location.md (normalized).
	if code := Run(append([]string{"import", "claude-code", "--apply"}, c...)); code != exitOK {
		t.Fatalf("import exit = %d", code)
	}

	// 2) Migrate dry-run classifies the original as ADOPT.
	out := captureStdout(t, func() {
		if code := Run(append([]string{"migrate", "claude-code"}, c...)); code != exitOK {
			t.Fatalf("migrate dry-run exit = %d", code)
		}
	})
	if !strings.Contains(out, "\"ADOPT\"") || !strings.Contains(out, "obsidian_vault") {
		t.Fatalf("migrate dry-run should classify obsidian_vault as ADOPT; got:\n%s", out)
	}
	// Dry-run wrote nothing: the original file is still there.
	if _, err := os.Stat(filepath.Join(slugMem, "obsidian_vault.md")); err != nil {
		t.Fatalf("dry-run must not modify the slug: %v", err)
	}

	// 3) Migrate --apply adopts the original in place.
	if code := Run(append([]string{"migrate", "claude-code", "--apply"}, c...)); code != exitOK {
		t.Fatalf("migrate --apply exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(slugMem, "obsidian_vault.md")); !os.IsNotExist(err) {
		t.Errorf("original obsidian_vault.md should be gone after adopt")
	}
	adopted := filepath.Join(slugMem, "obsidian-vault-location.md")
	got, err := os.ReadFile(adopted)
	if err != nil {
		t.Fatalf("adopted file missing: %v", err)
	}
	if !strings.Contains(string(got), "origin: engram-sync") {
		t.Errorf("adopted file is not engram-owned:\n%s", got)
	}

	// 4) A subsequent sync into the same slug is clean — no CONFLICT, no duplicate.
	out = captureStdout(t, func() {
		if code := Run(append([]string{"sync", "claude-code"}, c...)); code != exitOK {
			t.Fatalf("post-migrate sync exit = %d", code)
		}
	})
	if strings.Contains(out, "CONFLICT") {
		t.Errorf("post-migrate sync still reports a CONFLICT:\n%s", out)
	}
}

// TestRunMigrateCodexUnsupported pins that migrate refuses codex (single
// consolidated source) with a usage error rather than acting.
func TestRunMigrateCodexUnsupported(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+filepath.Join(dir, "canonical")+"\nharnesses:\n  codex:\n    home: "+dir+"\n")
	defer silenceStdout(t)()
	if code := Run([]string{"migrate", "codex", "--config", cfg, "--json"}); code != exitUsage {
		t.Errorf("migrate codex exit = %d, want %d", code, exitUsage)
	}
}

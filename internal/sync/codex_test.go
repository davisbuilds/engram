package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
)

func codexTarget(dir string, mems ...*schema.CanonicalMemory) CodexTarget {
	return CodexTarget{
		ExtensionDir: dir,
		Desired:      mems,
		Now:          func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

func codexNoteExistsFor(t *testing.T, extDir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(extDir, "notes"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(extDir, "notes", e.Name()))
		if err != nil {
			continue
		}
		if n, _, ok := marker.CodexNoteName(string(b)); ok && n == name {
			return true
		}
	}
	return false
}

func TestCodexApplyCreatesNoteAndInstructions(t *testing.T) {
	dir := t.TempDir()
	res, err := codexTarget(dir, mem("codex-lesson")).Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) == 0 {
		t.Fatal("expected applied actions")
	}
	if _, err := os.Stat(filepath.Join(dir, "instructions.md")); err != nil {
		t.Errorf("instructions.md should be written: %v", err)
	}
	if !codexNoteExistsFor(t, dir, "codex-lesson") {
		t.Error("no engram-marked note for codex-lesson")
	}
}

func TestCodexApplySecondRunIsNoop(t *testing.T) {
	dir := t.TempDir()
	tg := codexTarget(dir, mem("codex-lesson"), mem("beta-lesson"))
	res, err := tg.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) == 0 {
		t.Fatal("first apply should create (non-degenerate)")
	}
	plan, err := tg.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 0 {
		t.Errorf("second run is not a no-op despite timestamped filenames: %v", plan)
	}
}

func TestCodexStaleRemovesOwnedNoteOnly(t *testing.T) {
	dir := t.TempDir()
	if _, err := codexTarget(dir, mem("orphan-lesson")).Apply(); err != nil {
		t.Fatal(err)
	}
	// A foreign (unmarked) file in the notes dir must be preserved.
	foreign := filepath.Join(dir, "notes", "2020-01-01T00-00-00-foreign.md")
	if err := os.WriteFile(foreign, []byte("# not an engram note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := codexTarget(dir, mem("keeper-lesson")).Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if kinds(res.Applied)[Stale] != 1 {
		t.Errorf("expected exactly 1 STALE (the owned orphan note); got %v", res.Applied)
	}
	if codexNoteExistsFor(t, dir, "orphan-lesson") {
		t.Error("owned orphan note should have been removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("foreign note must be preserved")
	}
}

// The Codex notes directory is shared by every cwd: a note another session
// rendered is not stale just because this session does not want it.
func TestCodexStaleSparesNotesOutOfView(t *testing.T) {
	dir := t.TempDir()
	alpha := mem("alpha-lesson")
	alpha.Scope = "project:alpha"
	if _, err := codexTarget(dir, alpha, mem("orphan-lesson")).Apply(); err != nil {
		t.Fatal(err)
	}
	tg := codexTarget(dir)
	tg.InView = func(_, scope string) bool { return scope == "global" }
	res, err := tg.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if kinds(res.Applied)[Stale] != 1 {
		t.Errorf("expected exactly 1 STALE (the in-view orphan); got %v", res.Applied)
	}
	if codexNoteExistsFor(t, dir, "orphan-lesson") {
		t.Error("in-view orphan note should have been removed")
	}
	if !codexNoteExistsFor(t, dir, "alpha-lesson") {
		t.Error("note rendered for another project was removed")
	}
}

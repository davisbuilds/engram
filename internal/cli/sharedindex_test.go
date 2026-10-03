package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// turnSharedIndexOff rewrites the fixture config (c[1], after --config) so the
// claude-code harness sets shared_index: false.
func turnSharedIndexOff(t *testing.T, c []string) {
	t.Helper()
	b, err := os.ReadFile(c[1])
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, c[1], string(b)+"    shared_index: false\n")
}

func runQuiet(t *testing.T, args ...string) int {
	t.Helper()
	defer silenceStdout(t)()
	return Run(args)
}

// TestSharedIndexOffRendersPerProject pins the opt-out: with the shared index
// off, a memory every project sees renders into the project slug, as it did
// before the index existed, and engram writes no shared dir or rules file.
func TestSharedIndexOffRendersPerProject(t *testing.T) {
	claude, c := sharedFixture(t)
	turnSharedIndexOff(t, c)
	if code := runQuiet(t, append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("sync exit = %d", code)
	}
	slug := filepath.Join(claude, "projects", "-work-x", "memory")
	for _, n := range []string{"g-mem", "p-mem", "n-mem"} {
		if !exists(filepath.Join(slug, n+".md")) {
			t.Errorf("%s not rendered into the project slug", n)
		}
	}
	if exists(filepath.Join(claude, "engram", "memory", "g-mem.md")) {
		t.Error("the shared dir was written with the shared index off")
	}
	if exists(filepath.Join(claude, "rules", "engram-memory.md")) {
		t.Error("the rules file was written with the shared index off")
	}
}

// TestTurningSharedIndexOffCleansUp pins the transition: engram's shared renders
// and its rules file are removed, and the memory moves into the slug.
func TestTurningSharedIndexOffCleansUp(t *testing.T) {
	claude, c := sharedFixture(t)
	sync := append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)
	if code := runQuiet(t, sync...); code != exitOK {
		t.Fatalf("seed sync exit = %d", code)
	}
	turnSharedIndexOff(t, c)
	if code := runQuiet(t, sync...); code != exitOK {
		t.Fatalf("sync after turning the index off exit = %d", code)
	}
	if exists(filepath.Join(claude, "engram", "memory", "g-mem.md")) {
		t.Error("the shared render outlived the shared index")
	}
	if exists(filepath.Join(claude, "rules", "engram-memory.md")) {
		t.Error("the rules file still imports an index that is off")
	}
	if !exists(filepath.Join(claude, "projects", "-work-x", "memory", "g-mem.md")) {
		t.Error("the memory did not move into the project slug")
	}
}

// TestSharedIndexOffKeepsThenSettlesAnEdit pins that turning the index off never
// loses an edit made in place: the edited render is held, reconcile imports it,
// and once canonical says what the edit says the render is removed and the slug
// carries the edit.
func TestSharedIndexOffKeepsThenSettlesAnEdit(t *testing.T) {
	claude, c, canon, render := editedShared(t)
	turnSharedIndexOff(t, c)
	if code := runQuiet(t, append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)...); code != exitConflicts {
		t.Fatalf("sync exit = %d, want %d for the held edit", code, exitConflicts)
	}
	if b, err := os.ReadFile(render); err != nil || !strings.Contains(string(b), sharedEdit) {
		t.Fatalf("the edited render was not kept (err=%v)", err)
	}
	if code := runQuiet(t, append([]string{"reconcile", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("reconcile exit = %d", code)
	}
	if got := gMemBody(t, canon); got != sharedEdit {
		t.Errorf("canonical body = %q, want the edit", got)
	}
	if exists(render) {
		t.Error("the shared render was kept after canonical took its edit")
	}
	b, err := os.ReadFile(filepath.Join(claude, "projects", "-work-x", "memory", "g-mem.md"))
	if err != nil || !strings.Contains(string(b), sharedEdit) {
		t.Errorf("the slug render does not carry the edit (err=%v)", err)
	}
}

// TestMigrateSharedRefusesWithIndexOff pins the guard: there is no shared render
// to retire originals into, so migrate --shared is a usage error.
func TestMigrateSharedRefusesWithIndexOff(t *testing.T) {
	_, c := sharedFixture(t)
	turnSharedIndexOff(t, c)
	code, env := runEnvelope(t, append([]string{"migrate", "claude-code", "--shared"}, c...)...)
	if code != exitUsage {
		t.Fatalf("migrate --shared exit = %d, want %d", code, exitUsage)
	}
	if e, _ := env["error"].(map[string]any); e["code"] != "shared_index_disabled" {
		t.Errorf("error = %v, want shared_index_disabled", env["error"])
	}
}

// TestConfigReportsSharedIndex pins discoverability: config says whether the
// shared index is on.
func TestConfigReportsSharedIndex(t *testing.T) {
	_, c := sharedFixture(t)
	for _, want := range []bool{true, false} {
		if !want {
			turnSharedIndexOff(t, c)
		}
		_, env := runEnvelope(t, append([]string{"config"}, c...)...)
		data, _ := env["data"].(map[string]any)
		if got, ok := data["claude_shared_index"].(bool); !ok || got != want {
			t.Errorf("claude_shared_index = %v, want %v", data["claude_shared_index"], want)
		}
	}
}

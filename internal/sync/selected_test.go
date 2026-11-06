package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

func TestSelectedClaudeDeliveryPreservesUnselectedState(t *testing.T) {
	dir := t.TempDir()
	if _, err := target(dir, mem("selected"), mem("unselected"), mem("stale")).Apply(); err != nil {
		t.Fatal(err)
	}
	beforeUnselected := readOr(t, filepath.Join(dir, "unselected.md"))
	beforeStale := readOr(t, filepath.Join(dir, "stale.md"))
	index := filepath.Join(dir, "MEMORY.md")
	writeAt(t, index, readOr(t, index)+"foreign index line\n")
	beforeIndex := readOr(t, index)
	selected, unselected := mem("selected"), mem("unselected")
	selected.Body, unselected.Body = "corrected selected\n", "must not render\n"
	tg := target(dir, selected, unselected, mem("new-selected"), mem("new-unselected"))
	tg.Names = map[string]bool{"selected": true, "new-selected": true, "stale": true}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || kinds(res.Applied)[Update] != 1 || kinds(res.Applied)[Create] != 1 {
		t.Fatalf("selected apply = %+v", res)
	}
	if !strings.Contains(readOr(t, filepath.Join(dir, "selected.md")), selected.Body) {
		t.Fatal("selected correction was not delivered")
	}
	if readOr(t, filepath.Join(dir, "unselected.md")) != beforeUnselected || readOr(t, filepath.Join(dir, "stale.md")) != beforeStale {
		t.Fatal("unselected or stale render changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "new-unselected.md")); !os.IsNotExist(err) {
		t.Fatal("unselected missing render was created")
	}
	for _, line := range strings.Split(strings.TrimSpace(beforeIndex), "\n") {
		if !strings.Contains(readOr(t, index), line+"\n") {
			t.Errorf("existing index line changed: %s", line)
		}
	}
	if actions, err := tg.Plan(); err != nil || len(actions) != 0 {
		t.Fatalf("second plan = %v, %v", actions, err)
	}
}

func TestSelectedCodexDeliveryPreservesUnselectedState(t *testing.T) {
	dir := t.TempDir()
	if _, err := codexTarget(dir, mem("selected"), mem("unselected"), mem("stale")).Apply(); err != nil {
		t.Fatal(err)
	}
	owned, err := scanCodexNotes(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	selected, unselected := mem("selected"), mem("unselected")
	selected.Body, unselected.Body = "corrected selected\n", "must not render\n"
	tg := codexTarget(dir, selected, unselected, mem("new-selected"), mem("new-unselected"))
	tg.Names = map[string]bool{"selected": true, "new-selected": true, "stale": true}
	res, err := tg.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || kinds(res.Applied)[Update] != 1 || kinds(res.Applied)[Create] != 1 {
		t.Fatalf("selected apply = %+v", res)
	}
	if !strings.Contains(readOr(t, owned["selected"].path), selected.Body) {
		t.Fatal("selected correction was not delivered")
	}
	for _, name := range []string{"unselected", "stale"} {
		if got := readOr(t, owned[name].path); got != string(owned[name].content) {
			t.Errorf("%s changed", name)
		}
	}
	if codexNoteExistsFor(t, dir, "new-unselected") {
		t.Fatal("unselected missing render was created")
	}
	if actions, err := tg.Plan(); err != nil || len(actions) != 0 {
		t.Fatalf("second plan = %v, %v", actions, err)
	}
}

func TestSelectedTargetsWithoutAdmittedMemoryWriteNothing(t *testing.T) {
	for _, harness := range []string{"claude", "shared", "codex"} {
		t.Run(harness, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "absent")
			names := map[string]bool{"not-in-desired": true}
			var tg Target
			switch harness {
			case "claude":
				tg = ClaudeTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{mem("unselected")}, Names: names}
			case "shared":
				tg = ClaudeSharedTarget{Dir: dir, RulesFile: filepath.Join(dir, "rules.md"), Desired: []*schema.CanonicalMemory{mem("unselected")}, Names: names}
			case "codex":
				tg = CodexTarget{ExtensionDir: dir, Desired: []*schema.CanonicalMemory{mem("unselected")}, Names: names}
			}
			if actions, err := tg.Plan(); err != nil || len(actions) != 0 {
				t.Fatalf("plan = %v, %v", actions, err)
			}
			if res, err := tg.Apply(); err != nil || len(res.Applied)+len(res.Conflicts) != 0 {
				t.Fatalf("apply = %+v, %v", res, err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("empty selected target created a directory or lock")
			}
		})
	}
}

func TestSelectedSharedWiringConflictBlocksDependentWrites(t *testing.T) {
	tg := sharedTarget(t, mem("selected"), mem("unselected"))
	tg.Names = map[string]bool{"selected": true}
	writeAt(t, tg.RulesFile, "foreign rules\n")
	plan, err := tg.Plan()
	if err != nil || len(plan) != 1 || plan[0].Kind != Conflict || plan[0].Name != RulesActionName {
		t.Fatalf("plan = %v, %v", plan, err)
	}
	res, err := tg.Apply()
	if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if got := readOr(t, tg.RulesFile); got != "foreign rules\n" {
		t.Fatal("foreign rules changed")
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "selected.md")); !os.IsNotExist(err) {
		t.Fatal("selected memory written behind foreign wiring")
	}
}

func TestSelectedSharedCreatesAndRepairsOwnedWiring(t *testing.T) {
	tg := sharedTarget(t, mem("selected"), mem("unselected"))
	tg.Names = map[string]bool{"selected": true}
	for _, existing := range []string{"", marker.SharedRulesMarker + "\nwrong import\n"} {
		if existing != "" {
			writeAt(t, tg.RulesFile, existing)
		}
		if _, err := tg.Apply(); err != nil {
			t.Fatal(err)
		}
		if got := readOr(t, tg.RulesFile); got != string(SharedRulesContent(tg.Dir)) {
			t.Fatal("shared wiring not repaired")
		}
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "unselected.md")); !os.IsNotExist(err) {
		t.Fatal("unselected shared memory created")
	}
}

func TestSelectedCodexWiringConflictBlocksDependentWrites(t *testing.T) {
	dir := t.TempDir()
	tg := codexTarget(dir, mem("selected"))
	tg.Names = map[string]bool{"selected": true}
	writeAt(t, tg.instructionsPath(), "foreign instructions\n")
	plan, err := tg.Plan()
	if err != nil || len(plan) != 1 || plan[0].Kind != Conflict || plan[0].Name != instructionsFile {
		t.Fatalf("plan = %v, %v", plan, err)
	}
	res, err := tg.Apply()
	if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if readOr(t, tg.instructionsPath()) != "foreign instructions\n" || codexNoteExistsFor(t, dir, "selected") {
		t.Fatal("foreign wiring or dependent note changed")
	}
}

func TestSelectedCodexDuplicateNotesConflict(t *testing.T) {
	dir := t.TempDir()
	tg := codexTarget(dir, mem("selected"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	owned, err := scanCodexNotes(tg.notesDir())
	if err != nil {
		t.Fatal(err)
	}
	duplicate := filepath.Join(tg.notesDir(), "duplicate.md")
	writeAt(t, duplicate, string(owned["selected"].content))
	tg.Desired[0].Body = "corrected\n"
	tg.Names = map[string]bool{"selected": true}
	plan, err := tg.Plan()
	if err != nil || len(plan) != 1 || plan[0].Kind != Conflict || plan[0].Name != "selected" {
		t.Fatalf("plan = %v, %v", plan, err)
	}
	res, err := tg.Apply()
	if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if readOr(t, duplicate) != string(owned["selected"].content) || readOr(t, owned["selected"].path) != string(owned["selected"].content) {
		t.Fatal("ambiguous selected notes changed")
	}
}

func TestSelectedCodexCreateDoesNotOverwriteOccupiedFilename(t *testing.T) {
	for _, content := range []string{"foreign note\n", marker.CodexNoteMarker("another", "global") + "\nowned other note\n"} {
		t.Run(content[:7], func(t *testing.T) {
			dir := t.TempDir()
			tg := codexTarget(dir, mem("selected"))
			tg.Names = map[string]bool{"selected": true}
			writeAt(t, tg.instructionsPath(), render.CodexInstructions)
			collision := filepath.Join(tg.notesDir(), "2026-01-02T03-04-05-selected.md")
			writeAt(t, collision, content)
			res, err := tg.Apply()
			if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 || res.Conflicts[0].Name != "selected" {
				t.Fatalf("apply = %+v, %v", res, err)
			}
			if readOr(t, collision) != content {
				t.Fatal("occupied note filename overwritten")
			}
		})
	}
}

func TestSelectedCodexCreatesMissingWiring(t *testing.T) {
	dir := t.TempDir()
	tg := codexTarget(dir, mem("selected"), mem("unselected"))
	tg.Names = map[string]bool{"selected": true}
	res, err := tg.Apply()
	if err != nil || kinds(res.Applied)[Create] != 2 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if got := readOr(t, tg.instructionsPath()); got != render.CodexInstructions {
		t.Fatal("selected create omitted companion instructions")
	}
	if !codexNoteExistsFor(t, dir, "selected") || codexNoteExistsFor(t, dir, "unselected") {
		t.Fatal("selected note creation did not honor selection")
	}
}

func TestSelectedClaudePreservesForeignMemory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "selected.md")
	writeAt(t, file, "foreign memory\n")
	tg := target(dir, mem("selected"))
	tg.Names = map[string]bool{"selected": true}
	res, err := tg.Apply()
	if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if readOr(t, file) != "foreign memory\n" {
		t.Fatal("foreign memory overwritten")
	}
}

func TestSelectedSharedPreservesHeldEdits(t *testing.T) {
	tg := sharedTarget(t, mem("selected"), mem("unselected"))
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	selectedPath := filepath.Join(tg.Dir, "selected.md")
	unselectedPath := filepath.Join(tg.Dir, "unselected.md")
	editBody(t, selectedPath, "selected in-place edit\n")
	editBody(t, unselectedPath, "unselected in-place edit\n")
	beforeSelected, beforeUnselected := readOr(t, selectedPath), readOr(t, unselectedPath)
	tg.Names = map[string]bool{"selected": true}
	res, err := tg.Apply()
	if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 || res.Conflicts[0].Name != "selected" {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if readOr(t, selectedPath) != beforeSelected || readOr(t, unselectedPath) != beforeUnselected {
		t.Fatal("in-place edit changed")
	}
}

func TestSelectedDeliveryHonorsTargetLock(t *testing.T) {
	for _, harness := range []string{"claude", "shared", "codex"} {
		t.Run(harness, func(t *testing.T) {
			dir := t.TempDir()
			names := map[string]bool{"selected": true}
			var tg Target
			switch harness {
			case "claude":
				tg = ClaudeTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{mem("selected")}, Names: names}
			case "shared":
				tg = ClaudeSharedTarget{Dir: dir, RulesFile: filepath.Join(dir, "rules.md"), Desired: []*schema.CanonicalMemory{mem("selected")}, Names: names}
			case "codex":
				tg = CodexTarget{ExtensionDir: dir, Desired: []*schema.CanonicalMemory{mem("selected")}, Names: names}
			}
			release, err := lock.Acquire(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if res, err := tg.Apply(); !errors.Is(err, lock.ErrLocked) || len(res.Applied) != 0 {
				t.Fatalf("apply under held lock = %+v, %v", res, err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 1 || files[0].Name() != lock.Name {
				t.Fatalf("held lock permitted writes: %v, %v", files, err)
			}
		})
	}
}

func TestSelectedWiringPreservesDanglingSymlinks(t *testing.T) {
	for _, harness := range []string{"shared", "codex"} {
		t.Run(harness, func(t *testing.T) {
			dir := t.TempDir()
			names := map[string]bool{"selected": true}
			var tg Target
			var wiring string
			switch harness {
			case "shared":
				wiring = filepath.Join(dir, "rules.md")
				tg = ClaudeSharedTarget{Dir: filepath.Join(dir, "memory"), RulesFile: wiring, Desired: []*schema.CanonicalMemory{mem("selected")}, Names: names}
			case "codex":
				wiring = filepath.Join(dir, instructionsFile)
				tg = CodexTarget{ExtensionDir: dir, Desired: []*schema.CanonicalMemory{mem("selected")}, Names: names}
			}
			if err := os.Symlink("missing", wiring); err != nil {
				t.Fatal(err)
			}
			res, err := tg.Apply()
			if err != nil || len(res.Applied) != 0 || len(res.Conflicts) != 1 {
				t.Fatalf("apply = %+v, %v", res, err)
			}
			if dest, err := os.Readlink(wiring); err != nil || dest != "missing" {
				t.Fatalf("foreign wiring symlink replaced: %q, %v", dest, err)
			}
		})
	}
}

package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/lock"
)

type selectedSyncFixture struct {
	root, canon, claude, codex, memory string
	args                               []string
}

func newSelectedSyncFixture(t *testing.T, mode string) selectedSyncFixture {
	t.Helper()
	f := selectedSyncFixture{root: t.TempDir()}
	f.canon = filepath.Join(f.root, "canonical")
	f.claude = filepath.Join(f.root, "claude")
	f.codex = filepath.Join(f.root, "codex")
	f.memory = filepath.Join(f.claude, "engram", "memory")
	scope := "global"
	if mode == "project" {
		scope = "project:x"
	}
	if mode != "shared" {
		f.memory = filepath.Join(f.claude, "projects", "-work-x", "memory")
	}
	for _, name := range []string{"selected", "unselected", "retired"} {
		writeFile(t, filepath.Join(f.canon, name+".md"), selectedSyncMemory(name, scope, "old"))
	}
	cfg := filepath.Join(f.root, "config.yaml")
	text := "canonical_root: " + f.canon + "\nharnesses:\n  claude-code:\n    home: " + f.claude + "\n"
	if mode == "unshared" {
		text += "    shared_index: false\n"
	}
	text += "  codex:\n    home: " + f.codex + "\n"
	writeFile(t, cfg, text)
	f.args = []string{"--config", cfg, "--cwd", "/work/x", "--host", "test-host"}
	if code, resp := runEnvelope(t, append([]string{"sync", "--apply"}, f.args...)...); code != exitOK {
		t.Fatalf("seed sync = %d: %v", code, resp)
	}
	for _, name := range []string{"selected", "unselected", "pending"} {
		writeFile(t, filepath.Join(f.canon, name+".md"), selectedSyncMemory(name, scope, "corrected"))
	}
	if err := os.Remove(filepath.Join(f.canon, "retired.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.memory, "native-only.md"), "---\nname: native-only\ndescription: native content\nmetadata:\n  type: lesson\n---\nNative Claude lesson.\n")
	index := filepath.Join(f.memory, "MEMORY.md")
	contents, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, index, string(contents)+"Native index line, retained verbatim.\n")
	writeFile(t, filepath.Join(f.codex, "memories", "MEMORY.md"), "# Task Group: Native Control\n\n- Native Codex lesson.\n")
	writeFile(t, filepath.Join(f.codex, "memories", "memory_summary.md"), "Native summary.\n")
	writeFile(t, filepath.Join(f.codex, "memories", "extensions", "engram", "notes", "native-only.md"), "Native extension content.\n")
	return f
}

func selectedSyncMemory(name, scope, revision string) string {
	return "---\nname: " + name + "\ndescription: " + name + " " + revision + "\ntype: lesson\nscope: " + scope + "\nprovenance:\n  origin: fixture-author\n  source: " + name + "-source.md\n  import_source: fixture/" + name + "-source.md\n  import_hash: sha256:fixture\n---\n" + name + " " + revision + " body\n"
}

func (f selectedSyncFixture) run(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	return runEnvelope(t, append(append([]string{"sync"}, args...), f.args...)...)
}

func TestSelectedSyncLeavesUnselectedOutdatedRenderUntouched(t *testing.T) {
	f := newSelectedSyncFixture(t, "shared")
	before := inventorySnapshot(t, f.root)
	code, resp := f.run(t, "--name", "selected", "--harness", "codex", "--apply")
	if code != exitOK {
		t.Fatalf("selected sync = %d: %v", code, resp)
	}
	after := inventorySnapshot(t, f.root)
	changedSelected := false
	for path, old := range before {
		if strings.HasPrefix(path, filepath.Join(f.codex, "memories", "extensions", "engram", "notes")+string(filepath.Separator)) && strings.HasSuffix(path, "-selected.md") {
			changedSelected = after[path] != old && strings.Contains(after[path], "selected corrected body")
			continue
		}
		if after[path] != old {
			t.Errorf("unselected file changed or disappeared: %s", path)
		}
	}
	if !changedSelected {
		t.Error("selected owned note did not receive its canonical correction")
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("unselected file created: %s", path)
		}
	}
}

func TestSelectedSyncClaudePreservesOtherIndexEntries(t *testing.T) {
	for _, mode := range []string{"shared", "unshared", "project"} {
		t.Run(mode, func(t *testing.T) {
			f := newSelectedSyncFixture(t, mode)
			before := inventorySnapshot(t, f.root)
			code, resp := f.run(t, "--name", "selected", "--harness", "claude-code", "--apply")
			if code != exitOK {
				t.Fatalf("selected sync = %d: %v", code, resp)
			}
			after := inventorySnapshot(t, f.root)
			selected := filepath.Join(f.memory, "selected.md")
			index := filepath.Join(f.memory, "MEMORY.md")
			if after[selected] == before[selected] || !strings.Contains(after[selected], "selected corrected body") {
				t.Error("selected Claude render did not receive its correction")
			}
			if !strings.Contains(after[index], "selected corrected") {
				t.Error("selected index entry did not receive its corrected description")
			}
			// Existing index order, native prose, and every other owned entry survive.
			wantIndex := strings.Replace(before[index], "— selected old ", "— selected corrected ", 1)
			if after[index] != wantIndex {
				t.Errorf("unselected index content changed:\ngot: %s\nwant: %s", after[index], wantIndex)
			}
			for path, old := range before {
				if path != selected && path != index && after[path] != old {
					t.Errorf("unselected file changed or disappeared: %s", path)
				}
			}
			for path := range after {
				if _, ok := before[path]; !ok {
					t.Errorf("unselected file created: %s", path)
				}
			}
		})
	}
}

func TestSelectedSyncCreateDeduplicatesAndIsIdempotent(t *testing.T) {
	for _, harness := range []string{"claude-code", "codex"} {
		t.Run(harness, func(t *testing.T) {
			f := newSelectedSyncFixture(t, "shared")
			before := inventorySnapshot(t, f.root)
			args := []string{"--name", "pending", "--name", "pending", "--harness", harness, "--apply"}
			code, resp := f.run(t, args...)
			if code != exitOK {
				t.Fatalf("create = %d: %v", code, resp)
			}
			data, ok := resp["data"].(map[string]any)
			if !ok || data["harness"] != harness || !reflect.DeepEqual(data["names"], []any{"pending"}) {
				t.Fatalf("selection missing or not deduplicated: %v", resp)
			}
			after := inventorySnapshot(t, f.root)
			created := 0
			for path, content := range after {
				if _, ok := before[path]; ok {
					if harness == "claude-code" && path == filepath.Join(f.memory, "MEMORY.md") {
						if !strings.Contains(content, "[pending](pending.md)") {
							t.Error("created Claude memory missing from index")
						}
						continue
					}
					if content != before[path] {
						t.Errorf("existing unselected file changed: %s", path)
					}
					continue
				}
				if !strings.Contains(content, "pending corrected body") || (harness == "codex" && !strings.HasPrefix(path, f.codex+string(filepath.Separator))) || (harness == "claude-code" && path != filepath.Join(f.memory, "pending.md")) {
					t.Errorf("unexpected created file: %s", path)
				}
				created++
			}
			if created != 1 {
				t.Errorf("created %d files, want exactly one selected render", created)
			}
			for path := range before {
				if _, ok := after[path]; !ok {
					t.Errorf("unselected file deleted: %s", path)
				}
			}
			code, resp = f.run(t, args...)
			if code != exitOK || !reflect.DeepEqual(after, inventorySnapshot(t, f.root)) {
				t.Fatalf("second selected apply is not idempotent: %d %v", code, resp)
			}
		})
	}
}

func TestSelectedSyncDryRunDoesNotWrite(t *testing.T) {
	for _, harness := range []string{"claude-code", "codex"} {
		t.Run(harness, func(t *testing.T) {
			f := newSelectedSyncFixture(t, "shared")
			before := inventorySnapshot(t, f.root)
			code, resp := f.run(t, "--name", "selected", "--name", "pending", "--harness", harness)
			if code != exitOK {
				t.Fatalf("preview = %d: %v", code, resp)
			}
			if !reflect.DeepEqual(before, inventorySnapshot(t, f.root)) {
				t.Fatal("selected preview changed files")
			}
			data := resp["data"].(map[string]any)
			if data["apply"] != false || data["harness"] != harness {
				t.Errorf("preview context missing: %v", data)
			}
			seen := map[string]string{}
			for _, raw := range data["harnesses"].([]any) {
				h := raw.(map[string]any)
				for _, action := range h["actions"].([]any) {
					a := action.(map[string]any)
					seen[a["name"].(string)] = a["kind"].(string)
				}
			}
			if !reflect.DeepEqual(seen, map[string]string{"selected": "UPDATE", "pending": "CREATE"}) {
				t.Errorf("preview did not isolate selected create/update actions: %v", seen)
			}
		})
	}
}

func TestSelectedSyncValidatesEntireBatchBeforeWriting(t *testing.T) {
	for _, kind := range []string{"unknown", "malformed", "duplicate", "project", "cwd", "agent", "host", "disabled", "forgotten", "missing-root"} {
		t.Run(kind, func(t *testing.T) {
			f := newSelectedSyncFixture(t, "shared")
			name, want := "blocked", "unknown_memory"
			path := filepath.Join(f.canon, name+".md")
			switch kind {
			case "malformed":
				writeFile(t, path, "---\nname: [malformed\n---\nbody\n")
				want = "withheld_memory"
			case "duplicate":
				body := selectedSyncMemory(name, "global", "old")
				writeFile(t, path, body)
				writeFile(t, filepath.Join(f.canon, "another.md"), body)
				want = "withheld_memory"
			case "project", "cwd", "agent", "host":
				body := selectedSyncMemory(name, "global", "old")
				switch kind {
				case "project":
					body = strings.Replace(body, "scope: global", "scope: project:elsewhere", 1)
				case "cwd":
					body = strings.Replace(body, "provenance:", "applies_to:\n  cwd: [/elsewhere]\nprovenance:", 1)
				case "agent":
					body = strings.Replace(body, "provenance:", "applies_to:\n  agents: [claude]\nprovenance:", 1)
				case "host":
					body = strings.Replace(body, "provenance:", "applies_to:\n  hosts: [elsewhere]\nprovenance:", 1)
				}
				writeFile(t, path, body)
				want = "out_of_scope"
			case "disabled":
				name, want = "selected", "harness_disabled"
				cfg, err := os.ReadFile(f.args[1])
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, f.args[1], strings.Replace(string(cfg), "  codex:\n", "  codex:\n    disabled: true\n", 1))
			case "forgotten":
				name = "retired"
				writeFile(t, filepath.Join(f.canon, "retired.md"), selectedSyncMemory(name, "global", "old"))
				if code, resp := runEnvelope(t, append([]string{"forget", name, "--apply"}, f.args...)...); code != exitOK {
					t.Fatalf("fixture forget = %d: %v", code, resp)
				}
			case "missing-root":
				if err := os.RemoveAll(f.canon); err != nil {
					t.Fatal(err)
				}
			}
			before := inventorySnapshot(t, f.root)
			code, resp := f.run(t, "--name", "selected", "--name", name, "--harness", "codex", "--apply")
			if code != exitUsage || errCode(resp) != want {
				t.Errorf("invalid batch = %d %v, want exit %d %s", code, resp, exitUsage, want)
			}
			if !reflect.DeepEqual(before, inventorySnapshot(t, f.root)) {
				t.Error("refused selection changed files or partially applied a valid name")
			}
		})
	}
}

func TestSelectedSyncRejectsIncompleteOrRepeatedSelectors(t *testing.T) {
	for _, args := range [][]string{
		{"--name", "selected"},
		{"--harness", "codex"},
		{"--name", "selected", "--harness", "unknown"},
		{"--name", "selected", "--harness", "codex", "--harness", "codex"},
		{"--name", "selected", "--harness", "codex", "--harness", "claude-code"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newSelectedSyncFixture(t, "shared")
			before := inventorySnapshot(t, f.root)
			code, resp := f.run(t, append(args, "--apply")...)
			if code != exitUsage || errCode(resp) != "usage" {
				t.Errorf("invalid arguments = %d %v, want usage", code, resp)
			}
			if !reflect.DeepEqual(before, inventorySnapshot(t, f.root)) {
				t.Error("invalid arguments changed files")
			}
		})
	}
}

func TestSelectedSyncRespectsCanonicalWriterLock(t *testing.T) {
	f := newSelectedSyncFixture(t, "shared")
	release, err := lock.Acquire(f.canon)
	if err != nil {
		t.Fatal(err)
	}
	before := inventorySnapshot(t, f.root)
	code, resp := f.run(t, "--name", "selected", "--harness", "codex", "--apply")
	release()
	if code != exitError || errCode(resp) != "locked" {
		t.Errorf("apply under canonical writer = %d %v, want locked", code, resp)
	}
	if !reflect.DeepEqual(before, inventorySnapshot(t, f.root)) {
		t.Error("selected apply wrote while canonical writer held the lock")
	}
	if code, resp = f.run(t, "--name", "selected", "--harness", "codex", "--apply"); code != exitOK {
		t.Errorf("apply after writer release = %d: %v", code, resp)
	}
}

func TestSelectedSyncOrdinarySyncStillProcessesWholeView(t *testing.T) {
	f := newSelectedSyncFixture(t, "shared")
	code, resp := f.run(t, "--apply")
	if code != exitOK {
		t.Fatalf("ordinary sync = %d: %v", code, resp)
	}
	for _, name := range []string{"selected", "unselected", "pending"} {
		data, err := os.ReadFile(filepath.Join(f.memory, name+".md"))
		if err != nil || !strings.Contains(string(data), name+" corrected body") {
			t.Errorf("ordinary sync did not render %s: %v", name, err)
		}
	}
	if exists(filepath.Join(f.memory, "retired.md")) {
		t.Error("ordinary sync no longer removes stale owned renders")
	}
}

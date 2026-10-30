package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/slug"
)

func TestStatusInventoryJoinsNativeAndCanonical(t *testing.T) {
	_, _, args := orphanFixture(t)
	code, env := runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
	if code != exitOK {
		t.Fatalf("status: %v", env)
	}
	data := env["data"].(map[string]any)
	if got := data["canonical"].(map[string]any)["count"]; got != float64(5) {
		t.Fatalf("canonical count = %v, want five actual fixture memories", got)
	}
}

func statusHarness(t *testing.T, env map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range env["data"].(map[string]any)["harnesses"].([]any) {
		h := row.(map[string]any)
		if h["harness"] == name {
			return h
		}
	}
	t.Fatalf("missing harness %s", name)
	return nil
}

func assertInventoryCounts(t *testing.T, h map[string]any, want map[string]float64) {
	t.Helper()
	counts := h["counts"].(map[string]any)
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("%s %s=%v, want %v", h["harness"], k, counts[k], v)
		}
	}
}

func TestStatusNormalizesNamesAndCountsOwnedFilesOnce(t *testing.T) {
	canon, codex, args := orphanFixture(t)
	base := filepath.Dir(canon)
	claude := filepath.Join(base, "claude")
	project := filepath.Join(claude, "projects", "-work-x", "memory")
	writeFile(t, filepath.Join(project, "live.md"), "---\nname: Live Lesson\ndescription: d\n---\nprivate-memory-body\n")
	rendered := "---\nname: live-lesson\nmetadata:\n  origin: engram-sync\n---\nrender\n"
	writeFile(t, filepath.Join(project, "render.md"), rendered)
	writeFile(t, filepath.Join(claude, "projects", "-work-y", "memory", "render.md"), rendered)
	writeFile(t, filepath.Join(claude, "engram", "memory", "kept-one.md"), strings.ReplaceAll(rendered, "live-lesson", "kept-one"))
	writeFile(t, filepath.Join(project, "MEMORY.md"), "ignored index\n")
	notes := filepath.Join(codex, "memories", "extensions", "engram", "notes")
	writeFile(t, filepath.Join(notes, "new.md"), marker.CodexNoteMarker("new-group", "global")+"\nprivate-memory-body\n")
	writeFile(t, filepath.Join(notes, "old.md"), marker.CodexNoteMarker("old-group", "global")+"\nrender\n")
	writeFile(t, filepath.Join(notes, "unowned.md"), "unowned file\n")
	before := inventorySnapshot(t, base)
	for _, all := range []bool{false, true} {
		argv := []string{"status", "--apply"}
		if all {
			argv = append(argv, "--all-projects")
		}
		code, env := runEnvelope(t, append(argv, args...)...)
		if code != exitOK {
			t.Fatalf("status: %v", env)
		}
		h := statusHarness(t, env, "claude-code")
		renders := float64(2)
		if all {
			renders = 3
		}
		assertInventoryCounts(t, h, map[string]float64{"native_entries": 1, "rendered_entries": renders, "native_names": 1, "rendered_names": 2, "overlapping_names": 1, "distinct_names": 2})
		assertInventoryCounts(t, statusHarness(t, env, "codex"), map[string]float64{"native_entries": 3, "rendered_entries": 2, "native_names": 3, "rendered_names": 2, "overlapping_names": 1, "distinct_names": 4})
		blob, _ := json.Marshal(env)
		if strings.Contains(string(blob), "private-memory-body") {
			t.Error("inventory must not dump memory bodies")
		}
	}
	if !reflect.DeepEqual(before, inventorySnapshot(t, base)) {
		t.Error("status --apply changed fixture files")
	}
}

func inventorySnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[path] = string(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestStatusFailuresArePartialNotCleanZeros(t *testing.T) {
	for _, kind := range []string{"claude_parse", "codex_unrecognized", "codex_read", "canonical_parse", "projects_read", "notes_read"} {
		t.Run(kind, func(t *testing.T) {
			canon, codex, args := orphanFixture(t)
			claude := filepath.Join(filepath.Dir(canon), "claude")
			switch kind {
			case "claude_parse":
				writeFile(t, filepath.Join(claude, "projects", "-work-x", "memory", "broken.md"), "---\nname: broken\n")
			case "codex_unrecognized":
				writeFile(t, filepath.Join(codex, "memories", "MEMORY.md"), "changed native format, cannot parse\n")
			case "codex_read":
				if err := os.Remove(filepath.Join(codex, "memories", "MEMORY.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(codex, "memories", "MEMORY.md"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "canonical_parse":
				writeFile(t, filepath.Join(canon, "bad.md"), "not canonical\n")
			case "projects_read":
				if err := os.RemoveAll(filepath.Join(claude, "projects")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(claude, "projects"), "not a directory")
			case "notes_read":
				writeFile(t, filepath.Join(codex, "memories", "extensions", "engram", "notes"), "not a directory")
			}
			code, env := runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
			if code != exitError || env["data"].(map[string]any)["complete"] != false {
				t.Fatalf("failure reported as complete: %v", env)
			}
			if env["error"].(map[string]any)["code"] != "inventory_incomplete" {
				t.Errorf("unexpected error: %v", env)
			}
			if kind != "canonical_parse" && env["data"].(map[string]any)["canonical"].(map[string]any)["count"] != float64(5) {
				t.Error("valid canonical evidence lost on harness failure")
			}
		})
	}
}

func TestStatusMissingStoresAreExplicitAndNeverCreated(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "c.yaml")
	canon := filepath.Join(dir, "missing-canonical")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  codex:\n    home: "+filepath.Join(dir, "missing-codex")+"\n  claude-code:\n    home: "+filepath.Join(dir, "missing-claude")+"\n")
	before := inventorySnapshot(t, dir)
	code, env := runEnvelope(t, "status", "--all-projects", "--apply", "--config", cfg, "--cwd", "/work/x")
	if code != exitOK {
		t.Fatalf("explicit missing stores are not read errors: %v", env)
	}
	c := env["data"].(map[string]any)["canonical"].(map[string]any)
	if c["state"] != "missing" || c["complete"] != false {
		t.Errorf("missing canonical appears empty: %v", c)
	}
	for _, name := range []string{"codex", "claude-code"} {
		h := statusHarness(t, env, name)
		assertInventoryCounts(t, h, map[string]float64{"native_entries": 0, "rendered_entries": 0, "distinct_names": 0})
		for _, loc := range h["locations"].([]any) {
			if loc.(map[string]any)["state"] != "missing" {
				t.Errorf("missing location: %v", loc)
			}
		}
	}
	if !reflect.DeepEqual(before, inventorySnapshot(t, dir)) {
		t.Error("inventory created missing stores or locks")
	}
}

func TestStatusExplainsAbsenceWithoutClaimingConsumption(t *testing.T) {
	canon, _, args := orphanFixture(t)
	writeFile(t, filepath.Join(canon, "other.md"), "---\nname: other\ndescription: d\ntype: lesson\nscope: project:other\n---\nbody\n")
	writeFile(t, filepath.Join(canon, "host.md"), "---\nname: host\ndescription: d\ntype: lesson\nscope: global\napplies_to:\n  hosts: [only-host]\n---\nbody\n")
	code, env := runEnvelope(t, append([]string{"status"}, args...)...)
	if code != exitOK {
		t.Fatal(env)
	}
	codex := statusHarness(t, env, "codex")
	reasons := map[string]string{}
	for _, a := range codex["canonical_absent"].([]any) {
		r := a.(map[string]any)
		reasons[r["name"].(string)] = r["reason"].(string)
	}
	for name, want := range map[string]string{"old-group": "source_suppressed", "other": "scope_excluded", "host": "scope_excluded", "kept-one": "missing_render"} {
		if reasons[name] != want {
			t.Errorf("%s reason %q, want %q", name, reasons[name], want)
		}
	}
	_, env = runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
	codex = statusHarness(t, env, "codex")
	for _, a := range codex["canonical_absent"].([]any) {
		r := a.(map[string]any)
		if r["name"] == "other" && r["reason"] != "unknown_scope" {
			t.Errorf("unresolved slug must not prove scope exclusion: %v", r)
		}
	}
}

func TestStatusDisabledHarnessStillShowsObservedFiles(t *testing.T) {
	canon, codex, _ := orphanFixture(t)
	cfg := filepath.Join(filepath.Dir(canon), "disabled.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  codex:\n    home: "+codex+"\n    disabled: true\n")
	code, env := runEnvelope(t, "status", "--config", cfg, "--cwd", "/work/x")
	if code != exitOK {
		t.Fatal(env)
	}
	h := statusHarness(t, env, "codex")
	if h["enabled"] != false {
		t.Fatal(h)
	}
	assertInventoryCounts(t, h, map[string]float64{"native_entries": 3})
	for _, a := range h["canonical_absent"].([]any) {
		if a.(map[string]any)["reason"] != "harness_disabled" {
			t.Errorf("disabled reason: %v", a)
		}
	}
}

func TestStatusAllProjectsResolvesScopeAndPreservesDefaultBoundary(t *testing.T) {
	canon, _, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	project := filepath.Join(t.TempDir(), "another-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(canon, "another.md"), "---\nname: another\ndescription: d\ntype: lesson\nscope: project:another-project\n---\nbody\n")
	path := filepath.Join(claude, "projects", slug.ForCwd(project), "memory", "another.md")
	writeFile(t, path, "---\nname: another\ndescription: native\n---\nbody\n")
	_, env := runEnvelope(t, append([]string{"status"}, args...)...)
	assertInventoryCounts(t, statusHarness(t, env, "claude-code"), map[string]float64{"native_entries": 1})
	_, env = runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
	h := statusHarness(t, env, "claude-code")
	assertInventoryCounts(t, h, map[string]float64{"native_entries": 2})
	found := false
	for _, cwd := range h["scope_cwds"].([]any) {
		found = found || cwd == project
	}
	if !found {
		t.Errorf("resolved project omitted from scope contexts: %v", h)
	}
}

func TestStatusReportsNormalizationCollisionAndBadOwnedNote(t *testing.T) {
	for _, kind := range []string{"collision", "marker"} {
		t.Run(kind, func(t *testing.T) {
			canon, codex, args := orphanFixture(t)
			if kind == "collision" {
				project := filepath.Join(filepath.Dir(canon), "claude", "projects", "-work-x", "memory")
				writeFile(t, filepath.Join(project, "collision.md"), "---\nname: Live Lesson\ndescription: d\n---\ndifferent\n")
			} else {
				writeFile(t, filepath.Join(codex, "memories", "extensions", "engram", "notes", "bad.md"), "<!-- engram-sync extension=engram -->\n")
			}
			code, env := runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
			if code != exitError {
				t.Fatalf("ambiguous input reported as clean: %v", env)
			}
			h := statusHarness(t, env, "claude-code")
			if kind == "collision" {
				assertInventoryCounts(t, h, map[string]float64{"native_entries": 2, "native_names": 1})
			}
		})
	}
}

func TestStatusAllProjectsReportsCrossSlugCollisions(t *testing.T) {
	canon, _, args := orphanFixture(t)
	other := filepath.Join(filepath.Dir(canon), "claude", "projects", "-work-y", "memory", "alias.md")
	writeFile(t, other, "---\nname: Live Lesson\ndescription: different native source\n---\nbody\n")
	code, env := runEnvelope(t, append([]string{"status", "--all-projects"}, args...)...)
	if code != exitError {
		t.Fatalf("cross-slug normalization collision reported as complete: %v", env["data"])
	}
	assertInventoryCounts(t, statusHarness(t, env, "claude-code"), map[string]float64{"native_entries": 2, "native_names": 1, "distinct_names": 1})
}

func TestStatusPlainShowsValuesAndPartialEvidence(t *testing.T) {
	canon, codex, args := orphanFixture(t)
	for _, broken := range []bool{false, true} {
		if broken {
			writeFile(t, filepath.Join(codex, "memories", "MEMORY.md"), "unrecognized native format\n")
		}
		code := 0
		out := captureStdout(t, func() { code = Run(append([]string{"status", "--plain"}, args...)) })
		wantCode := exitOK
		if broken {
			wantCode = exitError
		}
		if code != wantCode {
			t.Errorf("plain exit %d, want %d", code, wantCode)
		}
		for _, want := range []string{"Canonical: count=5", "claude-code:", "codex:", "native_entries=", filepath.Join(filepath.Dir(canon), "claude", "projects", "-work-x", "memory"), "old-group", "source_suppressed"} {
			// With an unrecognized native source, absence must stay unknown.
			if broken && want == "source_suppressed" {
				want = "unknown_incomplete_scan"
			}
			if !strings.Contains(out, want) {
				t.Errorf("plain inventory omits %q:\n%s", want, out)
			}
		}
		if broken && !strings.Contains(out, "nonempty source has no recognized Task Group sections") {
			t.Errorf("plain partial evidence omits diagnostic:\n%s", out)
		}
		if strings.Contains(out, "[0x") {
			t.Errorf("plain inventory contains struct pointers:\n%s", out)
		}
	}
	out := captureStdout(t, func() { Run(append([]string{"status", "--plain", "--quiet"}, args...)) })
	if out != "" {
		t.Errorf("quiet printed inventory: %q", out)
	}
}

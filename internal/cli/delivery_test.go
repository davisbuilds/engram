package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/render"
	syncer "github.com/davisbuilds/engram/internal/sync"
)

func TestDeliverySeparatesStoredFilesFromConsumption(t *testing.T) {
	canon, codex, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	shared := filepath.Join(claude, "engram", "memory")
	writeFile(t, filepath.Join(claude, "projects", "-work-x", "memory", "MEMORY.md"), "project index\n")
	writeFile(t, filepath.Join(shared, "MEMORY.md"), "shared index\n")
	writeFile(t, filepath.Join(claude, "rules", "engram-memory.md"), string(syncer.SharedRulesContent(shared)))
	extension := filepath.Join(codex, "memories", "extensions", "engram")
	writeFile(t, filepath.Join(extension, "instructions.md"), render.CodexInstructions)
	writeFile(t, filepath.Join(extension, "notes", "native.txt"), "not owned\n")
	writeFile(t, filepath.Join(codex, "config.toml"), "memories = true\n")
	before := inventorySnapshot(t, filepath.Dir(canon))
	code, env := runEnvelope(t, append([]string{"status", "--apply"}, args...)...)
	if code != exitOK {
		t.Fatalf("status: %v", env)
	}
	for _, name := range []string{"claude-code", "codex"} {
		h := statusHarness(t, env, name)
		d, ok := h["delivery"].(map[string]any)
		if !ok {
			t.Fatalf("%s lacks delivery evidence: %v", name, h)
		}
		r := d["readiness"].(map[string]any)
		if r["ready"] != true || r["readiness_basis"] != "configuration_heuristic" {
			t.Errorf("readiness is not labeled as configuration only: %v", r)
		}
		consumption := d["consumption"].(map[string]any)
		if consumption["state"] != "unknown" || consumption["reason"] != "no_session_consumption_evidence" {
			t.Errorf("stored files/config must not imply consumption: %v", consumption)
		}
		for _, value := range d["load_paths"].([]any) {
			route := value.(map[string]any)
			if route["eligibility"] != "unknown" || route["reason"] != "runtime_loading_unverified" {
				t.Errorf("static wiring must not prove eligibility: %v", route)
			}
		}
	}
	if !reflect.DeepEqual(before, inventorySnapshot(t, filepath.Dir(canon))) {
		t.Error("delivery inspection changed files")
	}
}

func TestDeliveryMissingDifferentAndUnreadableWiring(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "different", "error"} {
		t.Run(kind, func(t *testing.T) {
			canon, _, args := orphanFixture(t)
			claude := filepath.Join(filepath.Dir(canon), "claude")
			shared := filepath.Join(claude, "engram", "memory")
			writeFile(t, filepath.Join(shared, "MEMORY.md"), "index\n")
			wiring := filepath.Join(claude, "rules", "engram-memory.md")
			switch kind {
			case "empty":
				writeFile(t, wiring, " \n")
			case "different":
				writeFile(t, wiring, "custom loader; do not expose its body\n")
			case "error":
				if err := os.MkdirAll(wiring, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			code, env := runEnvelope(t, append([]string{"status"}, args...)...)
			wantCode := exitOK
			if kind == "error" {
				wantCode = exitError
			}
			if code != wantCode {
				t.Fatalf("code = %d, want %d: %v", code, wantCode, env)
			}
			h := statusHarness(t, env, "claude-code")
			d, ok := h["delivery"].(map[string]any)
			if !ok {
				t.Fatalf("missing delivery evidence: %v", h)
			}
			found := false
			for _, value := range d["load_paths"].([]any) {
				route := value.(map[string]any)
				if route["kind"] != "claude_shared" {
					continue
				}
				found = true
				artifact := route["artifacts"].([]any)[1].(map[string]any)
				if artifact["state"] != kind {
					t.Errorf("wiring state = %v, want %s", artifact, kind)
				}
				wantEligibility := "unknown"
				if kind == "missing" || kind == "empty" {
					wantEligibility = "prerequisites_missing"
				}
				if route["eligibility"] != wantEligibility {
					t.Errorf("eligibility = %v, want %s", route, wantEligibility)
				}
			}
			if !found {
				t.Error("missing shared load path")
			}
			blob, _ := json.Marshal(env)
			if strings.Contains(string(blob), "custom loader; do not expose") {
				t.Error("inspection exposed wiring content")
			}
		})
	}
}

func TestDeliveryAllProjectsAndDisabledHarnessRemainObservable(t *testing.T) {
	canon, codex, args := orphanFixture(t)
	claude := filepath.Join(filepath.Dir(canon), "claude")
	writeFile(t, filepath.Join(claude, "projects", "-work-y", "memory", "MEMORY.md"), "another index\n")
	writeFile(t, filepath.Join(codex, "config.toml"), "memories = false\n")
	configPath := args[1]
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, strings.ReplaceAll(string(data), "    home:", "    disabled: true\n    home:"))
	for _, all := range []bool{false, true} {
		argv := []string{"status"}
		if all {
			argv = append(argv, "--all-projects")
		}
		code, env := runEnvelope(t, append(argv, args...)...)
		if code != exitOK {
			t.Fatalf("status: %v", env)
		}
		d := statusHarness(t, env, "claude-code")["delivery"].(map[string]any)
		want := 2 // current project plus shared
		if all {
			want++
		}
		if len(d["load_paths"].([]any)) != want {
			t.Errorf("load paths do not match scan boundary: %v", d)
		}
		if d["readiness"].(map[string]any)["engram_enabled"] != false {
			t.Fatal("fixture must disable the harness")
		}
		co := statusHarness(t, env, "codex")["delivery"].(map[string]any)
		if co["readiness"].(map[string]any)["native_memory"] != "off" || co["consumption"].(map[string]any)["state"] != "unknown" {
			t.Errorf("a setting scan must not imply session consumption or non-consumption: %v", co)
		}
	}
}

func TestDeliveryPlainOutputIncludesEvidence(t *testing.T) {
	_, _, args := orphanFixture(t)
	out := captureStdout(t, func() { Run(append([]string{"status", "--plain"}, args...)) })
	for _, text := range []string{"configuration_heuristic", "Consumption: unknown", "claude_project", "claude_shared", "codex_extension", "prerequisites_missing"} {
		if !strings.Contains(out, text) {
			t.Errorf("plain output lacks %q: %s", text, out)
		}
	}
}

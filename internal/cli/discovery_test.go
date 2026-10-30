package cli

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDiscoveryDescribesArgumentsAndCapabilities(t *testing.T) {
	for _, command := range []string{"help", "schema"} {
		code, resp := runEnvelope(t, command)
		if code != exitOK {
			t.Fatalf("%s: %v", command, resp)
		}
		d := respData(resp)
		if _, ok := d["capabilities"].(map[string]any); !ok {
			t.Fatalf("%s must describe supported capabilities", command)
		}
		rows, ok := d["commands"].([]any)
		if !ok || len(rows) != len(commands()) {
			t.Fatalf("%s must describe every dispatched command", command)
		}
		for _, row := range rows {
			r := row.(map[string]any)
			if _, ok := r["arguments"].(map[string]any); !ok {
				t.Errorf("%s lacks argument discovery", r["name"])
			}
		}
	}
}

func TestDiscoveryMatchesImportantFlagsAndBothSurfaces(t *testing.T) {
	_, help := runEnvelope(t, "help")
	_, schema := runEnvelope(t, "schema")
	for _, key := range []string{"commands", "global_flags", "capabilities"} {
		if !reflect.DeepEqual(respData(help)[key], respData(schema)[key]) {
			t.Errorf("help and schema disagree on %s", key)
		}
	}
	rows := respData(help)["commands"].([]any)
	expected := map[string]map[string][]string{
		"curate":   {"value_flags": {"--proposal", "--save-plan", "--plan", "--harness"}},
		"remember": {"value_flags": {"--from-json", "--name", "--applies-cwd", "--related"}, "boolean_flags": {"--force"}},
		"status":   {"boolean_flags": {"--all-projects"}},
		"diff":     {"value_flags": {"--native", "--name", "--source-name"}, "boolean_flags": {"--shared"}},
	}
	for _, row := range rows {
		r := row.(map[string]any)
		for key, flags := range expected[r["name"].(string)] {
			got := []string{}
			for _, value := range r["arguments"].(map[string]any)[key].([]any) {
				got = append(got, value.(string))
			}
			for _, f := range flags {
				if !slices.Contains(got, f) {
					t.Errorf("%s lacks %s in %s", r["name"], f, key)
				}
			}
		}
	}
	if respData(help)["capabilities"].(map[string]any)["curate_supplied_proposals"] != float64(1) {
		t.Error("missing supplied-proposal capability version")
	}
}

func TestDiscoveryGlobalFlagsWorkWithoutReadingConfigOrWriting(t *testing.T) {
	dir := t.TempDir()
	for _, f := range globalArgs.bools {
		code, _ := runEnvelope(t, "schema", f, "--config", filepath.Join(dir, "absent.yaml"))
		if code != exitOK {
			t.Errorf("advertised global boolean %s rejected", f)
		}
	}
	for _, f := range globalArgs.values {
		code, _ := runEnvelope(t, "schema", f, "synthetic", "--apply")
		if code != exitOK {
			t.Errorf("advertised global value flag %s rejected", f)
		}
	}
	if files := inventorySnapshot(t, dir); len(files) != 0 {
		t.Errorf("discovery created state: %v", files)
	}
}

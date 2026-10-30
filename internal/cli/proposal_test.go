package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/curate"
)

func TestSuppliedProposalSavesAndAppliesWithoutAgent(t *testing.T) {
	f := newPlanFixture(t, "a", "b")
	in := filepath.Join(f.dir, "proposal.json")
	writeFile(t, in, `{"operations":[{"op":"remove","name":"a","reason":"superseded"}]}`)
	before := storeTree(t, f.canon)
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--proposal", in, "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save: %d %v", code, resp)
	}
	sameStore(t, before, storeTree(t, f.canon))
	raw, err := os.ReadFile(f.plan)
	if err != nil {
		t.Fatal("validated proposal must save a bound plan:", err)
	}
	p, err := curate.ParsePlan(raw)
	if err != nil || len(p.Operations) != 1 || p.Operations[0].Name != "a" {
		t.Fatalf("saved proposal: %v %v", p, err)
	}
	if p.Proposer != nil {
		t.Error("a supplied proposal must not claim a model invocation")
	}
	code, resp = runCurate(t, curateEnv(f.cfg, true, noAgent(t)), "--plan", f.plan)
	if code != exitOK {
		t.Fatalf("apply: %d %v", code, resp)
	}
	if _, err := os.Stat(filepath.Join(f.canon, "a.md")); !os.IsNotExist(err) {
		t.Error("reviewed removal was not applied")
	}
}

func TestSuppliedProposalRejectsInvalidBatchWithoutWrites(t *testing.T) {
	f := newPlanFixture(t, "a")
	in := filepath.Join(f.dir, "proposal.json")
	writeFile(t, in, `{"operations":[{"op":"remove","name":"a"},{"op":"remove","name":"missing"}]}`)
	before := storeTree(t, f.canon)
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--proposal", in, "--save-plan", f.plan)
	if code != exitConflicts || errCode(resp) != "invalid_proposal" {
		t.Fatalf("invalid batch: %d %v", code, resp)
	}
	for _, value := range resp["next_steps"].([]any) {
		if !strings.Contains(value.(map[string]any)["command"].(string), "--proposal") {
			t.Error("supplied-proposal leads must retain the no-agent workflow")
		}
	}
	sameStore(t, before, storeTree(t, f.canon))
	if _, err := os.Stat(f.plan); !os.IsNotExist(err) {
		t.Error("invalid batch saved a plan")
	}
}

func TestSuppliedProposalStrictJSON(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"operations":null}`, `{"operations":[],"extra":1}`,
		`{"operations":[],"Operations":[]}`, `{"operations":[]} {}`,
		"```json\n{\"operations\":[]}\n```",
		`{"operations":[{"op":"remove","name":"a","NAME":"b"}]}`,
		`{"operations":[{"op":"remove","name":"a","unknown":true}]}`,
		`{"operations":[{"op":"update","name":"a","memory":{"name":"a","unknown":true}}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			f := newPlanFixture(t, "a")
			in := filepath.Join(f.dir, "proposal.json")
			writeFile(t, in, raw)
			before := inventorySnapshot(t, f.dir)
			code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--proposal", in)
			if code != exitUsage || errCode(resp) != "parse_proposal" {
				t.Fatalf("strict parse: %d %v", code, resp)
			}
			sameStore(t, before, inventorySnapshot(t, f.dir))
		})
	}
}

func TestSuppliedProposalStdinIsReadOnlyAndIdentified(t *testing.T) {
	f := newPlanFixture(t, "a")
	in := filepath.Join(f.dir, "stdin.json")
	writeFile(t, in, `{"operations":[{"op":"remove","name":"a"}]}`)
	file, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	old := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = old }()
	before := inventorySnapshot(t, f.dir)
	code, resp := runEnvelope(t, "curate", "--proposal", "-", "--config", f.cfg)
	if code != exitOK {
		t.Fatalf("stdin: %d %v", code, resp)
	}
	d := respData(resp)
	p := d["proposal"].(map[string]any)
	if p["path"] != "-" || p["sha256"] == "" || d["invocation"] != nil || len(d["operations"].([]any)) != 1 {
		t.Errorf("proposal receipt: %v", d)
	}
	sameStore(t, before, inventorySnapshot(t, f.dir))
}

func TestSuppliedProposalRejectsFlagsBeforeInput(t *testing.T) {
	for _, flags := range [][]string{
		{"--apply"},
		{"--plan", "plan.json"},
		{"--harness", "codex"},
		{"--model", "x"},
		{"--effort", "high"},
		{"--timeout", "1m"},
		{"--proposal", "second.json"},
		{"--save-plan", "a.json", "--save-plan", "b.json"},
	} {
		f := newPlanFixture(t, "a")
		// The missing file proves flag rejection precedes any input read.
		args := append([]string{"--proposal", filepath.Join(f.dir, "missing.json")}, flags...)
		e := curateEnv(f.cfg, false, noAgent(t))
		if flags[0] == "--apply" {
			// runCurate bypasses global parsing.
			e.apply = true
			args = args[:2]
		}
		code, resp := runCurate(t, e, args...)
		if code != exitUsage || errCode(resp) != "usage" {
			t.Fatalf("%v: %d %v", flags, code, resp)
		}
	}
}

func TestSuppliedProposalSavedPlanRejectsDriftUnderLock(t *testing.T) {
	f := newPlanFixture(t, "a")
	in := filepath.Join(f.dir, "proposal.json")
	writeFile(t, in, `{"operations":[{"op":"remove","name":"a"}]}`)
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--proposal", in, "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save: %d %v", code, resp)
	}
	e := curateEnv(f.cfg, true, noAgent(t))
	e.beforeApplyLock = func() { writeFile(t, filepath.Join(f.canon, "unrelated.txt"), "outside edit") }
	code, resp = runCurate(t, e, "--plan", f.plan)
	if code != exitConflicts || errCode(resp) != "stale_plan" {
		t.Fatalf("stale plan: %d %v", code, resp)
	}
	if _, err := os.Stat(filepath.Join(f.canon, "a.md")); err != nil {
		t.Error("stale plan removed a memory")
	}
}

func TestSuppliedProposalRefusesIncompleteCorpusAndLockedSave(t *testing.T) {
	for _, kind := range []string{"malformed", "locked", "read_error"} {
		t.Run(kind, func(t *testing.T) {
			f := newPlanFixture(t, "a")
			in := filepath.Join(f.dir, "proposal.json")
			writeFile(t, in, `{"operations":[]}`)
			argv := []string{"--proposal", in}
			want := "corpus_parse"
			switch kind {
			case "malformed":
				writeFile(t, filepath.Join(f.canon, "bad.md"), "broken\n")
			case "locked":
				release, rerr := canonLock(f.canon)
				if rerr != nil {
					t.Fatal(rerr)
				}
				defer release()
				argv = append(argv, "--save-plan", f.plan)
				want = "locked"
			case "read_error":
				argv[1] = filepath.Join(f.dir, "missing.json")
				want = "proposal_read"
			}
			code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), argv...)
			if code != exitError || errCode(resp) != want {
				t.Fatalf("%s: %d %v", kind, code, resp)
			}
			if _, err := os.Stat(f.plan); !os.IsNotExist(err) {
				t.Error("failed inspection saved a plan")
			}
		})
	}
}

func TestSuppliedProposalSavedMemoryUsesResolvedProvenance(t *testing.T) {
	f := newPlanFixture(t, "a")
	canonFile(t, f.canon, "a", "remember", "", "original\n")
	in := filepath.Join(f.dir, "proposal.json")
	writeFile(t, in, `{"operations":[{"op":"update","name":"a","memory":{"name":"a","description":"new","type":"lesson","scope":"global","body":"new","provenance":{"origin":"invented"}}}]}`)
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--proposal", in, "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save: %d %v", code, resp)
	}
	var p curate.Plan
	raw, err := os.ReadFile(f.plan)
	if err != nil || json.Unmarshal(raw, &p) != nil {
		t.Fatal("missing saved plan", err)
	}
	if p.Operations[0].Memory.Provenance.Origin != "remember" {
		t.Error("reviewed plan must show provenance apply preserves")
	}
}

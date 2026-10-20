package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/schema"
)

// runCurate runs cmdCurate and returns its exit code and its JSON envelope.
func runCurate(t *testing.T, e *env, args ...string) (int, map[string]any) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = cmdCurate(e, "curate", args) })
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out)
	}
	return code, resp
}

func errCode(resp map[string]any) string {
	e, _ := resp["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func respData(resp map[string]any) map[string]any {
	d, _ := resp["data"].(map[string]any)
	return d
}

// noAgent fails the test if engram invokes an agent: a saved plan is applied
// without a model call.
func noAgent(t *testing.T) agentexec.Runner {
	return func(agentexec.Invocation) ([]byte, error) {
		t.Error("an agent ran; a saved plan must be applied without a model call")
		return nil, errors.New("no agent")
	}
}

// storeTree returns every file under root but the apply lock, as path -> content,
// or for a symlink, path -> "-> target".
func storeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == lock.Name {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.Type()&fs.ModeSymlink != 0 {
			target, lerr := os.Readlink(p)
			out[filepath.ToSlash(rel)] = "-> " + target
			return lerr
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameStore(t *testing.T, before, after map[string]string) {
	t.Helper()
	for p, v := range before {
		if got, ok := after[p]; !ok {
			t.Errorf("%s was removed", p)
		} else if got != v {
			t.Errorf("%s changed:\n%s\nwant:\n%s", p, got, v)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("%s was created", p)
		}
	}
}

func fenced(ops ...string) string {
	return "```json\n{\"operations\":[" + strings.Join(ops, ",") + "]}\n```"
}

type planFixture struct{ dir, canon, cfg, plan string }

func newPlanFixture(t *testing.T, names ...string) planFixture {
	t.Helper()
	dir := t.TempDir()
	f := planFixture{
		dir: dir, canon: filepath.Join(dir, "canonical"),
		cfg: filepath.Join(dir, "c.yaml"), plan: filepath.Join(dir, "plan.json"),
	}
	if err := os.MkdirAll(f.canon, 0o755); err != nil {
		t.Fatal(err)
	}
	seedCanon(t, f.canon, names...)
	writeFile(t, f.cfg, "canonical_root: "+f.canon+"\n")
	return f
}

// save runs the dry run that writes the plan and fails the test unless it does.
func (f planFixture) save(t *testing.T, proposal string) map[string]any {
	t.Helper()
	code, resp := runCurate(t, curateEnv(f.cfg, false, fakeClaudeRunner(proposal)), "--save-plan", f.plan)
	if code != exitOK || !exists(f.plan) {
		t.Fatalf("save-plan exit = %d (plan written: %v), want %d: %v", code, exists(f.plan), exitOK, resp)
	}
	return resp
}

// apply applies the saved plan with an agent that must not run.
func (f planFixture) apply(t *testing.T) (int, map[string]any) {
	t.Helper()
	return runCurate(t, curateEnv(f.cfg, true, noAgent(t)), "--plan", f.plan)
}

// readPlan decodes the saved plan as generic JSON, for tests that edit it.
func (f planFixture) readPlan(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("plan is not JSON: %v\n%s", err, b)
	}
	return m
}

func (f planFixture) writePlan(t *testing.T, m map[string]any) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.plan, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func planOps(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["operations"].([]any)
	if !ok {
		t.Fatalf("plan has no operations array: %v", m)
	}
	ops := make([]map[string]any, len(raw))
	for i, o := range raw {
		ops[i], _ = o.(map[string]any)
	}
	return ops
}

// rendered turns a plan operation's memory back into the canonical bytes it
// stands for.
func rendered(t *testing.T, memory any) string {
	t.Helper()
	b, err := json.Marshal(memory)
	if err != nil {
		t.Fatal(err)
	}
	var m schema.CanonicalMemory
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out, err := m.Render()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

const reviewedProposal = `{"op":"merge","sources":["dup-a","dup-b"],"memory":{"name":"dup","description":"merged","type":"lesson","scope":"global","body":"reviewed merge\n"},"reason":"same lesson"},
{"op":"remove","name":"stale","reason":"obsolete"},
{"op":"update","name":"keep","memory":{"name":"keep","description":"tightened","type":"lesson","scope":"global","body":"reviewed update\n"},"reason":"tighten"},
{"op":"add","memory":{"name":"fresh","description":"new","type":"reference","scope":"project:acme","body":"reviewed add\n"},"reason":"missing"},
{"op":"rescope","name":"wide","to_scope":"project:acme","reason":"repo specific"}`

// The plan the dry run saved is the plan --apply commits, operation for
// operation and byte for byte, and applying it runs no agent: a second proposal
// (here, removing keep) never reaches canonical.
func TestCurateAppliesTheSavedPlanNotANewProposal(t *testing.T) {
	f := newPlanFixture(t, "dup-a", "dup-b", "stale", "keep", "wide")
	saved := f.save(t, fenced(reviewedProposal))
	info, err := os.Stat(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("plan mode = %v, want 0600: it carries memory bodies", info.Mode().Perm())
	}
	planBytes, _ := os.ReadFile(f.plan)
	sum := sha256.Sum256(planBytes)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	ops := planOps(t, f.readPlan(t))
	if len(ops) != 5 {
		t.Fatalf("plan holds %d operations, want 5", len(ops))
	}

	code, resp := f.apply(t)
	if code != exitOK {
		t.Fatalf("apply exit = %d, want %d: %v", code, exitOK, resp)
	}
	for _, r := range []map[string]any{saved, resp} {
		p, _ := respData(r)["plan"].(map[string]any)
		if p["sha256"] != digest {
			t.Errorf("data.plan.sha256 = %v, want %s, the digest of the reviewed file", p["sha256"], digest)
		}
	}
	for _, i := range []int{0, 2, 3} {
		name, _ := ops[i]["memory"].(map[string]any)["name"].(string)
		got, err := os.ReadFile(filepath.Join(f.canon, name+".md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := rendered(t, ops[i]["memory"]); string(got) != want {
			t.Errorf("%s.md =\n%s\nwant the reviewed memory:\n%s", name, got, want)
		}
	}
	for _, gone := range []string{"dup-a", "dup-b", "stale"} {
		if exists(filepath.Join(f.canon, gone+".md")) {
			t.Errorf("%s.md should have been removed", gone)
		}
	}
	if ts, err := os.ReadFile(filepath.Join(f.canon, ".forgotten", "stale.yaml")); err != nil || !strings.Contains(string(ts), "obsolete") {
		t.Errorf("stale was not tombstoned with the reviewed reason: %v\n%s", err, ts)
	}
	if b, _ := os.ReadFile(filepath.Join(f.canon, "wide.md")); !strings.Contains(string(b), "scope: project:acme") {
		t.Errorf("wide was not rescoped:\n%s", b)
	}
}

// Any change to the store after the plan was saved makes it stale, an edit
// that leaves the parsed memory unchanged included: applying it refuses, exit 3,
// names what changed, and writes nothing.
func TestCuratePlanRefusesAChangedCorpus(t *testing.T) {
	cases := []struct {
		name, drift string
		change      func(t *testing.T, canon string)
	}{
		{"formatting-only edit", "b.md", func(t *testing.T, canon string) {
			writeFile(t, filepath.Join(canon, "b.md"),
				"---\n# a hand-written note\nname: b\ndescription: d\ntype: lesson\nscope: global\n---\nbody\n")
		}},
		{"target edited", "a.md", func(t *testing.T, canon string) {
			writeFile(t, filepath.Join(canon, "a.md"),
				"---\nname: a\ndescription: d\ntype: lesson\nscope: global\n---\nedited since review\n")
		}},
		{"memory added", "d.md", func(t *testing.T, canon string) { seedCanon(t, canon, "d") }},
		{"memory removed", "c.md", func(t *testing.T, canon string) {
			if err := os.Remove(filepath.Join(canon, "c.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"tombstone written", ".forgotten/z.yaml", func(t *testing.T, canon string) {
			writeFile(t, filepath.Join(canon, ".forgotten", "z.yaml"), "name: z\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPlanFixture(t, "a", "b", "c")
			f.save(t, fenced(`{"op":"remove","name":"a","reason":"old"}`))
			tc.change(t, f.canon)
			before := storeTree(t, f.canon)
			code, resp := f.apply(t)
			if code != exitConflicts || errCode(resp) != "stale_plan" {
				t.Fatalf("exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
			}
			drift, _ := respData(resp)["drift"].([]any)
			if len(drift) != 1 || drift[0] != tc.drift {
				t.Errorf("data.drift = %v, want [%s]", drift, tc.drift)
			}
			sameStore(t, before, storeTree(t, f.canon))
		})
	}
}

// The binding is checked again under the canonical lock, immediately before
// the batch writes: a change landing after the unlocked preflight passed still
// refuses the plan.
func TestCuratePlanRechecksTheCorpusUnderTheLock(t *testing.T) {
	f := newPlanFixture(t, "a", "b")
	f.save(t, fenced(`{"op":"remove","name":"a","reason":"old"}`))
	e := curateEnv(f.cfg, true, noAgent(t))
	e.beforeApplyLock = func() {
		writeFile(t, filepath.Join(f.canon, "b.md"),
			"---\nname: b\ndescription: d\ntype: lesson\nscope: global\n---\nconcurrent write\n")
	}
	code, resp := runCurate(t, e, "--plan", f.plan)
	if code != exitConflicts || errCode(resp) != "stale_plan" {
		t.Fatalf("exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
	}
	if !exists(filepath.Join(f.canon, "a.md")) {
		t.Error("a.md was removed by a plan whose corpus changed")
	}
}

// A plan file that is not exactly a plan engram wrote is refused before
// anything is read from the store: exit 2, nothing written, no agent run.
func TestCuratePlanRejectsAMalformedPlan(t *testing.T) {
	update := `{"op":"update","name":"a","memory":{"name":"a","description":"d","type":"lesson","scope":"global","body":"new\n"},"reason":"r"}`
	cases := []struct {
		name, code string
		edit       func(t *testing.T, f planFixture)
	}{
		{"not JSON", "invalid_plan", func(t *testing.T, f planFixture) { writeFile(t, f.plan, "not a plan\n") }},
		{"trailing data", "invalid_plan", func(t *testing.T, f planFixture) {
			b, _ := os.ReadFile(f.plan)
			writeFile(t, f.plan, string(b)+"{}\n")
		}},
		{"unknown field", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			m["approved_by"] = "someone"
			f.writePlan(t, m)
		}},
		{"unknown memory field", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			planOps(t, m)[0]["memory"].(map[string]any)["colour"] = "red"
			f.writePlan(t, m)
		}},
		{"future version", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			m["version"] = 2
			f.writePlan(t, m)
		}},
		{"wrong kind", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			m["kind"] = "something-else"
			f.writePlan(t, m)
		}},
		{"missing operations", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			delete(m, "operations")
			f.writePlan(t, m)
		}},
		{"null operations", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			m["operations"] = nil
			f.writePlan(t, m)
		}},
		{"missing corpus binding", "invalid_plan", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			delete(m, "corpus")
			f.writePlan(t, m)
		}},
		{"duplicate key", "invalid_plan", func(t *testing.T, f planFixture) {
			b, _ := os.ReadFile(f.plan)
			writeFile(t, f.plan, string(bytes.Replace(b, []byte("{"), []byte(`{"operations": [],`), 1)))
		}},
		{"another store", "plan_root_mismatch", func(t *testing.T, f planFixture) {
			m := f.readPlan(t)
			m["canonical_root"] = "/elsewhere/canonical"
			f.writePlan(t, m)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPlanFixture(t, "a", "b")
			f.save(t, fenced(update))
			tc.edit(t, f)
			before := storeTree(t, f.canon)
			code, resp := f.apply(t)
			if code != exitUsage || errCode(resp) != tc.code {
				t.Fatalf("exit = %d, error = %v; want %d %s", code, resp["error"], exitUsage, tc.code)
			}
			sameStore(t, before, storeTree(t, f.canon))
		})
	}
}

// A plan whose operations no longer validate (it was edited by hand) is
// refused whole, as an agent's invalid batch is: exit 3, nothing written. That
// includes a memory whose provenance differs from what apply would write, since
// the reviewed memory would not be the one written.
func TestCuratePlanRejectsInvalidOperations(t *testing.T) {
	cases := []struct {
		name string
		edit func(ops []map[string]any) []map[string]any
	}{
		{"unknown target", func([]map[string]any) []map[string]any {
			return []map[string]any{{"op": "remove", "name": "ghost"}}
		}},
		{"unsafe target", func([]map[string]any) []map[string]any {
			return []map[string]any{{"op": "remove", "name": "../a"}}
		}},
		{"duplicate merge source", func([]map[string]any) []map[string]any {
			return []map[string]any{{
				"op": "merge", "sources": []string{"b", "b"},
				"memory": map[string]any{"name": "m", "description": "d", "type": "lesson", "scope": "global"},
			}}
		}},
		{"forged provenance", func(ops []map[string]any) []map[string]any {
			ops[0]["memory"].(map[string]any)["provenance"] = map[string]any{"origin": "import:claude-code"}
			return ops
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPlanFixture(t, "b")
			writeFile(t, filepath.Join(f.canon, "a.md"),
				"---\nname: a\ndescription: d\ntype: lesson\nscope: global\nprovenance:\n  origin: import:codex\n---\nbody\n")
			f.save(t, fenced(`{"op":"update","name":"a","memory":{"name":"a","description":"d","type":"lesson","scope":"global","body":"new\n"},"reason":"r"}`))
			m := f.readPlan(t)
			m["operations"] = tc.edit(planOps(t, m))
			f.writePlan(t, m)
			before := storeTree(t, f.canon)
			code, resp := f.apply(t)
			if code != exitConflicts || errCode(resp) != "invalid_proposal" {
				t.Fatalf("exit = %d, error = %v; want %d invalid_proposal", code, resp["error"], exitConflicts)
			}
			sameStore(t, before, storeTree(t, f.canon))
		})
	}
}

// --plan without --apply re-checks a saved plan against the store as it is now,
// running no agent and writing nothing.
func TestCuratePlanPreviewChecksWithoutWriting(t *testing.T) {
	f := newPlanFixture(t, "a")
	f.save(t, fenced(`{"op":"remove","name":"a","reason":"old"}`))
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--plan", f.plan)
	if code != exitOK {
		t.Fatalf("preview exit = %d, want %d: %v", code, exitOK, resp)
	}
	if ops, _ := respData(resp)["operations"].([]any); len(ops) != 1 || ops[0].(map[string]any)["valid"] != true {
		t.Errorf("data.operations = %v, want the one valid remove", respData(resp)["operations"])
	}
	if !exists(filepath.Join(f.canon, "a.md")) {
		t.Fatal("a preview removed a.md")
	}
	seedCanon(t, f.canon, "late")
	code, resp = runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--plan", f.plan)
	if code != exitConflicts || errCode(resp) != "stale_plan" {
		t.Errorf("stale preview: exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
	}
}

// A canonical_root that is a symlink to the store saves, previews and applies a
// plan like the store's real path, either spelling applying a plan saved through
// the other, and a change behind the link makes it stale. The root stays a link.
func TestCuratePlanThroughALinkedRoot(t *testing.T) {
	f := newPlanFixture(t, "a", "b")
	link := filepath.Join(f.dir, "canonical-link")
	if err := os.Symlink(f.canon, link); err != nil {
		t.Fatal(err)
	}
	realCfg := filepath.Join(f.dir, "real.yaml")
	writeFile(t, realCfg, "canonical_root: "+f.canon+"\n")
	writeFile(t, f.cfg, "canonical_root: "+link+"\n")
	update := `{"op":"update","name":"a","memory":{"name":"a","description":"d","type":"lesson","scope":"global","body":"reviewed\n"},"reason":"r"}`
	f.save(t, fenced(update))
	for _, cfg := range []string{f.cfg, realCfg} {
		code, resp := runCurate(t, curateEnv(cfg, false, noAgent(t)), "--plan", f.plan)
		if code != exitOK {
			t.Fatalf("preview with %s: exit = %d, want %d: %v", filepath.Base(cfg), code, exitOK, resp)
		}
	}

	before := storeTree(t, f.canon)
	writeFile(t, filepath.Join(f.canon, "b.md"), "---\nname: b\ndescription: d\ntype: lesson\nscope: global\n---\nedited\n")
	code, resp := f.apply(t)
	if code != exitConflicts || errCode(resp) != "stale_plan" {
		t.Fatalf("stale apply: exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
	}
	writeFile(t, filepath.Join(f.canon, "b.md"), before["b.md"])

	code, resp = runCurate(t, curateEnv(realCfg, true, noAgent(t)), "--plan", f.plan)
	if code != exitOK {
		t.Fatalf("apply through the real path: exit = %d, want %d: %v", code, exitOK, resp)
	}
	if b, _ := os.ReadFile(filepath.Join(f.canon, "a.md")); !strings.Contains(string(b), "reviewed") {
		t.Errorf("a.md was not updated:\n%s", b)
	}

	if err := os.Remove(f.plan); err != nil {
		t.Fatal(err)
	}
	code, resp = runCurate(t, curateEnv(realCfg, false, fakeClaudeRunner(fenced(`{"op":"remove","name":"b","reason":"old"}`))), "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save-plan through the real path: exit = %d, want %d: %v", code, exitOK, resp)
	}
	if code, resp := f.apply(t); code != exitOK {
		t.Fatalf("apply through the link: exit = %d, want %d: %v", code, exitOK, resp)
	}
	if exists(filepath.Join(f.canon, "b.md")) {
		t.Error("b.md was not removed")
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the root link was replaced: %v", err)
	}
}

// A canonical_root spelled relative to the working directory saves, previews
// and applies a plan like its absolute spelling: through the root link or the
// real path, either applying a plan saved through the other. Another store,
// relative or not, is refused with nothing written.
func TestCuratePlanThroughARelativeRoot(t *testing.T) {
	for _, tc := range []struct{ saved, applied string }{
		{"canonical-link", "canonical"},
		{"canonical", "canonical-link"},
		{"abs:canonical-link", "canonical"},
		{"canonical", "abs:canonical-link"},
		{"canonical-link", "abs:canonical"},
		{"abs:canonical", "canonical-link"},
	} {
		t.Run(tc.saved+" then "+tc.applied, func(t *testing.T) {
			f := newPlanFixture(t, "a", "b")
			link := filepath.Join(f.dir, "canonical-link")
			if err := os.Symlink("canonical", link); err != nil {
				t.Fatal(err)
			}
			cfg := func(name, root string) string {
				if r, ok := strings.CutPrefix(root, "abs:"); ok {
					root = filepath.Join(f.dir, r)
				}
				p := filepath.Join(f.dir, name+".yaml")
				writeFile(t, p, "canonical_root: "+root+"\n")
				return p
			}
			savedCfg, appliedCfg := cfg("saved", tc.saved), cfg("applied", tc.applied)
			t.Chdir(f.dir)
			code, resp := runCurate(t, curateEnv(savedCfg, false, fakeClaudeRunner(fenced(`{"op":"remove","name":"a","reason":"old"}`))), "--save-plan", f.plan)
			if code != exitOK {
				t.Fatalf("save-plan: exit = %d, want %d: %v", code, exitOK, resp)
			}
			if code, resp := runCurate(t, curateEnv(appliedCfg, false, noAgent(t)), "--plan", f.plan); code != exitOK {
				t.Fatalf("preview: exit = %d, want %d: %v", code, exitOK, resp)
			}
			if code, resp := runCurate(t, curateEnv(appliedCfg, true, noAgent(t)), "--plan", f.plan); code != exitOK {
				t.Fatalf("apply: exit = %d, want %d: %v", code, exitOK, resp)
			}
			if exists(filepath.Join(f.canon, "a.md")) {
				t.Error("a.md was not removed")
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
				t.Errorf("the root link was replaced: %v", err)
			}
		})
	}

	t.Run("another store", func(t *testing.T) {
		f := newPlanFixture(t, "a", "b")
		other := filepath.Join(f.dir, "other")
		seedCanon(t, other, "a", "b")
		writeFile(t, f.cfg, "canonical_root: canonical\n")
		t.Chdir(f.dir)
		f.save(t, fenced(`{"op":"remove","name":"a","reason":"old"}`))
		for _, root := range []string{"other", other} {
			otherCfg := filepath.Join(f.dir, "other.yaml")
			writeFile(t, otherCfg, "canonical_root: "+root+"\n")
			before := storeTree(t, other)
			code, resp := runCurate(t, curateEnv(otherCfg, true, noAgent(t)), "--plan", f.plan)
			if code != exitUsage || errCode(resp) != "plan_root_mismatch" {
				t.Errorf("apply to %s: exit = %d, error = %v; want %d plan_root_mismatch", root, code, resp["error"], exitUsage)
			}
			sameStore(t, before, storeTree(t, other))
		}
	})
}

// newDirLinkFixture is a plan fixture whose store also holds shelf, a symlink
// to a directory outside it that holds a valid memory, hidden.
func newDirLinkFixture(t *testing.T, names ...string) (f planFixture, shelf string) {
	t.Helper()
	f = newPlanFixture(t, names...)
	shelf = filepath.Join(f.dir, "shelf-target")
	writeFile(t, filepath.Join(shelf, "hidden.md"),
		"---\nname: hidden\ndescription: d\ntype: lesson\nscope: global\n---\nhidden-marker\n")
	if err := os.Symlink(shelf, filepath.Join(f.canon, "shelf")); err != nil {
		t.Fatal(err)
	}
	return f, shelf
}

// A store holding a symlink to a directory saves, previews and applies a plan.
// The link is bound by its target, never read through: the memory behind it is
// not shown to the agent or bound, so editing it leaves the plan current, and
// the link survives the apply.
func TestCuratePlanOverADirectoryLink(t *testing.T) {
	f, shelf := newDirLinkFixture(t, "a", "b")
	var prompt string
	runner := func(inv agentexec.Invocation) ([]byte, error) {
		prompt = inv.Stdin
		return fakeClaudeRunner(fenced(`{"op":"remove","name":"a","reason":"old"}`))(inv)
	}
	code, resp := runCurate(t, curateEnv(f.cfg, false, runner), "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save-plan exit = %d, want %d: %v", code, exitOK, resp)
	}
	if !strings.Contains(prompt, `"b"`) || strings.Contains(prompt, "hidden") {
		t.Errorf("the agent was shown the wrong corpus:\n%s", prompt)
	}
	files, _ := f.readPlan(t)["corpus"].(map[string]any)["files"].(map[string]any)
	if _, ok := files["shelf"]; !ok || len(files) != 3 {
		t.Errorf("corpus.files = %v, want a.md, b.md and shelf", files)
	}

	writeFile(t, filepath.Join(shelf, "hidden.md"),
		"---\nname: hidden\ndescription: d\ntype: lesson\nscope: global\n---\nedited behind the link\n")
	if code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--plan", f.plan); code != exitOK {
		t.Fatalf("preview exit = %d, want %d: %v", code, exitOK, resp)
	}
	if code, resp := f.apply(t); code != exitOK {
		t.Fatalf("apply exit = %d, want %d: %v", code, exitOK, resp)
	}
	if exists(filepath.Join(f.canon, "a.md")) {
		t.Error("a.md was not removed")
	}
	if got, err := os.Readlink(filepath.Join(f.canon, "shelf")); err != nil || got != shelf {
		t.Errorf("shelf is not the original link (readlink = %q, %v)", got, err)
	}
	if !exists(filepath.Join(shelf, "hidden.md")) {
		t.Error("the memory behind the link was removed")
	}
}

// Retargeting or removing a directory link after the plan was saved makes it
// stale, naming the link, and nothing is written.
func TestCuratePlanRefusesAChangedDirectoryLink(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, link string){
		"retargeted": func(t *testing.T, link string) {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
		},
		"removed": func(t *testing.T, link string) {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := newDirLinkFixture(t, "a", "b")
			f.save(t, fenced(`{"op":"remove","name":"a","reason":"old"}`))
			change(t, filepath.Join(f.canon, "shelf"))
			before := storeTree(t, f.canon)
			code, resp := f.apply(t)
			if code != exitConflicts || errCode(resp) != "stale_plan" {
				t.Fatalf("exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
			}
			if drift, _ := respData(resp)["drift"].([]any); len(drift) != 1 || drift[0] != "shelf" {
				t.Errorf("data.drift = %v, want [shelf]", drift)
			}
			sameStore(t, before, storeTree(t, f.canon))
		})
	}
}

// --save-plan never overwrites a file, never writes into the canonical store
// (where the plan would change the corpus it is bound to), and is checked
// before the agent runs, so a bad path costs no model call.
func TestCurateSavePlanRefusesAnUnsafeOutputPath(t *testing.T) {
	f := newPlanFixture(t, "a")
	link := filepath.Join(f.dir, "into-canon")
	if err := os.Symlink(f.canon, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.canon, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	subLink := filepath.Join(f.dir, "into-sub")
	if err := os.Symlink(filepath.Join(f.canon, "sub"), subLink); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	writeFile(t, f.plan, "an earlier plan\n")
	cases := []struct{ name, path, code string }{
		{"existing file", f.plan, "plan_exists"},
		{"inside the store", filepath.Join(f.canon, "plan.json"), "plan_path"},
		{"through a link into the store", filepath.Join(link, "plan.json"), "plan_path"},
		// Lexically this is f.dir/up.json, but the kernel resolves the ".."
		// from the link's target, which is in the store.
		{"up from a link into the store", subLink + sep + ".." + sep + "up.json", "plan_path"},
		{"not a file name", f.dir + sep, "plan_path"},
		{"missing directory", filepath.Join(f.dir, "missing", "plan.json"), "plan_path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := storeTree(t, f.canon)
			code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--save-plan", tc.path)
			if code != exitUsage || errCode(resp) != tc.code {
				t.Fatalf("exit = %d, error = %v; want %d %s", code, resp["error"], exitUsage, tc.code)
			}
			sameStore(t, before, storeTree(t, f.canon))
		})
	}
	if b, _ := os.ReadFile(f.plan); string(b) != "an earlier plan\n" {
		t.Errorf("the existing plan was overwritten: %q", b)
	}
}

// A store that does not exist yet still has a location, and a plan created
// there would take it: --save-plan refuses the root itself however the path is
// spelled, before any agent runs, and the root stays absent.
func TestCurateSavePlanRefusesAnAbsentRoot(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		name  string
		chdir bool
		paths func(t *testing.T, dir string) (root, out string)
	}{
		{"the root's own path", false, func(t *testing.T, dir string) (string, string) {
			return filepath.Join(dir, "canonical"), filepath.Join(dir, "canonical")
		}},
		{"through a symlinked parent", false, func(t *testing.T, dir string) (string, string) {
			link := filepath.Join(dir, "link")
			if err := os.Symlink(dir, link); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "canonical"), filepath.Join(link, "canonical")
		}},
		{"a relative spelling", true, func(t *testing.T, dir string) (string, string) {
			return filepath.Join(dir, "canonical"), "canonical"
		}},
		{"an unclean root", false, func(t *testing.T, dir string) (string, string) {
			return dir + sep + "." + sep + "canonical" + sep, filepath.Join(dir, "canonical")
		}},
		{"the target of a dangling root link", false, func(t *testing.T, dir string) (string, string) {
			root := filepath.Join(dir, "canonical")
			if err := os.Symlink("store", root); err != nil {
				t.Fatal(err)
			}
			return root, filepath.Join(dir, "store")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root, out := tc.paths(t, dir)
			cfg := filepath.Join(dir, "c.yaml")
			writeFile(t, cfg, "canonical_root: "+root+"\n")
			if tc.chdir {
				t.Chdir(dir)
			}
			code, resp := runCurate(t, curateEnv(cfg, false, noAgent(t)), "--save-plan", out)
			if code != exitUsage || errCode(resp) != "plan_path" {
				t.Errorf("exit = %d, error = %v; want %d plan_path", code, resp["error"], exitUsage)
			}
			for _, p := range []string{filepath.Join(dir, "canonical"), filepath.Join(dir, "store")} {
				if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() {
					t.Errorf("a plan was written at %s, where the store would be", p)
				}
			}
		})
	}
}

// A relative --save-plan path through a symlinked directory outside the store
// is fine: the plan lands where it was named, and --plan applies it.
func TestCurateSavePlanFollowsALinkOutsideTheStore(t *testing.T) {
	f := newPlanFixture(t, "a")
	plans := filepath.Join(f.dir, "plans")
	if err := os.Mkdir(plans, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(plans, filepath.Join(f.dir, "plans-link")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(f.dir)
	out := filepath.Join("plans-link", "p.json")
	proposal := fenced(`{"op":"remove","name":"a","reason":"old"}`)
	code, resp := runCurate(t, curateEnv(f.cfg, false, fakeClaudeRunner(proposal)), "--save-plan", out)
	if code != exitOK {
		t.Fatalf("save-plan exit = %d, want %d: %v", code, exitOK, resp)
	}
	if p, _ := respData(resp)["plan"].(map[string]any); p["path"] != out {
		t.Errorf("data.plan.path = %v, want %s as given", p["path"], out)
	}
	if !exists(filepath.Join(plans, "p.json")) {
		t.Fatal("the plan was not written into the linked directory")
	}
	code, resp = runCurate(t, curateEnv(f.cfg, true, noAgent(t)), "--plan", out)
	if code != exitOK || exists(filepath.Join(f.canon, "a.md")) {
		t.Fatalf("apply exit = %d (a.md kept: %v), want %d: %v", code, exists(filepath.Join(f.canon, "a.md")), exitOK, resp)
	}
}

// A store that does not fully parse cannot be bound or applied, so --save-plan
// refuses it before running the agent.
func TestCurateSavePlanRefusesAnUnparsedCorpus(t *testing.T) {
	f := newPlanFixture(t, "a")
	writeFile(t, filepath.Join(f.canon, "broken.md"), "no frontmatter\n")
	code, resp := runCurate(t, curateEnv(f.cfg, false, noAgent(t)), "--save-plan", f.plan)
	if code != exitError || errCode(resp) != "corpus_parse" {
		t.Fatalf("exit = %d, error = %v; want %d corpus_parse", code, resp["error"], exitError)
	}
	if exists(f.plan) {
		t.Error("a plan was saved for a store that does not parse")
	}
}

// No plan is saved for a proposal that does not validate, or for a store that
// changed while the agent ran (the plan would be stale on arrival).
func TestCurateSavePlanWritesOnlyACurrentValidPlan(t *testing.T) {
	t.Run("invalid proposal", func(t *testing.T) {
		f := newPlanFixture(t, "a")
		code, _ := runCurate(t, curateEnv(f.cfg, false, fakeClaudeRunner(fenced(`{"op":"remove","name":"ghost"}`))), "--save-plan", f.plan)
		if code != exitConflicts {
			t.Errorf("exit = %d, want %d", code, exitConflicts)
		}
		if exists(f.plan) {
			t.Error("a plan was saved for an invalid proposal")
		}
	})
	t.Run("store changed during the run", func(t *testing.T) {
		f := newPlanFixture(t, "a")
		racing := func(inv agentexec.Invocation) ([]byte, error) {
			seedCanon(t, f.canon, "written-meanwhile")
			return fakeClaudeRunner(fenced(`{"op":"remove","name":"a","reason":"old"}`))(inv)
		}
		code, resp := runCurate(t, curateEnv(f.cfg, false, racing), "--save-plan", f.plan)
		if code != exitConflicts || errCode(resp) != "stale_plan" {
			t.Errorf("exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
		}
		if exists(f.plan) {
			t.Error("a plan was saved although the store changed under the agent")
		}
	})
}

// --plan runs no agent, so the agent flags are usage errors with it rather than
// silently ignored; --save-plan is a dry-run output and cannot be combined with
// --apply or --plan.
func TestCuratePlanFlagCombinations(t *testing.T) {
	f := newPlanFixture(t, "a")
	other := filepath.Join(f.dir, "other.json")
	cases := []struct {
		apply bool
		args  []string
	}{
		{true, []string{"--plan", f.plan, "--harness", "codex"}},
		{true, []string{"--plan", f.plan, "--model", "m"}},
		{false, []string{"--plan", f.plan, "--effort", "high"}},
		{true, []string{"--plan", f.plan, "--timeout", "1m"}},
		{false, []string{"--plan", f.plan, "--save-plan", other}},
		{true, []string{"--save-plan", other}},
	}
	for _, tc := range cases {
		code, resp := runCurate(t, curateEnv(f.cfg, tc.apply, noAgent(t)), tc.args...)
		if code != exitUsage || errCode(resp) != "usage" {
			t.Errorf("%v (apply %v): exit = %d, error = %v; want %d usage", tc.args, tc.apply, code, resp["error"], exitUsage)
		}
	}
	if exists(other) {
		t.Error("a refused invocation wrote a plan")
	}
}

// A saved plan's batch that fails while being written rolls back like any
// curate batch, through the same atomic apply.
func TestCuratePlanRollsBackAFailedBatch(t *testing.T) {
	f := newPlanFixture(t, "dup-a", "dup-b")
	writeFile(t, filepath.Join(f.canon, ".forgotten"), "in the way\n")
	f.save(t, fenced(`{"op":"merge","sources":["dup-a","dup-b"],"memory":{"name":"dup","description":"d","type":"lesson","scope":"global","body":"b\n"},"reason":"same"}`))
	before := storeTree(t, f.canon)
	code, resp := f.apply(t)
	if code != exitError || errCode(resp) != "apply" {
		t.Fatalf("exit = %d, error = %v; want %d apply", code, resp["error"], exitError)
	}
	if applied, _ := respData(resp)["applied"].([]any); applied == nil || len(applied) != 0 {
		t.Errorf("data.applied = %v, want empty after a rollback", respData(resp)["applied"])
	}
	sameStore(t, before, storeTree(t, f.canon))
}

// The dry run shows the memory apply writes: an update keeps the stored
// provenance, so the reviewer sees that rather than whatever the agent wrote.
func TestCurateDryRunShowsTheProvenanceApplyWrites(t *testing.T) {
	f := newPlanFixture(t)
	writeFile(t, filepath.Join(f.canon, "a.md"),
		"---\nname: a\ndescription: d\ntype: lesson\nscope: global\nprovenance:\n  origin: import:codex\n---\nbody\n")
	proposal := fenced(`{"op":"update","name":"a","memory":{"name":"a","description":"d","type":"lesson","scope":"global","provenance":{"author":"agent"},"body":"new\n"},"reason":"r"}`)
	code, resp := runCurate(t, curateEnv(f.cfg, false, fakeClaudeRunner(proposal)))
	if code != exitOK {
		t.Fatalf("dry-run exit = %d, want %d: %v", code, exitOK, resp)
	}
	ops, _ := respData(resp)["operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("data.operations = %v", respData(resp)["operations"])
	}
	prov, _ := ops[0].(map[string]any)["memory"].(map[string]any)["provenance"].(map[string]any)
	if prov["origin"] != "import:codex" || prov["author"] != nil {
		t.Errorf("shown provenance = %v, want the stored {origin: import:codex}", prov)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/lock"
	"github.com/davisbuilds/engram/internal/sync"
)

// These tests pin the ordering between canonical writers and the commands that
// render from canonical: a render apply reads canonical and writes every render
// from that one read while holding the canonical lock (shared, or exclusive if
// it also writes canonical), so a writer lands wholly before the read or is
// refused until the renders are written. A writer that slipped in between
// would have its removal undone: the render of a memory it just removed,
// re-created from the earlier read.

// attempt runs an engram writer from inside a hook and returns its exit code
// and error code.
func attempt(t *testing.T, argv ...string) (int, string) {
	t.Helper()
	code, resp := runEnvelope(t, argv...)
	return code, errCode(resp)
}

// wantLocked fails unless the writer a hook ran was refused for the held lock.
func wantLocked(t *testing.T, writer string, code int, ecode string) {
	t.Helper()
	if code != exitError || ecode != "locked" {
		t.Errorf("%s during the render apply: exit = %d, error.code = %q; want %d locked", writer, code, ecode, exitError)
	}
}

// wantRenderTracksCanonical fails when a render exists for a memory canonical
// no longer holds (resurrected) or is missing for one it does.
func wantRenderTracksCanonical(t *testing.T, canon, render, name string) {
	t.Helper()
	inCanon, rendered := exists(filepath.Join(canon, name+".md")), exists(render)
	switch {
	case rendered && !inCanon:
		t.Errorf("%s was removed from canonical but its render was re-created", name)
	case inCanon && !rendered:
		t.Errorf("%s is in canonical but has no render", name)
	}
}

// removeG is a curate proposal removing g-mem.
const removeG = "```json\n{\"operations\":[{\"op\":\"remove\",\"name\":\"g-mem\",\"reason\":\"old\"}]}\n```"

func TestSyncApplyRefusesAForgetDuringItsRender(t *testing.T) {
	claude, c := sharedFixture(t)
	canon := filepath.Join(filepath.Dir(claude), "canonical")
	var code int
	var ecode string
	e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", canonHook: func(p string) {
		if p == hookSnapshot {
			code, ecode = attempt(t, "forget", "g-mem", "--apply", "--config", c[1])
		}
	}}
	defer silenceStdout(t)()
	if got := cmdSync(e, "sync", nil); got != exitOK {
		t.Fatalf("sync exit = %d", got)
	}
	wantLocked(t, "forget", code, ecode)
	wantRenderTracksCanonical(t, canon, filepath.Join(claude, "engram", "memory", "g-mem.md"), "g-mem")
}

func TestSyncApplyRefusesACurateRemoveDuringItsRender(t *testing.T) {
	claude, c := sharedFixture(t)
	canon := filepath.Join(filepath.Dir(claude), "canonical")
	var code int
	var ecode string
	e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", canonHook: func(p string) {
		if p == hookSnapshot {
			var resp map[string]any
			code, resp = runCurate(t, curateEnv(c[1], true, fakeClaudeRunner(removeG)))
			ecode = errCode(resp)
		}
	}}
	defer silenceStdout(t)()
	if got := cmdSync(e, "sync", nil); got != exitOK {
		t.Fatalf("sync exit = %d", got)
	}
	wantLocked(t, "curate --apply", code, ecode)
	wantRenderTracksCanonical(t, canon, filepath.Join(claude, "engram", "memory", "g-mem.md"), "g-mem")
}

// A writer that lands before the snapshot is simply seen: the render is never
// created, or is removed as stale.
func TestSyncApplySeesAForgetBeforeItsSnapshot(t *testing.T) {
	claude, c := sharedFixture(t)
	canon := filepath.Join(filepath.Dir(claude), "canonical")
	render := filepath.Join(claude, "engram", "memory", "g-mem.md")
	if code := runQuiet(t, append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("seed sync exit = %d", code)
	}
	var code int
	e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", beforeApplyLock: func() {
		code, _ = attempt(t, "forget", "g-mem", "--apply", "--config", c[1])
	}}
	defer silenceStdout(t)()
	if got := cmdSync(e, "sync", nil); got != exitOK {
		t.Fatalf("sync exit = %d", got)
	}
	if code != exitOK {
		t.Fatalf("forget before the snapshot: exit = %d, want %d", code, exitOK)
	}
	if exists(render) || exists(filepath.Join(canon, "g-mem.md")) {
		t.Error("a memory forgotten before sync's snapshot still renders")
	}
}

// A render apply refused the canonical lock writes nothing, and a dry run
// takes no lock and writes nothing (not even the lock file).
func TestSyncApplyUnderAHeldCanonicalLockWritesNothing(t *testing.T) {
	claude, c := sharedFixture(t)
	canon := filepath.Join(filepath.Dir(claude), "canonical")
	if code := runQuiet(t, append([]string{"sync", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("dry-run exit = %d", code)
	}
	if exists(filepath.Join(canon, lock.Name)) || exists(filepath.Join(claude, "engram")) {
		t.Fatal("a sync dry run wrote a lock file or a render")
	}
	release, err := lock.Acquire(canon)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	code, ecode := attempt(t, append([]string{"sync", "--apply", "--cwd", "/work/x"}, c[:2]...)...)
	if code != exitError || ecode != "locked" {
		t.Errorf("sync --apply under a held canonical lock: exit = %d, error.code = %q; want %d locked", code, ecode, exitError)
	}
	if exists(filepath.Join(claude, "engram")) || exists(filepath.Join(claude, "projects")) {
		t.Error("sync --apply wrote renders while a canonical writer held the lock")
	}
}

// Readers share the canonical lock: a sync never refuses another sync.
func TestSyncApplySharesTheCanonicalLockWithAnotherReader(t *testing.T) {
	claude, c := sharedFixture(t)
	var code int
	e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", canonHook: func(p string) {
		if p == hookSnapshot {
			code, _ = attempt(t, "sync", "--apply", "--cwd", "/work/y", "--config", c[1])
		}
	}}
	defer silenceStdout(t)()
	if got := cmdSync(e, "sync", nil); got != exitOK {
		t.Fatalf("outer sync exit = %d", got)
	}
	if code != exitOK {
		t.Errorf("a sync inside another sync's render: exit = %d, want %d", code, exitOK)
	}
	if !exists(filepath.Join(claude, "engram", "memory", "g-mem.md")) {
		t.Error("g-mem was not rendered into the shared dir")
	}
}

// A missing canonical root stays missing under sync --apply: creating it would
// turn "absent" into "empty" and prune every render.
func TestSyncApplyDoesNotCreateAMissingCanonicalRoot(t *testing.T) {
	canon, base, rendered := staleHoldFixture(t)
	if err := os.RemoveAll(canon); err != nil {
		t.Fatal(err)
	}
	if code := runQuiet(t, append([]string{"sync", "--apply"}, base...)...); code != exitOK {
		t.Fatalf("sync exit = %d", code)
	}
	if exists(canon) {
		t.Error("sync --apply created the missing canonical root")
	}
	if !exists(rendered) {
		t.Error("render pruned while the canonical root was missing")
	}
}

// reconcile --apply and import --shared --apply create a missing root (their
// exclusive lock lives there), but still render as for a missing root, not an
// empty store.
func TestExclusiveRenderAppliesKeepRendersWhenCanonicalRootIsMissing(t *testing.T) {
	canon, base, rendered := staleHoldFixture(t)
	if err := os.RemoveAll(canon); err != nil {
		t.Fatal(err)
	}
	runQuiet(t, append([]string{"reconcile", "--apply"}, base...)...)
	if !exists(rendered) {
		t.Error("reconcile pruned a render while the canonical root was missing")
	}
	if err := os.RemoveAll(canon); err != nil {
		t.Fatal(err)
	}
	runQuiet(t, append([]string{"import", "claude-code", "--shared", "--apply"}, base...)...)
	if !exists(rendered) {
		t.Error("import --shared pruned a render while the canonical root was missing")
	}
}

// seedReconciled reconciles both harnesses so canonical holds claude-lesson and
// codex-lesson, codex-lesson rendered (global) into Claude's shared dir. It
// returns the canonical root, args and that render.
func seedReconciled(t *testing.T) (canon string, args []string, render string) {
	t.Helper()
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	if code := runQuiet(t, append([]string{"reconcile", "--apply"}, args...)...); code != exitOK {
		t.Fatalf("seed reconcile exit = %d", code)
	}
	render = filepath.Join(sharedDirOf(claudeMem), "codex-lesson.md")
	if !exists(render) {
		t.Fatal("seed reconcile did not render codex-lesson into the shared dir")
	}
	return canon, args, render
}

func reconcileEnv(args []string) *env {
	return &env{jsonMode: true, apply: true, config: args[1], cwd: args[3]}
}

// Reconcile writes canonical, then propagates it, under one exclusive lock: a
// forget cannot land between the two.
func TestReconcileApplyRefusesAForgetDuringPropagation(t *testing.T) {
	canon, args, render := seedReconciled(t)
	var code int
	var ecode string
	e := reconcileEnv(args)
	e.canonHook = func(p string) {
		if p == hookSnapshot {
			code, ecode = attempt(t, "forget", "codex-lesson", "--apply", "--config", args[1])
		}
	}
	defer silenceStdout(t)()
	cmdReconcile(e, "reconcile", nil)
	wantLocked(t, "forget", code, ecode)
	wantRenderTracksCanonical(t, canon, render, "codex-lesson")
}

// Propagation renders canonical as it is under the lock, not as the unlocked
// simulation saw it: a content edit and a scope change landing between the
// two are rendered, not reverted.
func TestReconcileApplyPropagatesCanonicalAsItIsUnderTheLock(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		canon, args, render := seedReconciled(t)
		file := filepath.Join(canon, "codex-lesson.md")
		e := reconcileEnv(args)
		e.beforeApplyLock = func() {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, file, strings.Replace(string(b), "codex body worth keeping", "canonical moved on", 1))
		}
		defer silenceStdout(t)()
		cmdReconcile(e, "reconcile", nil)
		if b, _ := os.ReadFile(render); !strings.Contains(string(b), "canonical moved on") {
			t.Errorf("the render lost a canonical edit made before the lock:\n%s", b)
		}
	})
	t.Run("scope", func(t *testing.T) {
		_, args, render := seedReconciled(t)
		var code int
		e := reconcileEnv(args)
		e.beforeApplyLock = func() {
			code, _ = attempt(t, "share", "codex-lesson", "--to", "project:elsewhere", "--config", args[1])
		}
		defer silenceStdout(t)()
		cmdReconcile(e, "reconcile", nil)
		if code != exitOK {
			t.Fatalf("share before the lock: exit = %d", code)
		}
		if exists(render) {
			t.Error("a memory narrowed to another project before the lock still renders into the shared dir")
		}
	})
}

// A canonical file that stops parsing between the simulation and the lock
// holds stale removals, exactly as it would had it been malformed from the
// start: its render survives, and the warning says why.
func TestReconcileApplyHoldsStaleRemovalsForAFileMalformedUnderTheLock(t *testing.T) {
	canon, args, render := seedReconciled(t)
	e := reconcileEnv(args)
	e.beforeApplyLock = func() {
		writeFile(t, filepath.Join(canon, "codex-lesson.md"), "---\nname: codex-lesson\n")
	}
	var resp map[string]any
	out := captureStdout(t, func() { cmdReconcile(e, "reconcile", nil) })
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out)
	}
	if !exists(render) {
		t.Error("the render of a memory whose canonical file went malformed was removed")
	}
	warns := stringify(resp["warnings"])
	for _, want := range []string{"unparseable canonical file", "stale renders kept"} {
		if !strings.Contains(warns, want) {
			t.Errorf("warnings lack %q: %s", want, warns)
		}
	}
}

func TestImportSharedApplyRefusesAForgetDuringItsRender(t *testing.T) {
	_, c, canon, render := editedShared(t)
	var code int
	var ecode string
	e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", canonHook: func(p string) {
		if p == hookSnapshot {
			code, ecode = attempt(t, "forget", "g-mem", "--apply", "--config", c[1])
		}
	}}
	s := mustSession(t, e)
	defer silenceStdout(t)()
	cmdImportShared(e, "import", s, nil, nil)
	wantLocked(t, "forget", code, ecode)
	wantRenderTracksCanonical(t, canon, render, "g-mem")
	if gMemBody(t, canon) != sharedEdit {
		t.Errorf("canonical g-mem body = %q, want the imported edit", gMemBody(t, canon))
	}
}

// migrateFixture imports a hand-authored Claude memory, so migrate can adopt
// its original in place. It returns the canonical root, args and the original.
func migrateFixture(t *testing.T) (canon string, args []string, original string) {
	t.Helper()
	dir := t.TempDir()
	canon = filepath.Join(dir, "canonical")
	claude := filepath.Join(dir, "claude")
	original = filepath.Join(claude, "projects", "-work-x", "memory", "vault.md")
	writeFile(t, original, "---\nname: vault\ndescription: where\nmetadata:\n  type: reference\n---\nvault body\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\nharnesses:\n  claude-code:\n    home: "+claude+"\n    shared_index: false\n")
	args = []string{"--config", cfg, "--cwd", "/work/x"}
	if code := runQuiet(t, append([]string{"import", "claude-code", "--apply", "--json"}, args...)...); code != exitOK {
		t.Fatalf("seed import exit = %d", code)
	}
	return canon, args, original
}

// migrate --apply renders canonical into a slug (an adopted original becomes
// engram's render of canonical), so it reads canonical under the lock too: a
// held writer refuses it before anything changes, and a forget during it is
// refused rather than leaving an engram render of a forgotten memory.
func TestMigrateApplyHoldsTheCanonicalLock(t *testing.T) {
	t.Run("held", func(t *testing.T) {
		canon, args, original := migrateFixture(t)
		before, _ := os.ReadFile(original)
		release, err := lock.Acquire(canon)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		code, ecode := attempt(t, append([]string{"migrate", "claude-code", "--apply"}, args...)...)
		if code != exitError || ecode != "locked" {
			t.Errorf("migrate --apply under a held lock: exit = %d, error.code = %q; want %d locked", code, ecode, exitError)
		}
		if after, _ := os.ReadFile(original); string(after) != string(before) {
			t.Errorf("the hand-authored original changed under a refused migrate:\n%s", after)
		}
	})
	t.Run("forget during", func(t *testing.T) {
		canon, args, original := migrateFixture(t)
		var code int
		var ecode string
		e := &env{jsonMode: true, apply: true, config: args[1], cwd: args[3], canonHook: func(p string) {
			if p == hookSnapshot {
				code, ecode = attempt(t, "forget", "vault", "--apply", "--config", args[1])
			}
		}}
		defer silenceStdout(t)()
		cmdMigrate(e, "migrate", []string{"claude-code"})
		wantLocked(t, "forget", code, ecode)
		if b, err := os.ReadFile(original); err == nil && sync.IsEngramOwned(b) && !exists(filepath.Join(canon, "vault.md")) {
			t.Error("migrate adopted the original of a memory forgotten during it")
		}
	})
}

// migrate --shared --apply detaches canonical after removing originals, so it
// takes the canonical lock first: a held writer refuses it before any
// original is removed, never midway.
func TestMigrateSharedApplyTakesTheCanonicalLockFirst(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	if code := runQuiet(t, append([]string{"reconcile", "--apply"}, args...)...); code != exitOK {
		t.Fatalf("seed reconcile exit = %d", code)
	}
	native := filepath.Join(claudeMem, "lesson-a.md")
	release, err := lock.Acquire(canon)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	code, ecode := attempt(t, append([]string{"migrate", "claude-code", "--shared", "--apply"}, args...)...)
	if code != exitError || ecode != "locked" {
		t.Errorf("migrate --shared --apply under a held lock: exit = %d, error.code = %q; want %d locked", code, ecode, exitError)
	}
	if !exists(native) {
		t.Error("an original was removed although the canonical lock was refused")
	}
}

// The lock is held from migrate --shared's read through the detach: a writer
// cannot change a memory between the read that judges its original identical
// and the original's removal.
func TestMigrateSharedApplyRefusesAWriterDuringIt(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	if code := runQuiet(t, append([]string{"reconcile", "--apply"}, args...)...); code != exitOK {
		t.Fatalf("seed reconcile exit = %d", code)
	}
	var code int
	var ecode string
	e := &env{jsonMode: true, apply: true, config: args[1], cwd: args[3], canonHook: func(p string) {
		if p == hookSnapshot {
			code, ecode = attempt(t, "remember", "--name", "claude-lesson", "--description", "a claude lesson",
				"--type", "lesson", "--body", "canonical moved on", "--force", "--config", args[1])
		}
	}}
	defer silenceStdout(t)()
	cmdMigrate(e, "migrate", []string{"claude-code", "--shared"})
	wantLocked(t, "remember", code, ecode)
	b, _ := os.ReadFile(filepath.Join(canon, "claude-lesson.md"))
	if !exists(filepath.Join(claudeMem, "lesson-a.md")) && strings.Contains(string(b), "canonical moved on") {
		t.Error("migrate removed an original that canonical no longer matches")
	}
}

// forget holds the canonical lock from its tombstones through its purge, so
// no render apply runs between them.
func TestForgetHoldsTheCanonicalLockThroughItsPurge(t *testing.T) {
	canon, claudeMem, _, args := forgetFixture(t)
	var code int
	var ecode string
	e := &env{jsonMode: true, apply: true, config: args[1], cwd: args[3], canonHook: func(p string) {
		if p == hookPurge {
			code, ecode = attempt(t, "sync", "--apply", "--config", args[1], "--cwd", args[3])
		}
	}}
	defer silenceStdout(t)()
	if got := cmdForget(e, "forget", []string{"codex-lesson"}); got != exitOK {
		t.Fatalf("forget exit = %d", got)
	}
	wantLocked(t, "sync --apply", code, ecode)
	if exists(filepath.Join(sharedDirOf(claudeMem), "codex-lesson.md")) || exists(filepath.Join(canon, "codex-lesson.md")) {
		t.Error("forget left codex-lesson in canonical or its shared render")
	}
}

// A curate capture (--save-plan, or --apply) reads canonical under the shared
// lock, so a writer held at the time refuses it before the agent runs.
func TestCurateCaptureRefusesAHeldWriter(t *testing.T) {
	for _, mode := range []string{"save-plan", "apply"} {
		t.Run(mode, func(t *testing.T) {
			f := newPlanFixture(t, "a")
			release, err := lock.Acquire(f.canon)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			args := []string{"--save-plan", f.plan}
			if mode == "apply" {
				args = nil
			}
			code, resp := runCurate(t, curateEnv(f.cfg, mode == "apply", noAgent(t)), args...)
			if code != exitError || errCode(resp) != "locked" {
				t.Errorf("exit = %d, error = %v; want %d locked", code, resp["error"], exitError)
			}
			if exists(f.plan) {
				t.Error("a plan was saved from a capture refused the lock")
			}
		})
	}
}

// The capture's shared lock covers the fingerprint and the corpus read: an
// engram writer cannot change a memory between them and change it back, so
// the corpus the agent is shown is the store the plan is bound to.
func TestCurateCaptureIsCoherentWithItsBinding(t *testing.T) {
	f := newPlanFixture(t)
	remember := func(body string) (int, string) {
		return attempt(t, "remember", "--name", "a", "--description", "d", "--type", "lesson",
			"--body", body, "--force", "--config", f.cfg)
	}
	if code, _ := remember("original body"); code != exitOK {
		t.Fatalf("seed remember exit = %d", code)
	}
	var codes []int
	var prompt string
	e := curateEnv(f.cfg, false, func(inv agentexec.Invocation) ([]byte, error) {
		prompt = inv.Stdin
		return fakeClaudeRunner(fenced())(inv)
	})
	e.canonHook = func(p string) {
		switch p {
		case hookFingerprint:
			code, _ := remember("interleaved body")
			codes = append(codes, code)
		case hookSnapshot:
			code, _ := remember("original body")
			codes = append(codes, code)
		}
	}
	code, resp := runCurate(t, e, "--save-plan", f.plan)
	if code != exitOK {
		t.Fatalf("save-plan exit = %d: %v", code, resp["error"])
	}
	for _, c := range codes {
		if c != exitError {
			t.Errorf("a remember during the capture: exit = %d, want %d (locked)", c, exitError)
		}
	}
	if strings.Contains(prompt, "interleaved body") || !strings.Contains(prompt, "original body") {
		t.Error("the agent was shown a corpus other than the one the plan is bound to")
	}
}

// A capture that finds the root missing holds no lock, so it must not read a
// root a writer creates after that: a second writer could change a memory
// between the fingerprint and the corpus read and change it back. The capture
// is the empty store it found, and a plan bound to it is stale against the
// store the writer created.
func TestCaptureOfAMissingRootReadsNothingCreatedSince(t *testing.T) {
	for _, mode := range []string{"save-plan", "apply"} {
		t.Run(mode, func(t *testing.T) {
			f := newPlanFixture(t)
			if err := os.Remove(f.canon); err != nil {
				t.Fatal(err)
			}
			remember := func(body string) {
				if code, ecode := attempt(t, "remember", "--name", "a", "--description", "d", "--type", "lesson",
					"--body", body, "--force", "--config", f.cfg); code != exitOK {
					t.Fatalf("remember %q: exit = %d (%s)", body, code, ecode)
				}
			}
			var prompt string
			e := curateEnv(f.cfg, mode == "apply", func(inv agentexec.Invocation) ([]byte, error) {
				prompt = inv.Stdin
				return fakeClaudeRunner(fenced())(inv)
			})
			e.canonHook = func(p string) {
				switch p {
				case hookCaptureLock:
					if !rootAbsent(f.canon) {
						t.Fatal("the fixture's root exists before the capture")
					}
					remember("original body")
				case hookFingerprint:
					remember("interleaved body")
				case hookSnapshot:
					remember("original body")
				}
			}
			args := []string{"--save-plan", f.plan}
			if mode == "apply" {
				args = nil
			}
			code, resp := runCurate(t, e, args...)
			if strings.Contains(prompt, "interleaved body") || strings.Contains(prompt, "original body") {
				t.Errorf("the agent was shown a root created after the capture found it missing:\n%s", prompt)
			}
			if mode == "apply" {
				if code != exitOK {
					t.Errorf("apply exit = %d: %v", code, resp["error"])
				}
				return
			}
			if code != exitConflicts || errCode(resp) != "stale_plan" {
				t.Errorf("save-plan exit = %d, error = %v; want %d stale_plan", code, resp["error"], exitConflicts)
			}
			if exists(f.plan) {
				t.Error("a plan was saved from a capture that read an unlocked root")
			}
		})
	}
}

// With no writer about, a capture of a missing root leaves it missing, and a
// plan saved from it is bound to the empty store.
func TestCaptureOfAMissingRootKeepsItMissing(t *testing.T) {
	for _, mode := range []string{"dry-run", "save-plan"} {
		t.Run(mode, func(t *testing.T) {
			f := newPlanFixture(t)
			if err := os.Remove(f.canon); err != nil {
				t.Fatal(err)
			}
			var args []string
			if mode == "save-plan" {
				args = []string{"--save-plan", f.plan}
			}
			if code, resp := runCurate(t, curateEnv(f.cfg, false, fakeClaudeRunner(fenced())), args...); code != exitOK {
				t.Fatalf("exit = %d: %v", code, resp["error"])
			}
			if !rootAbsent(f.canon) {
				t.Error("the capture created the missing canonical root")
			}
			if mode != "save-plan" {
				return
			}
			var p struct {
				Corpus *struct {
					Files map[string]string `json:"files"`
				} `json:"corpus"`
			}
			raw, err := os.ReadFile(f.plan)
			if err == nil {
				err = json.Unmarshal(raw, &p)
			}
			if err != nil || p.Corpus == nil || p.Corpus.Files == nil || len(p.Corpus.Files) != 0 {
				t.Errorf("plan corpus = %s (%v); want an empty binding", raw, err)
			}
		})
	}
}

// The agent runs with no lock held: neither the capture's shared lock nor any
// other. A writer can take the canonical lock while it runs.
func TestCurateAgentRunsWithNoLockHeld(t *testing.T) {
	for _, mode := range []string{"save-plan", "apply"} {
		t.Run(mode, func(t *testing.T) {
			f := newPlanFixture(t, "a")
			e := curateEnv(f.cfg, mode == "apply", func(inv agentexec.Invocation) ([]byte, error) {
				release, err := lock.Acquire(f.canon)
				if err != nil {
					t.Errorf("the canonical lock is held while the agent runs: %v", err)
				} else {
					release()
				}
				return fakeClaudeRunner(fenced())(inv)
			})
			args := []string{"--save-plan", f.plan}
			if mode == "apply" {
				args = nil
			}
			if code, resp := runCurate(t, e, args...); code != exitOK {
				t.Fatalf("exit = %d: %v", code, resp["error"])
			}
		})
	}
}

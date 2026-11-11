package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests pin the import outcome vocabulary as one state machine shared by
// every import source (a native harness, Claude's shared renders) and every
// surface (import dry-run, import --apply, reconcile): the same candidate state
// reads the same everywhere, and `differs` only ever names fields.

// withheldGMem replaces canonical g-mem with a file that parses but fails
// validation, so discovery withholds it while it still claims the name.
func withheldGMem(t *testing.T, canon string) {
	t.Helper()
	writeFile(t, filepath.Join(canon, "g-mem.md"), "---\nname: g-mem\ndescription: d\ntype: bogus\nscope: global\n---\nbroken\n")
}

// TestSharedEditOfAWithheldCanonicalIsWithheldNotRemoved pins the shared-edit
// gate for a canonical file that claims the name but does not validate: it is
// a withheld conflict (as for a native import), not a removal, and no --keep
// is offered, since there is no readable canonical to keep.
func TestSharedEditOfAWithheldCanonicalIsWithheldNotRemoved(t *testing.T) {
	_, c, canon, _ := editedShared(t)
	withheldGMem(t, canon)
	code, env := runEnvelope(t, append([]string{"reconcile", "--cwd", "/work/x"}, c...)...)
	if code != exitConflicts {
		t.Fatalf("exit = %d, want %d", code, exitConflicts)
	}
	rows := stringify(env["data"].(map[string]any)["import"])
	if !strings.Contains(rows, `"withheld":`) || strings.Contains(rows, "canonical removed") {
		t.Errorf("shared row should carry withheld, not a removal: %s", rows)
	}
	if steps := stringify(env["next_steps"]); strings.Contains(steps, "--keep g-mem") {
		t.Errorf("--keep offered for a withheld canonical: %s", steps)
	}
}

// TestImportSharedKeepNeverDiscardsAnEditOfAWithheldCanonical pins the data
// loss this gate prevents: --keep on a withheld canonical must not discard the
// edit (removing the render while warning that stale renders are kept). The
// conflict stands until the canonical file is fixed.
func TestImportSharedKeepNeverDiscardsAnEditOfAWithheldCanonical(t *testing.T) {
	_, c, canon, render := editedShared(t)
	withheldGMem(t, canon)
	before, _ := os.ReadFile(render)
	code, env := runEnvelope(t, append([]string{"import", "claude-code", "--shared", "--keep", "g-mem", "--apply", "--cwd", "/work/x"}, c...)...)
	if code != exitConflicts {
		t.Errorf("exit = %d, want %d (still held)", code, exitConflicts)
	}
	after, err := os.ReadFile(render)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the edited render was discarded or rewritten (err %v):\n%s", err, after)
	}
	if rows := stringify(env["data"]); !strings.Contains(rows, `"withheld":`) || strings.Contains(rows, `"outcome":"kept"`) {
		t.Errorf("row should stay a withheld conflict: %s", rows)
	}
}

// TestSharedEditOfARemovedCanonicalNamesNoFields pins `differs` as a field
// list: an edit whose canonical memory is gone reports that in its own key,
// and the only resolution offered reads as one.
func TestSharedEditOfARemovedCanonicalNamesNoFields(t *testing.T) {
	_, c, canon, _ := editedShared(t)
	if err := os.Remove(filepath.Join(canon, "g-mem.md")); err != nil {
		t.Fatal(err)
	}
	code, env := runEnvelope(t, append([]string{"reconcile", "--cwd", "/work/x"}, c...)...)
	if code != exitConflicts {
		t.Fatalf("exit = %d, want %d", code, exitConflicts)
	}
	rows := stringify(env["data"].(map[string]any)["import"])
	if strings.Contains(rows, `"differs":`) || !strings.Contains(rows, `"canonical":"removed"`) {
		t.Errorf("removed row should carry canonical=removed and no differs: %s", rows)
	}
	steps := stringify(env["next_steps"])
	if !strings.Contains(steps, "--shared --keep g-mem") || strings.Contains(steps, `"reason":"or `) {
		t.Errorf("next_steps should offer --keep as the resolution, not as an alternative: %s", steps)
	}
}

// TestSharedEditOfAForgottenMemoryIsForgotten pins the tombstone gate, which
// comes first for every import source: a forgotten memory's edited render is
// reported as forgotten, never as a conflict to --refresh back into canonical.
func TestSharedEditOfAForgottenMemoryIsForgotten(t *testing.T) {
	_, c, canon, render := editedShared(t)
	edited, _ := os.ReadFile(render)
	if code, env := runEnvelope(t, append([]string{"forget", "g-mem", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("forget exit = %d: %v", code, env["error"])
	}
	// The edit reappears after the forget (an editor saving a stale buffer).
	writeFile(t, render, string(edited))
	code, env := runEnvelope(t, append([]string{"reconcile", "--cwd", "/work/x"}, c...)...)
	// Not a conflict in canonical, but the render still waits on the operator.
	if code != exitConflicts {
		t.Errorf("exit = %d, want %d (render held)", code, exitConflicts)
	}
	if steps := stringify(env["next_steps"]); strings.Contains(steps, "reconcile imports it") {
		t.Errorf("a lead promises reconcile imports a forgotten edit: %s", steps)
	}
	rows := stringify(env["data"].(map[string]any)["import"])
	if !strings.Contains(rows, `"name":"g-mem","outcome":"forgotten"`) {
		t.Errorf("shared row should read forgotten: %s", rows)
	}
	if steps := stringify(env["next_steps"]); strings.Contains(steps, "--refresh g-mem") {
		t.Errorf("--refresh offered for a forgotten memory: %s", steps)
	}
	if _, err := os.Stat(filepath.Join(canon, "g-mem.md")); err == nil {
		t.Error("forgotten memory came back into canonical")
	}
	// --keep settles it for canonical: the memory stays forgotten, the render goes.
	if code, env := runEnvelope(t, append([]string{"import", "claude-code", "--shared", "--keep", "g-mem", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
		t.Fatalf("--keep exit = %d: %s", code, stringify(env))
	}
	if _, err := os.Stat(render); err == nil {
		t.Error("--keep left the forgotten memory's edited render in place")
	}
}

// TestImportDryRunReportsAnInvalidCandidate pins dry-run parity: a native that
// does not validate is previewed as the `invalid` apply reports, not `created`.
func TestImportDryRunReportsAnInvalidCandidate(t *testing.T) {
	claude, c := sharedFixture(t)
	slug := filepath.Join(claude, "projects", "-work-x", "memory")
	writeFile(t, filepath.Join(slug, "bad-one.md"), "---\nname: bad-one\ndescription: d\nmetadata:\n  type: bogus\n---\nbody\n")
	writeFile(t, filepath.Join(slug, "MEMORY.md"), "- [bad](bad-one.md) — x\n")
	args := append([]string{"import", "claude-code", "--cwd", "/work/x"}, c...)
	_, dry := runEnvelope(t, args...)
	code, applied := runEnvelope(t, append(args, "--apply")...)
	// Nothing was imported for it: like reconcile, apply asks for resolution.
	if code != exitConflicts {
		t.Errorf("apply exit = %d, want %d", code, exitConflicts)
	}
	for label, env := range map[string]map[string]any{"dry-run": dry, "apply": applied} {
		if !strings.Contains(stringify(env["data"]), `"outcome":"invalid"`) {
			t.Errorf("%s should report invalid: %s", label, stringify(env["data"]))
		}
	}
}

// TestCanonLockReportsLockedOnlyForAHeldLock pins the retry signal: `locked`
// tells an agent to retry, so a lock that cannot be taken at all (the root
// cannot be created) is a plain `lock` failure, not a retryable one.
func TestCanonLockReportsLockedOnlyForAHeldLock(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	writeFile(t, file, "x")
	root := filepath.Join(file, "canonical")
	if _, rerr := canonLock(root); rerr == nil || rerr.Code != "lock" {
		t.Errorf("exclusive: got %+v, want code lock", rerr)
	}
	if _, _, rerr := canonReadLock(file); rerr == nil || rerr.Code != "lock" {
		t.Errorf("shared: got %+v, want code lock", rerr)
	}
	held := t.TempDir()
	release, rerr := canonLock(held)
	if rerr != nil {
		t.Fatal(rerr)
	}
	defer release()
	if _, rerr := canonLock(held); rerr == nil || rerr.Code != "locked" {
		t.Errorf("held: got %+v, want code locked", rerr)
	}
}

// TestSharedEditIsNeverDiscardedWhileCanonicalIsIncomplete pins the stale hold
// for shared edits: when discovery cannot vouch that canonical lacks the name
// (the root is missing, or a file that does not parse may be it), the edit is
// not reported removed and --keep does not discard it.
func TestSharedEditIsNeverDiscardedWhileCanonicalIsIncomplete(t *testing.T) {
	for name, incomplete := range map[string]func(t *testing.T, canon string){
		"root missing": func(t *testing.T, canon string) {
			if err := os.Rename(canon, canon+".away"); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable file": func(t *testing.T, canon string) {
			if err := os.Remove(filepath.Join(canon, "g-mem.md")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(canon, "renamed.md"), "not a memory\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, c, canon, render := editedShared(t)
			incomplete(t, canon)
			before, _ := os.ReadFile(render)
			_, env := runEnvelope(t, append([]string{"reconcile", "--cwd", "/work/x"}, c...)...)
			rows := stringify(env["data"].(map[string]any)["import"])
			if !strings.Contains(rows, `"canonical":"unknown"`) {
				t.Errorf("row should read canonical=unknown: %s", rows)
			}
			if steps := stringify(env["next_steps"]); strings.Contains(steps, "--keep g-mem") {
				t.Errorf("--keep offered while canonical is incomplete: %s", steps)
			}
			code, env := runEnvelope(t, append([]string{"import", "claude-code", "--shared", "--keep", "g-mem", "--apply", "--cwd", "/work/x"}, c...)...)
			if code != exitConflicts {
				t.Errorf("--keep exit = %d, want %d: %s", code, exitConflicts, stringify(env))
			}
			if after, err := os.ReadFile(render); err != nil || string(after) != string(before) {
				t.Errorf("--keep discarded or rewrote the edit (err %v)", err)
			}
		})
	}
}

// TestImportGatesAgreeAcrossSurfaces pins one gate order for every surface: a
// candidate both withheld (canonical claims the name but does not load) and
// invalid reads as the withheld conflict in import dry-run, apply and reconcile.
func TestImportGatesAgreeAcrossSurfaces(t *testing.T) {
	claude, c := sharedFixture(t)
	canon := filepath.Join(filepath.Dir(claude), "canonical")
	writeFile(t, filepath.Join(canon, "bad-one.md"), "---\nname: bad-one\ndescription: d\ntype: bogus\nscope: global\n---\nbroken\n")
	slug := filepath.Join(claude, "projects", "-work-x", "memory")
	writeFile(t, filepath.Join(slug, "bad-one.md"), "---\nname: bad-one\ndescription: d\nmetadata:\n  type: bogus\n---\nbody\n")
	writeFile(t, filepath.Join(slug, "MEMORY.md"), "- [bad](bad-one.md) — x\n")
	imp := append([]string{"import", "claude-code", "--cwd", "/work/x"}, c...)
	for label, argv := range map[string][]string{
		"import dry-run": imp,
		"import apply":   append(append([]string{}, imp...), "--apply"),
		"reconcile":      append([]string{"reconcile", "--cwd", "/work/x"}, c...),
	} {
		_, env := runEnvelope(t, argv...)
		if data := stringify(env["data"]); !strings.Contains(data, `"withheld":`) || strings.Contains(data, `"outcome":"invalid"`) {
			t.Errorf("%s should report the withheld conflict: %s", label, data)
		}
	}
}

// TestSharedEditRecheckedUnderTheLock pins applySharedEdits' recheck of a
// planned fast-forward against canonical as it is under the lock.
func TestSharedEditRecheckedUnderTheLock(t *testing.T) {
	for name, tc := range map[string]struct {
		change   func(t *testing.T, canon string, c []string)
		wantCode int
		want     string
		keepLead bool
	}{
		"withheld": {func(t *testing.T, canon string, _ []string) { withheldGMem(t, canon) }, exitConflicts, `"withheld"`, false},
		"removed": {func(t *testing.T, canon string, _ []string) {
			if err := os.Remove(filepath.Join(canon, "g-mem.md")); err != nil {
				t.Fatal(err)
			}
		}, exitConflicts, `"canonical": "removed"`, true},
		"converged": {func(t *testing.T, canon string, _ []string) {
			writeFile(t, filepath.Join(canon, "g-mem.md"), "---\nname: g-mem\ndescription: d\ntype: lesson\nscope: global\n---\n"+sharedEdit)
		}, exitOK, `"outcome": "unchanged"`, false},
		"forgotten": {func(t *testing.T, _ string, c []string) {
			defer silenceStdout(t)()
			if code := Run(append([]string{"forget", "g-mem", "--apply", "--cwd", "/work/x"}, c...)); code != exitOK {
				t.Fatalf("forget exit = %d", code)
			}
		}, exitOK, `"outcome": "forgotten"`, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, c, canon, _ := editedShared(t)
			var code int
			out := captureStdout(t, func() {
				e := &env{jsonMode: true, apply: true, config: c[1], cwd: "/work/x", beforeApplyLock: func() { tc.change(t, canon, c) }}
				code = cmdImportShared(e, "import", mustSession(t, e), nil, nil)
			})
			if code != tc.wantCode || !strings.Contains(out, tc.want) {
				t.Errorf("exit = %d (want %d), want %s in:\n%s", code, tc.wantCode, tc.want, out)
			}
			if got := strings.Contains(out, "--shared --keep g-mem"); got != tc.keepLead {
				t.Errorf("--keep lead offered = %v, want %v:\n%s", got, tc.keepLead, out)
			}
		})
	}
}

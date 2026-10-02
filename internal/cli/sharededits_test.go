package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/sync"
)

// sharedEdit is the body editedShared writes into g-mem's shared render.
const sharedEdit = "edited in place\n"

// editedShared syncs the shared fixture, then edits g-mem's shared render body
// in place. It returns the Claude home, args, canonical root and render path.
func editedShared(t *testing.T) (claude string, c []string, canon, render string) {
	t.Helper()
	claude, c = sharedFixture(t)
	canon = filepath.Join(filepath.Dir(claude), "canonical")
	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"sync", "--apply", "--cwd", "/work/x"}, c...)); code != exitOK {
			t.Fatalf("seed sync exit = %d", code)
		}
	}()
	render = filepath.Join(claude, "engram", "memory", "g-mem.md")
	b, err := os.ReadFile(render)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s[4:], "\n---\n") + 4 + len("\n---\n")
	writeFile(t, render, s[:i]+sharedEdit)
	return claude, c, canon, render
}

// gMemBody is the body of canonical g-mem.
func gMemBody(t *testing.T, canon string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(canon, "g-mem.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	return s[strings.Index(s[4:], "\n---\n")+4+len("\n---\n"):]
}

// TestReconcileImportsAnEditedSharedRender pins the fast-forward: an edit made
// in place to a shared render, against a canonical that has not moved, becomes
// canonical, and the render is restamped. A dry-run previews it without writing.
func TestReconcileImportsAnEditedSharedRender(t *testing.T) {
	claude, c, canon, _ := editedShared(t)
	args := append([]string{"reconcile", "--cwd", "/work/x"}, c...)
	code, env := runEnvelope(t, args...)
	if code != exitOK {
		t.Fatalf("dry-run exit = %d: %v", code, env["error"])
	}
	if !strings.Contains(stringify(env["data"]), `"outcome":"updated"`) || gMemBody(t, canon) != "global\n" {
		t.Errorf("dry-run should preview the update and write nothing: %s", stringify(env["data"]))
	}
	if code, env := runEnvelope(t, append(args, "--apply")...); code != exitOK {
		t.Fatalf("apply exit = %d: %v", code, env["error"])
	}
	if got := gMemBody(t, canon); got != "edited in place\n" {
		t.Errorf("canonical body = %q, want the edit", got)
	}
	if edits, _ := sync.ScanSharedEdits(filepath.Join(claude, "engram", "memory")); len(edits) != 0 {
		t.Errorf("render not restamped after the import: %+v", edits)
	}
}

// TestReconcileHoldsASharedEditWhenCanonicalMovedToo pins the conflict: both
// sides moved, so nothing is overwritten and the leads offer both resolutions.
func TestReconcileHoldsASharedEditWhenCanonicalMovedToo(t *testing.T) {
	_, c, canon, render := editedShared(t)
	writeFile(t, filepath.Join(canon, "g-mem.md"), "---\nname: g-mem\ndescription: d\ntype: lesson\nscope: global\n---\ncanonical moved\n")
	before, _ := os.ReadFile(render)
	code, env := runEnvelope(t, append([]string{"reconcile", "--apply", "--cwd", "/work/x"}, c...)...)
	if code != exitConflicts {
		t.Fatalf("exit = %d, want %d", code, exitConflicts)
	}
	if after, _ := os.ReadFile(render); string(after) != string(before) {
		t.Error("an edited render was overwritten")
	}
	if got := gMemBody(t, canon); got != "canonical moved\n" {
		t.Errorf("canonical body = %q, want it untouched", got)
	}
	steps := stringify(env["next_steps"])
	for _, want := range []string{"--shared --refresh g-mem", "--shared --keep g-mem"} {
		if !strings.Contains(steps, want) {
			t.Errorf("next_steps lack %q: %s", want, steps)
		}
	}
}

// TestImportSharedRefreshAndKeep pins both resolutions of a held edit.
func TestImportSharedRefreshAndKeep(t *testing.T) {
	for _, tc := range []struct{ flag, wantCanon string }{
		{"--refresh", "edited in place\n"},
		{"--keep", "canonical moved\n"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			claude, c, canon, render := editedShared(t)
			writeFile(t, filepath.Join(canon, "g-mem.md"), "---\nname: g-mem\ndescription: d\ntype: lesson\nscope: global\n---\ncanonical moved\n")
			if code, env := runEnvelope(t, append([]string{"import", "claude-code", "--shared", tc.flag, "g-mem", "--apply", "--cwd", "/work/x"}, c...)...); code != exitOK {
				t.Fatalf("import exit = %d: %v", code, env["error"])
			}
			if got := gMemBody(t, canon); got != tc.wantCanon {
				t.Errorf("canonical body = %q, want %q", got, tc.wantCanon)
			}
			if b, _ := os.ReadFile(render); !strings.HasSuffix(string(b), tc.wantCanon) {
				t.Errorf("render after %s:\n%s", tc.flag, b)
			}
			if edits, _ := sync.ScanSharedEdits(filepath.Join(claude, "engram", "memory")); len(edits) != 0 {
				t.Errorf("render still reads as edited: %+v", edits)
			}
			if code, _ := runEnvelope(t, append([]string{"reconcile", "--cwd", "/work/x"}, c...)...); code != exitOK {
				t.Errorf("reconcile after %s exit = %d, want %d", tc.flag, code, exitOK)
			}
		})
	}
}

// TestImportSharedUsage pins the argument rules.
func TestImportSharedUsage(t *testing.T) {
	_, c, _, _ := editedShared(t)
	for name, argv := range map[string][]string{
		"with --all":      {"import", "claude-code", "--shared", "--all"},
		"with codex":      {"import", "codex", "--shared"},
		"unedited --keep": {"import", "claude-code", "--shared", "--keep", "p-mem"},
	} {
		if code, _ := runEnvelope(t, append(append(argv, "--cwd", "/work/x"), c...)...); code != exitUsage {
			t.Errorf("%s: exit = %d, want %d", name, code, exitUsage)
		}
	}
}

// stringify is v as compact JSON, for substring checks on envelope parts.
func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// TestImportSharedKeepAFastForwardableEdit pins --keep when canonical has not
// moved: the edit could fast-forward, but the operator chose canonical, so
// canonical is untouched and the render is restored from it.
func TestImportSharedKeepAFastForwardableEdit(t *testing.T) {
	_, c, canon, render := editedShared(t)
	code, env := runEnvelope(t, append([]string{"import", "claude-code", "--shared", "--keep", "g-mem", "--apply", "--cwd", "/work/x"}, c...)...)
	if code != exitOK {
		t.Fatalf("import exit = %d: %v", code, env["error"])
	}
	if got := gMemBody(t, canon); got != "global\n" {
		t.Errorf("canonical body = %q, want it kept (global)", got)
	}
	if b, _ := os.ReadFile(render); !strings.HasSuffix(string(b), "global\n") {
		t.Errorf("render not restored from canonical:\n%s", b)
	}
	if !strings.Contains(stringify(env["data"]), `"outcome":"kept"`) {
		t.Errorf("row should report the edit as kept-out: %s", stringify(env["data"]))
	}
}

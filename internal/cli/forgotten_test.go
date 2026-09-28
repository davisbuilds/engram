package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/tombstone"
)

// reconcileRows runs reconcile and returns its exit code and each import row by
// name, across harnesses.
func reconcileRows(t *testing.T, args ...string) (int, map[string]map[string]string) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = Run(append([]string{"reconcile"}, args...)) })
	var env struct {
		Data struct {
			Import []struct {
				Results []map[string]string `json:"results"`
			} `json:"import"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope: %v\n%s", err, out)
	}
	rows := map[string]map[string]string{}
	for _, h := range env.Data.Import {
		for _, r := range h.Results {
			rows[r["name"]] = r
		}
	}
	return code, rows
}

// forgottenFixture imports both harnesses' natives into canonical, renders each
// into the other harness, then forgets both canonical memories while their
// native sources live on.
func forgottenFixture(t *testing.T) (canon, claudeMem string, args []string) {
	t.Helper()
	canon, claudeMem, _, args = setupTwoHarnesses(t)
	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
			t.Fatalf("seed reconcile exit = %d", code)
		}
	}()
	for _, n := range []string{"claude-lesson", "codex-lesson"} {
		if _, err := tombstone.Forget(canon, n, tombstone.Note{Reason: "test"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return canon, claudeMem, args
}

func assertNoCanonical(t *testing.T, canon string, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(canon, n+".md")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("forgotten memory %s was re-created in canonical", n)
		}
	}
}

// import never re-creates a forgotten memory from its surviving native, not even
// with --refresh; it reports the candidate as forgotten, without a conflict.
func TestImportDoesNotResurrectForgottenMemories(t *testing.T) {
	canon, _, args := forgottenFixture(t)
	cases := []struct {
		name string
		argv []string
	}{
		{"claude-lesson", []string{"import", "claude-code"}},
		{"claude-lesson", []string{"import", "claude-code", "--apply"}},
		{"claude-lesson", []string{"import", "claude-code", "--all", "--apply", "--refresh", "claude-lesson"}},
		{"codex-lesson", []string{"import", "codex"}},
		{"codex-lesson", []string{"import", "codex", "--apply"}},
	}
	for _, c := range cases {
		code, rows := importRows(t, append(c.argv, args...)...)
		if code != exitOK {
			t.Errorf("%v: exit = %d, want %d", c.argv, code, exitOK)
		}
		if got := rows[c.name]["outcome"]; got != "forgotten" {
			t.Errorf("%v: %s outcome = %q, want forgotten", c.argv, c.name, got)
		}
	}
	assertNoCanonical(t, canon, "claude-lesson", "codex-lesson")
}

// reconcile reports forgotten candidates, keeps them out of the merged set, and
// so removes their renders from the other harness as stale.
func TestReconcileDoesNotResurrectForgottenMemories(t *testing.T) {
	canon, claudeMem, args := forgottenFixture(t)
	for _, argv := range [][]string{args, append([]string{"--apply"}, args...)} {
		code, rows := reconcileRows(t, argv...)
		if code != exitOK {
			t.Errorf("reconcile %v: exit = %d, want %d", argv, code, exitOK)
		}
		for _, n := range []string{"claude-lesson", "codex-lesson"} {
			if got := rows[n]["outcome"]; got != "forgotten" {
				t.Errorf("reconcile %v: %s outcome = %q, want forgotten", argv, n, got)
			}
		}
	}
	assertNoCanonical(t, canon, "claude-lesson", "codex-lesson")
	if _, err := os.Stat(filepath.Join(claudeMem, "codex-lesson.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the forgotten Codex lesson's Claude render should be removed as stale")
	}
}

// A forget landing between reconcile's simulation and its apply must not have
// its purged render re-created from the stale simulated set.
func TestReconcileApplyHonorsAForgetThatLandsMidRun(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	func() {
		defer silenceStdout(t)()
		if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
			t.Fatalf("seed reconcile exit = %d", code)
		}
	}()
	render := filepath.Join(claudeMem, "codex-lesson.md")
	e := &env{jsonMode: true, apply: true, config: args[1], cwd: args[3], beforeApplyLock: func() {
		// What a concurrent `forget codex-lesson --apply` does: tombstone the
		// memory and purge its render.
		if _, err := tombstone.Forget(canon, "codex-lesson", tombstone.Note{}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(render); err != nil {
			t.Fatal(err)
		}
	}}
	defer silenceStdout(t)()
	cmdReconcile(e, "reconcile", nil)
	if _, err := os.Stat(render); !errors.Is(err, os.ErrNotExist) {
		t.Error("reconcile re-created the render of a memory forgotten mid-run")
	}
	assertNoCanonical(t, canon, "codex-lesson")
}

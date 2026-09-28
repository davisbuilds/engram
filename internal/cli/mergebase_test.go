package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func seedImport(t *testing.T, args []string) {
	t.Helper()
	defer silenceStdout(t)()
	if code := Run(append([]string{"reconcile", "--apply"}, args...)); code != exitOK {
		t.Fatalf("seed reconcile exit = %d", code)
	}
}

// A native edited since the last agreeing import, with canonical untouched,
// fast-forwards: no conflict, no --refresh, and the edit reaches the other
// harness in the same reconcile.
func TestNativeOnlyEditFastForwards(t *testing.T) {
	canon, claudeMem, codexNotes, args := setupTwoHarnesses(t)
	seedImport(t, args)
	editNative(t, claudeMem, "lesson-a.md", "claude-lesson", "claude body v2")

	code, rows := importRows(t, append([]string{"import", "claude-code"}, args...)...)
	if code != exitOK || rows["claude-lesson"]["outcome"] != "updated" || !strings.Contains(rows["claude-lesson"]["differs"], "body") {
		t.Errorf("dry-run: exit %d row %v, want updated naming body", code, rows["claude-lesson"])
	}
	if code, rows := reconcileRows(t, append([]string{"--apply"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "updated" {
		t.Fatalf("reconcile --apply: exit %d row %v, want exit 0 and updated", code, rows["claude-lesson"])
	}
	got, _ := os.ReadFile(filepath.Join(canon, "claude-lesson.md"))
	if !strings.Contains(string(got), "claude body v2") || !strings.Contains(string(got), "description: edited desc") {
		t.Errorf("canonical not fast-forwarded:\n%s", got)
	}
	notes, _ := filepath.Glob(filepath.Join(codexNotes, "*claude-lesson.md"))
	if len(notes) != 1 {
		t.Fatalf("codex notes for claude-lesson = %v", notes)
	}
	if note, _ := os.ReadFile(notes[0]); !strings.Contains(string(note), "claude body v2") {
		t.Errorf("the fast-forwarded edit did not reach Codex:\n%s", note)
	}
}

// Canonical edited since the last agreeing import, native untouched: canonical
// is ahead, which is neither a write nor a conflict.
func TestCanonicalOnlyEditIsNotAConflict(t *testing.T) {
	canon, _, _, args := setupTwoHarnesses(t)
	seedImport(t, args)
	file := filepath.Join(canon, "claude-lesson.md")
	editCanonicalBody(t, file)
	before, _ := os.ReadFile(file)

	code, rows := importRows(t, append([]string{"import", "claude-code", "--apply"}, args...)...)
	if code != exitOK || rows["claude-lesson"]["outcome"] != "canonical_ahead" {
		t.Errorf("import --apply: exit %d row %v, want exit 0 and canonical_ahead", code, rows["claude-lesson"])
	}
	if code, rows := reconcileRows(t, args...); code != exitOK || rows["claude-lesson"]["outcome"] != "canonical_ahead" {
		t.Errorf("reconcile: exit %d row %v, want exit 0 and canonical_ahead", code, rows["claude-lesson"])
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Error("canonical_ahead must not write")
	}
	// --refresh still lets the operator take the native deliberately.
	if code, rows := importRows(t, append([]string{"import", "claude-code", "--apply", "--refresh", "claude-lesson"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "updated" {
		t.Errorf("--refresh over canonical_ahead: exit %d row %v", code, rows["claude-lesson"])
	}
	if after, _ := os.ReadFile(file); strings.Contains(string(after), "curated addition") {
		t.Error("--refresh should have taken the native content")
	}
}

// A store written before merge bases existed gets one the first time canonical
// and native agree (a one-time provenance update); from then on a native edit
// fast-forwards.
func TestMergeBaseBootstrapsOnAgreement(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	seedImport(t, args)
	file := filepath.Join(canon, "claude-lesson.md")
	b, _ := os.ReadFile(file)
	legacy := regexp.MustCompile(`(?m)^\s*import_hash: .*\n`).ReplaceAllString(string(b), "")
	if legacy == string(b) {
		t.Fatal("fixture: no import_hash to strip")
	}
	writeFile(t, file, legacy)

	if _, rows := importRows(t, append([]string{"import", "claude-code", "--apply"}, args...)...); rows["claude-lesson"]["outcome"] != "updated" {
		t.Errorf("bootstrap row = %v, want updated (base recorded)", rows["claude-lesson"])
	}
	if _, rows := importRows(t, append([]string{"import", "claude-code", "--apply"}, args...)...); rows["claude-lesson"]["outcome"] != "unchanged" {
		t.Errorf("second import row = %v, want unchanged", rows["claude-lesson"])
	}
	editNative(t, claudeMem, "lesson-a.md", "claude-lesson", "after bootstrap")
	if code, rows := importRows(t, append([]string{"import", "claude-code", "--apply"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "updated" {
		t.Errorf("post-bootstrap native edit: exit %d row %v, want a fast-forward", code, rows["claude-lesson"])
	}
}

// --keep <name> settles a conflict in canonical's favor: canonical is kept as
// is, and the native's current content becomes the base, so the memory reads as
// canonical_ahead from then on, until the native moves again.
func TestImportKeepSettlesAConflictForCanonical(t *testing.T) {
	canon, claudeMem, _, args := setupTwoHarnesses(t)
	seedImport(t, args)
	file := filepath.Join(canon, "claude-lesson.md")
	editCanonicalBody(t, file)
	editNative(t, claudeMem, "lesson-a.md", "claude-lesson", "native edit")
	if code, rows := importRows(t, append([]string{"import", "claude-code"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "conflict" {
		t.Fatalf("fixture: want a two-sided conflict, got %v", rows["claude-lesson"])
	}
	before, _ := os.ReadFile(file)

	if code, rows := importRows(t, append([]string{"import", "claude-code", "--keep", "claude-lesson"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "canonical_ahead" {
		t.Errorf("dry-run --keep: exit %d row %v, want canonical_ahead", code, rows["claude-lesson"])
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("a dry-run must not write")
	}
	if code, rows := importRows(t, append([]string{"import", "claude-code", "--apply", "--keep", "claude-lesson"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "canonical_ahead" {
		t.Errorf("--keep --apply: exit %d row %v, want exit 0 and canonical_ahead", code, rows["claude-lesson"])
	}
	after, _ := os.ReadFile(file)
	if !strings.Contains(string(after), "curated addition") || strings.Contains(string(after), "native edit") {
		t.Errorf("--keep must keep canonical's content:\n%s", after)
	}
	if code, rows := importRows(t, append([]string{"import", "claude-code", "--apply"}, args...)...); code != exitOK || rows["claude-lesson"]["outcome"] != "canonical_ahead" {
		t.Errorf("after --keep: exit %d row %v, want canonical_ahead", code, rows["claude-lesson"])
	}
	for _, argv := range [][]string{
		{"import", "claude-code", "--keep", "no-such-memory"},
		{"import", "claude-code", "--keep", "claude-lesson", "--refresh", "claude-lesson"},
	} {
		if code, _ := importRows(t, append(argv, args...)...); code != exitUsage {
			t.Errorf("%v: exit %d, want %d", argv, code, exitUsage)
		}
	}
}

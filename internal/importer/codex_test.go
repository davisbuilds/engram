package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/render"
)

// A lesson about Codex's memory format can quote a Task Group heading inside a
// code fence; that is content, not a new group.
func TestSplitTaskGroupsIgnoresHeadingsInsideFences(t *testing.T) {
	md := "# Task Group: Real Lesson\n\nintro\n\n```md\n# Task Group: Example Only\nnot a group\n```\n\nafter the fence\n"
	groups := splitTaskGroups(md)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	for _, want := range []string{"# Task Group: Example Only", "after the fence"} {
		if !strings.Contains(groups[0].body, want) {
			t.Errorf("body lost %q:\n%s", want, groups[0].body)
		}
	}
}

// CRLF is cosmetic; it must not bake a trailing \r into canonical titles or
// bodies, where a later LF source would diff against it forever.
func TestSplitTaskGroupsNormalizesCRLF(t *testing.T) {
	groups := splitTaskGroups("# Task Group: Title\r\n\r\nline one\r\nline two\r\n")
	if len(groups) != 1 {
		t.Fatalf("got %d groups", len(groups))
	}
	if strings.Contains(groups[0].title+groups[0].body, "\r") {
		t.Errorf("carriage return survived: %q / %q", groups[0].title, groups[0].body)
	}
}

// A longer fence can quote a shorter one; only a closing run of the same
// character, at least as long and alone on its line, ends it.
func TestSplitTaskGroupsHonorsLongerFences(t *testing.T) {
	md := "# Task Group: Real Lesson\n\n````md\n```\n# Task Group: Quoted Example\n```\n````\n\nafter\n"
	if groups := splitTaskGroups(md); len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	info := "# Task Group: Real Lesson\n\n```md\n```go not a close\n# Task Group: Still Quoted\n```\n"
	if groups := splitTaskGroups(info); len(groups) != 1 {
		t.Fatalf("a fence line with an info string closed the fence: %d groups", len(groups))
	}
}

// echoGroup is a consolidated Task Group shaped like the consolidator's real
// output: genuine bullets with engram notes folded in as labeled bullets, one of
// them wrapped onto an indented continuation line.
const echoGroup = "# Task Group: Workspace / public hygiene cleanup\n\n" +
	"## User preferences\n\n" +
	"- verify repository visibility before publishing [Task 1]\n\n" +
	"## Failures and how to do differently\n\n" +
	"- do not rewrite published history without escalation [Task 1]\n" +
	"- Curated Engram update (2026-09-27): stage explicit paths, never `git add -A`.\n" +
	"- Curated Engram update (2026-10-02): run `engram reconcile`, never plain sync;\n" +
	"  all tracked cwds reconciled clean.\n" +
	"- inspect each repository separately [Task 1]\n"

func importOne(t *testing.T, md string) Result {
	t.Helper()
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportCodex(path)
	if err != nil {
		t.Fatalf("ImportCodex: %v", err)
	}
	return res
}

// TestImportCodexStripsEchoBullets pins the bullet-level loop guard: an engram
// note the consolidator folded into a genuine group is not re-imported as part
// of that group, while the group's own bullets survive.
func TestImportCodexStripsEchoBullets(t *testing.T) {
	res := importOne(t, echoGroup)
	if len(res.Memories) != 1 {
		t.Fatalf("got %d memories, want the genuine group", len(res.Memories))
	}
	body := res.Memories[0].Body
	for _, echo := range []string{"Curated Engram update", "stage explicit paths", "all tracked cwds"} {
		if strings.Contains(body, echo) {
			t.Errorf("echo %q survived import:\n%s", echo, body)
		}
	}
	for _, own := range []string{"verify repository visibility", "do not rewrite published history", "inspect each repository separately"} {
		if !strings.Contains(body, own) {
			t.Errorf("genuine bullet %q was removed:\n%s", own, body)
		}
	}
}

// TestImportCodexEchoDoesNotMoveImportHash pins why stripping matters: when the
// consolidator re-folds a changed note, the group's own content has not moved,
// so its merge base must not move either.
func TestImportCodexEchoDoesNotMoveImportHash(t *testing.T) {
	changed := strings.Replace(echoGroup, "all tracked cwds reconciled clean", "a newer engram release", 1)
	a, b := importOne(t, echoGroup), importOne(t, changed)
	if a.Memories[0].Provenance.ImportHash != b.Memories[0].Provenance.ImportHash {
		t.Error("a changed echo bullet moved the group's import hash")
	}
}

// TestImportCodexSkipsAllEchoGroup pins the group case: a group holding nothing
// but echo bullets under headings is engram's output, so it is skipped and
// accounted for, while a genuine title-only group still imports.
func TestImportCodexSkipsAllEchoGroup(t *testing.T) {
	md := "# Task Group: Folded notes\n\n## Reusable knowledge\n\n" +
		"* curated engram update (2026-09-28): a lesson engram already holds\n" +
		"# Task Group: Title only\n"
	res := importOne(t, md)
	if len(res.Skipped) != 1 || res.Skipped[0] != "Folded notes" {
		t.Errorf("skipped = %v, want the all-echo group", res.Skipped)
	}
	if len(res.Memories) != 1 || res.Memories[0].Name != "title-only" {
		t.Errorf("memories = %d, want only the title-only group", len(res.Memories))
	}
}

// TestWithoutEchoesKeepsFencedBullet pins fences as content: a session that
// quotes the consolidator's output inside a code block keeps the quote.
func TestWithoutEchoesKeepsFencedBullet(t *testing.T) {
	body := "- own\n```md\n- Curated Engram update (2026-09-27): quoted example\n```\n- Curated Engram update (2026-09-27): real echo\n"
	got := withoutEchoes(body)
	if !strings.Contains(got, "quoted example") {
		t.Errorf("a fenced bullet was stripped:\n%s", got)
	}
	if strings.Contains(got, "real echo") {
		t.Errorf("an unfenced echo survived:\n%s", got)
	}
}

// TestCodexInstructionsRequestTheEchoLabel pins the contract between the two
// sides: the label engram asks the consolidator to write is the one import
// strips. A reworded instruction that drifts from echoPrefix fails here.
func TestCodexInstructionsRequestTheEchoLabel(t *testing.T) {
	for _, ln := range strings.Split(render.CodexInstructions, "\n") {
		if isEchoBullet(strings.TrimSpace(ln)) {
			return
		}
	}
	t.Errorf("CodexInstructions shows no bullet that import recognizes as an echo:\n%s", render.CodexInstructions)
}

// TestWithoutEchoesNeedsTheFullLabel pins the label shape: only the dated label
// marks a bullet as engram's, so genuine bullets that merely start with the same
// words survive (Codex review, PR #39).
func TestWithoutEchoesNeedsTheFullLabel(t *testing.T) {
	genuine := []string{
		"- Curated Engram updates require review",
		"- Curated Engram update: no date given",
		"- Curated Engram update (soon): not a date",
	}
	body := strings.Join(genuine, "\n") + "\n- Curated Engram update (2026-09-27): a real echo\n"
	got := withoutEchoes(body)
	for _, g := range genuine {
		if !strings.Contains(got, g) {
			t.Errorf("genuine bullet %q was stripped", g)
		}
	}
	if strings.Contains(got, "a real echo") {
		t.Error("the dated echo survived")
	}
}

package importer

import (
	"strings"
	"testing"
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

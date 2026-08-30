package marker

import "testing"

func TestClaudeIndexMarkerRoundTrip(t *testing.T) {
	for _, name := range []string{"rg-replace-flag-gotcha", "a", "x-y-z-2"} {
		line := "- [" + name + "](" + name + ".md) — a hook " + ClaudeIndexMarker(name)
		got, ok := ClaudeIndexName(line)
		if !ok {
			t.Errorf("ClaudeIndexName(%q) reported not-owned; want owned", line)
			continue
		}
		if got != name {
			t.Errorf("ClaudeIndexName(%q) = %q; want %q", line, got, name)
		}
	}
}

func TestClaudeIndexNameRejectsUnmarked(t *testing.T) {
	if got, ok := ClaudeIndexName("- [foo](foo.md) — a hand-authored line"); ok {
		t.Errorf("unmarked line recognized as engram-owned (name=%q)", got)
	}
}

func TestCodexNoteMarkerRoundTrip(t *testing.T) {
	cases := []struct{ name, scope string }{
		{"rg-replace-flag-gotcha", "global"},
		{"local-stack-e2e", "project:example-app"},
	}
	for _, c := range cases {
		note := CodexNoteMarker(c.name, c.scope) + "\n\n# " + c.name + "\nbody\n"
		n, sc, ok := CodexNoteName(note)
		if !ok {
			t.Errorf("CodexNoteName(%q) reported not-owned; want owned", note)
			continue
		}
		if n != c.name || sc != c.scope {
			t.Errorf("CodexNoteName round-trip = (%q,%q); want (%q,%q)", n, sc, c.name, c.scope)
		}
	}
}

func TestCodexNoteNameRejectsForeign(t *testing.T) {
	if _, _, ok := CodexNoteName("<!-- some other extension note -->\n# x\n"); ok {
		t.Error("foreign note recognized as engram-owned")
	}
}

func TestClaudeIndexHeaderIsDetectableAndNotAnEntry(t *testing.T) {
	if !IsClaudeIndexHeader(ClaudeIndexHeader) {
		t.Error("ClaudeIndexHeader not recognized by IsClaudeIndexHeader")
	}
	if !IsClaudeIndexHeader("  " + ClaudeIndexHeader) {
		t.Error("leading whitespace should not defeat header detection")
	}
	// The header must never be mistaken for a managed index entry, or a re-sync
	// would try to update it as one.
	if _, ok := ClaudeIndexName(ClaudeIndexHeader); ok {
		t.Error("header matched the index-entry pattern")
	}
	// A real entry line and a foreign line are not headers.
	if IsClaudeIndexHeader(ClaudeIndexMarker("x")) {
		t.Error("an index entry marker was misread as a header")
	}
	if IsClaudeIndexHeader("- [hand](hand.md) — a human wrote this") {
		t.Error("a foreign line was misread as a header")
	}
}

func TestMarkersAreDistinct(t *testing.T) {
	// A Claude index marker must not be misread as a Codex note marker or vice
	// versa; the two harnesses' ownership checks must not cross-match.
	if _, _, ok := CodexNoteName(ClaudeIndexMarker("x")); ok {
		t.Error("Claude index marker matched the Codex note pattern")
	}
	if _, ok := ClaudeIndexName(CodexNoteMarker("x", "global")); ok {
		t.Error("Codex note marker matched the Claude index pattern")
	}
}

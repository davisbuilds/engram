package scope

import (
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

func mem(name, scope string, at schema.AppliesTo) *schema.CanonicalMemory {
	return &schema.CanonicalMemory{
		Name: name, Description: "d", Type: schema.TypeLesson, Scope: scope, AppliesTo: at,
	}
}

func names(ms []*schema.CanonicalMemory) map[string]bool {
	s := map[string]bool{}
	for _, m := range ms {
		s[m.Name] = true
	}
	return s
}

func TestGlobalMatchesAnyCwd(t *testing.T) {
	got := RelevantFor([]*schema.CanonicalMemory{mem("g", "global", schema.AppliesTo{})},
		"/anywhere/at/all", "claude", "host-a")
	if !names(got)["g"] {
		t.Error("global memory should match any cwd")
	}
}

func TestProjectScopedMatchesOnlyInRepoTree(t *testing.T) {
	m := mem("p", "project:acme-web", schema.AppliesTo{})
	in := RelevantFor([]*schema.CanonicalMemory{m}, "/home/u/acme-web/src", "claude", "host-a")
	if !names(in)["p"] {
		t.Error("project memory should match inside its repo tree")
	}
	out := RelevantFor([]*schema.CanonicalMemory{m}, "/home/u/other-repo", "claude", "host-a")
	if names(out)["p"] {
		t.Error("project memory should not match outside its repo tree")
	}
}

func TestHostAxisFailsClosedForUnknownHost(t *testing.T) {
	hostScoped := mem("h", "global", schema.AppliesTo{Hosts: []string{"host-a"}})
	agnostic := mem("a", "global", schema.AppliesTo{})
	all := []*schema.CanonicalMemory{hostScoped, agnostic}

	// Known, matching host: both render.
	known := names(RelevantFor(all, "/x", "claude", "host-a"))
	if !known["h"] || !known["a"] {
		t.Errorf("known host should get both; got %v", known)
	}
	// Known, non-matching host: only the agnostic one.
	other := names(RelevantFor(all, "/x", "claude", "host-b"))
	if other["h"] || !other["a"] {
		t.Errorf("non-matching host should exclude host-scoped; got %v", other)
	}
	// Unknown host (""): host-scoped fails closed, agnostic still renders.
	unknown := names(RelevantFor(all, "/x", "claude", ""))
	if unknown["h"] {
		t.Error("unknown host must not receive a host-scoped memory (fail-closed)")
	}
	if !unknown["a"] {
		t.Error("unknown host should still receive host-agnostic memories")
	}
}

func TestAgentFilter(t *testing.T) {
	m := mem("only-codex", "global", schema.AppliesTo{Agents: []string{"codex"}})
	if names(RelevantFor([]*schema.CanonicalMemory{m}, "/x", "claude", "host-a"))["only-codex"] {
		t.Error("agent-filtered memory should not match a different agent")
	}
	if !names(RelevantFor([]*schema.CanonicalMemory{m}, "/x", "codex", "host-a"))["only-codex"] {
		t.Error("agent-filtered memory should match its agent")
	}
}

func TestCwdGlobPrefixMatch(t *testing.T) {
	m := mem("w", "global", schema.AppliesTo{Cwd: []string{"/work/**"}})
	if !names(RelevantFor([]*schema.CanonicalMemory{m}, "/work/proj/x", "claude", "host-a"))["w"] {
		t.Error("cwd under /work/** should match")
	}
	if names(RelevantFor([]*schema.CanonicalMemory{m}, "/elsewhere", "claude", "host-a"))["w"] {
		t.Error("cwd outside /work/** should not match")
	}
}

// Every absolute path splits to a leading empty segment, so an empty repo name
// must never count as a match.
func TestEmptySegmentNeverMatches(t *testing.T) {
	if pathHasSegment("/any/where", "") {
		t.Error("empty segment matched an absolute path")
	}
}

func TestInViewGatesOnRecordedTier(t *testing.T) {
	if InView("project:alpha", nil, "/w/beta", "codex", "") {
		t.Error("a project:alpha render is not in view from beta")
	}
	if !InView("project:alpha", nil, "/w/alpha/sub", "codex", "") {
		t.Error("a project:alpha render is in view from within alpha")
	}
	if !InView("global", nil, "/w/beta", "codex", "") {
		t.Error("a global render whose canonical is gone is in view anywhere")
	}
}

func TestInViewHonorsCanonicalCwdGlobs(t *testing.T) {
	m := mem("pinned", "global", schema.AppliesTo{Cwd: []string{"/w/alpha/**"}})
	if InView("global", m, "/w/beta", "codex", "") {
		t.Error("a render whose canonical is pinned to alpha is not in view from beta")
	}
	if !InView("global", m, "/w/alpha/sub", "codex", "") {
		t.Error("a render whose canonical is pinned to alpha is in view from alpha")
	}
}

func TestInViewHonorsCanonicalAgentAndHostAxes(t *testing.T) {
	forClaude := mem("for-claude", "global", schema.AppliesTo{Agents: []string{"claude"}})
	if InView("global", forClaude, "/w", "codex", "") {
		t.Error("a render whose canonical is for claude only is not in view from a codex session")
	}
	if !InView("global", forClaude, "/w", "claude", "") {
		t.Error("a render whose canonical is for claude is in view from a claude session")
	}
	onA := mem("on-a", "global", schema.AppliesTo{Hosts: []string{"host-a"}})
	if InView("global", onA, "/w", "codex", "host-b") {
		t.Error("a render whose canonical is pinned to host-a is not in view from host-b")
	}
	if !InView("global", onA, "/w", "codex", "host-a") {
		t.Error("a render whose canonical is pinned to host-a is in view from host-a")
	}
}

// TestInViewHonorsCanonicalTier pins a narrowed memory: a note rendered while
// its canonical was global still records "global", but once the canonical is
// re-tiered to project:alpha the note belongs to alpha's sessions. A run from
// beta must leave it alone rather than remove it as stale.
func TestInViewHonorsCanonicalTier(t *testing.T) {
	m := mem("narrowed", "project:alpha", schema.AppliesTo{})
	if InView("global", m, "/w/beta", "codex", "") {
		t.Error("a global-recorded render whose canonical is now project:alpha is not in view from beta")
	}
	if !InView("global", m, "/w/alpha/sub", "codex", "") {
		t.Error("a global-recorded render whose canonical is now project:alpha is in view from alpha")
	}
}

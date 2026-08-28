// Package importer reverse-syncs a harness's native memory back into canonical
// form. Import is a one-shot migration on-ramp, not the steady-state path, and
// every importer is loop-guarded so engram never re-imports its own rendered
// output back into canonical.
package importer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// Result is what an importer produced: the canonical memories it mapped, the
// names/titles it skipped as engram-origin (the loop guard), the sources it could
// not import (with reasons), and — for Codex — whether the source looks stale.
// Every candidate source is accounted for in exactly one of Memories, Skipped, or
// Dropped: import never loses a file silently.
type Result struct {
	Memories []*schema.CanonicalMemory
	Skipped  []string
	Dropped  []Dropped
	// StaleWarning is set when the Codex MEMORY.md being imported is older than
	// 30 days, i.e. the consolidator may be stalled and the source may lag reality.
	StaleWarning bool
}

// Dropped records a native source (a Claude file name, or a Codex Task Group
// title) that could not be imported, with why — so nothing vanishes unreported.
type Dropped struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// projectScopeFromRepo maps a directory path to a project scope, but only when
// the path resolves to a real git repository — the single principled signal that
// separates a project from a multi-project container (a workspace parent like
// ~/Dev is not a repo and stays global). It walks up from path for a .git marker,
// stopping at the home directory so a dotfiles repo at ~ never captures unrelated
// work, and returns project:<repo base>. A path that is not under a repo — or
// does not exist on this machine — yields "global". Only the repo's base name
// enters the scope; the full, machine-specific path never does. The base name is
// used verbatim (not slugified) because tier matching compares it against a
// literal path segment of the session cwd.
func projectScopeFromRepo(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "global"
	}
	// Resolve a relative cwd (e.g. "." or "src/pkg") to an absolute, cleaned path
	// so the walk finds the real repo root and the scope carries its base name,
	// not "." — otherwise "--cwd ." would yield "project:." or a wrong "global".
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = strings.TrimRight(path, string(filepath.Separator))
	if path == "" {
		return "global"
	}
	home, _ := os.UserHomeDir()
	home = strings.TrimRight(home, string(filepath.Separator))
	d := path
	for {
		if d == "" || d == string(filepath.Separator) || (home != "" && d == home) {
			return "global"
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return "project:" + filepath.Base(d)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "global"
		}
		d = parent
	}
}

// deriveCodexScope reads a Task Group's `applies_to: cwd=<path>` line and maps
// the path to a project scope by the same repo rule as Claude imports: a real
// git repo → project:<base>, anything else → global. A group with no such line
// stays global.
func deriveCodexScope(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "applies_to:") {
			continue
		}
		idx := strings.Index(ln, "cwd=")
		if idx < 0 {
			continue
		}
		rest := ln[idx+len("cwd="):]
		if end := strings.IndexAny(rest, "; \t"); end >= 0 {
			rest = rest[:end]
		}
		return projectScopeFromRepo(rest)
	}
	return "global"
}

// pathFromClaudeSlug reconstructs the absolute project path a Claude project slug
// encodes. The slug replaces every non-alphanumeric run with '-', so '/' and a
// literal '-' are indistinguishable in the slug; this resolves that ambiguity
// against the live filesystem, returning the real path (and true) when one exists,
// or ("", false) when no reconstruction does — e.g. the project was deleted. The
// caller maps a false result to global scope, exactly as a non-existent cwd would.
func pathFromClaudeSlug(slug string) (string, bool) {
	tokens := splitSlugTokens(slug)
	if len(tokens) == 0 {
		return "", false
	}
	return resolveTokens(string(filepath.Separator), tokens)
}

// splitSlugTokens splits a slug into its '-'-separated tokens, dropping the empty
// tokens that a leading '-' or any '-' run produces.
func splitSlugTokens(slug string) []string {
	var out []string
	for _, t := range strings.Split(slug, "-") {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// resolveTokens walks the filesystem from base, consuming slug tokens into real
// directory components. Because a component may itself contain '-' (e.g. a dir
// named "my-user" or "web-app-ios"), it tries the longest run of tokens that
// names an existing subdirectory first and backtracks on a dead end, so the
// greedy choice never traps a resolvable path. It returns the deepest resolved
// path when all tokens are consumed, or false when no split matches on disk.
func resolveTokens(base string, tokens []string) (string, bool) {
	if len(tokens) == 0 {
		return base, true
	}
	for k := len(tokens); k >= 1; k-- {
		candidate := filepath.Join(base, strings.Join(tokens[:k], "-"))
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			if got, ok := resolveTokens(candidate, tokens[k:]); ok {
				return got, true
			}
		}
	}
	return "", false
}

// opensFrontmatterFence reports whether the file's first line is a `---` fence
// (tolerating a trailing CR), i.e. it intends to carry frontmatter. It lets an
// importer tell a genuinely frontmatter-less file (recoverable from its filename)
// apart from one whose frontmatter is malformed — an opening fence with no
// parseable close — which must be reported rather than force-imported.
func opensFrontmatterFence(data []byte) bool {
	s := string(data)
	first := s
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		first = s[:nl]
	}
	return strings.TrimRight(first, "\r") == "---"
}

// frontmatterAndBody splits a canonical/native memory file into its YAML
// frontmatter bytes and its markdown body.
func frontmatterAndBody(data []byte) (front []byte, body string, ok bool) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", false
	}
	rest := s[len("---\n"):]
	i := strings.Index(rest, "\n---\n")
	if i < 0 {
		return nil, "", false
	}
	return []byte(rest[:i]), rest[i+len("\n---\n"):], true
}

// slugify converts a free-text title into a valid kebab-case memory name.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}

// taskGroup is one "# Task Group:" section of a Codex MEMORY.md.
type taskGroup struct {
	title string
	body  string
}

// splitTaskGroups breaks a Codex MEMORY.md into its Task Group sections.
func splitTaskGroups(s string) []taskGroup {
	const hdr = "# Task Group:"
	var groups []taskGroup
	var cur *taskGroup
	var buf []string
	flush := func() {
		if cur != nil {
			cur.body = strings.Join(buf, "\n")
			groups = append(groups, *cur)
		}
		buf = nil
	}
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(ln, hdr) {
			flush()
			cur = &taskGroup{title: strings.TrimSpace(strings.TrimPrefix(ln, hdr))}
			continue
		}
		if cur != nil {
			buf = append(buf, ln)
		}
	}
	flush()
	return groups
}

// isEngramOrigin reports whether a consolidated Task Group is engram's own output
// rebounding through Codex's consolidator. The guard rests on signals engram
// controls — its note marker text and its extension path — not on any single
// consolidator-preserved field, so it holds even if the consolidator drops
// rollout metadata (spec SC-06). A content-hash fallback against current
// canonical is the intended backstop once real consolidated fixtures exist.
func isEngramOrigin(body string) bool {
	return strings.Contains(body, "extension=engram") || strings.Contains(body, "extensions/engram/")
}

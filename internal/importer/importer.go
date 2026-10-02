// Package importer reverse-syncs a harness's native memory back into canonical
// form. Import is a one-shot migration on-ramp, not the steady-state path, and
// every importer is loop-guarded so engram never re-imports its own rendered
// output back into canonical.
package importer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/gitroot"
	"github.com/davisbuilds/engram/internal/schema"
	slug2 "github.com/davisbuilds/engram/internal/slug"
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
	// ScopeAuthoritative reports whether the derived scope may revise an existing
	// memory's scope. It is true only for a live single import, whose cwd is the
	// real session directory and whose repo probe is ground truth; a full-tree
	// sweep or a Codex import derives scope from reconstructed/recorded paths that
	// may not resolve on this machine, so those are provisional (false) and must
	// not silently re-scope an existing memory. See cli.decideImportScope.
	ScopeAuthoritative bool
}

// withHash records m's native hash as its import_hash, the merge base a later
// import compares against (see store.Plan).
func withHash(m *schema.CanonicalMemory) *schema.CanonicalMemory {
	m.Provenance.ImportHash = schema.NativeHash(m)
	return m
}

// importSource names a Claude native precisely: its project slug and file, so
// two projects' same-named files are different lineages.
func importSource(memoryDir, fname string) string {
	return filepath.Base(filepath.Dir(memoryDir)) + "/" + fname
}

// clearAmbiguous drops the import hash from every candidate whose name another
// candidate in the batch shares: two natives for one canonical memory are two
// lineages, and neither may move its merge base.
func clearAmbiguous(ms []*schema.CanonicalMemory) []*schema.CanonicalMemory {
	count := map[string]int{}
	for _, m := range ms {
		count[m.Name]++
	}
	for _, m := range ms {
		if count[m.Name] > 1 {
			m.Provenance.ImportHash = ""
		}
	}
	return ms
}

// Dropped records a native source (a Claude file name, or a Codex Task Group
// title) that could not be imported, with why — so nothing vanishes unreported.
type Dropped struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// cwdIsLiveDir reports whether path names an existing directory on this machine —
// the condition under which projectScopeFromRepo's .git probe is ground truth
// rather than a guess. A single import may revise scope only from a live cwd; a
// path pointing at a deleted or unmounted project is provisional, so it cannot
// force-widen an existing memory's scope.
func cwdIsLiveDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
	// A linked worktree resolves to the repository it was added from. A repo at
	// (or above) the home directory is not a project: it would claim every
	// path beneath it.
	root, ok := gitroot.Main(path)
	if !ok {
		return "global"
	}
	home, _ := os.UserHomeDir()
	home = strings.TrimRight(home, string(filepath.Separator))
	if root == string(filepath.Separator) || home != "" && (root == home || strings.HasPrefix(home, root+string(filepath.Separator))) {
		return "global"
	}
	return "project:" + filepath.Base(root)
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
// encodes. The slug maps every non-alphanumeric character — '/', but also '-',
// '.', '_', spaces, … — to '-', so it is lossy and not invertible by string
// surgery. This resolves it against the live filesystem: it walks real
// directories and matches each candidate by re-encoding its name with the same
// rule (slug.ForCwd). It returns the path (and true) only when *exactly one*
// reconstruction exists; zero matches (e.g. a deleted project) or two-or-more
// (an ambiguous slug like "-a-b" for both "/a-b" and "/a/b") both yield false, so
// the caller falls back to global rather than guess a wrong project.
func pathFromClaudeSlug(slug string) (string, bool) {
	matches := resolveSlugMatches(string(filepath.Separator), slug)
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// resolveSlugMatches returns every real path under base whose Claude encoding is
// slug. Each path component in the slug is introduced by the '-' that encodes its
// leading separator; at each level it re-encodes every real child directory name
// with slug.ForCwd and descends into those whose encoding is a whole leading
// component of the remaining slug. Collecting *all* matches (not the first) is
// what lets the caller detect ambiguity instead of silently narrowing to one repo.
func resolveSlugMatches(base, slug string) []string {
	if slug == "" {
		return []string{base}
	}
	if slug[0] != '-' {
		return nil // a component must be introduced by its encoded separator
	}
	rest := slug[1:]
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		enc := slug2.ForCwd(e.Name())
		if !strings.HasPrefix(rest, enc) {
			continue
		}
		// enc must be a whole component: the slug ends here, or the next character
		// is the '-' that encodes the following separator.
		tail := rest[len(enc):]
		if tail != "" && tail[0] != '-' {
			continue
		}
		out = append(out, resolveSlugMatches(filepath.Join(base, e.Name()), tail)...)
	}
	return out
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

// Slugify converts a free-text title into a valid kebab-case memory name. It is
// the single source of truth for how native names are normalized on import, so
// migrate reuses it to match a hand-authored file back to its canonical name.
func Slugify(s string) string {
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
		// Hard-cap at 60 chars (trailing dash trimmed). The cut is deliberately
		// left mid-token when it lands there: the slug is an identity, and the
		// trailing partial token is exactly what keeps two long titles sharing
		// their leading tokens distinct. Trimming back to a token boundary would
		// read cleaner but can collapse both to the same slug, and on import the
		// second then loses to a store.Save name conflict and never reaches
		// canonical. Cosmetics lose to not dropping a memory. See docs/project/BACKLOG.md.
		out = strings.Trim(out[:60], "-")
	}
	return out
}

// taskGroup is one "# Task Group:" section of a Codex MEMORY.md.
type taskGroup struct {
	title string
	body  string
}

// splitTaskGroups breaks a Codex MEMORY.md into its Task Group sections. CRLF is
// normalized first, and headings inside code fences are ignored.
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
	fence := "" // the open fence's full marker run (e.g. ```` ````), or "" outside one
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		// A heading quoted inside a code fence is content, not a new group.
		fence = nextFence(fence, strings.TrimSpace(ln))
		if fence == "" && strings.HasPrefix(ln, hdr) {
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
// rollout metadata (spec SC-06). In observed output the consolidator instead
// folds notes into genuine groups as labeled bullets, which withoutEchoes strips.
func isEngramOrigin(body string) bool {
	return strings.Contains(body, "extension=engram") || strings.Contains(body, "extensions/engram/")
}

// echoPrefix is how Codex's consolidator labels a bullet it folded from an
// engram note into a consolidated Task Group: "- Curated Engram update (DATE):".
// CodexInstructions asks for this label, so it is a signal engram requests.
const echoPrefix = "curated engram update"

// withoutEchoes returns a Task Group body with every engram echo bullet removed,
// along with the indented lines that continue it. Code fences are content, so a
// bullet quoted inside one is kept.
func withoutEchoes(body string) string {
	lines := strings.Split(body, "\n")
	kept := lines[:0:0]
	fence, echoIndent := "", -1 // echoIndent: the open echo bullet's indent, or -1
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		indent := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if echoIndent >= 0 {
			if t != "" && indent > echoIndent {
				continue // an indented line continues the echo bullet
			}
			echoIndent = -1
		}
		if fence = nextFence(fence, t); fence == "" && isEchoBullet(t) {
			echoIndent = indent
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// isEchoBullet reports whether a trimmed line is a list item carrying echoPrefix.
func isEchoBullet(t string) bool {
	if !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "* ") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(t[2:])), echoPrefix)
}

// hasContent reports whether a body holds any line besides blanks and headings.
func hasContent(body string) bool {
	for _, ln := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(ln); t != "" && !strings.HasPrefix(t, "#") {
			return true
		}
	}
	return false
}

// nextFence tracks Markdown code fences line by line: given the open fence (""
// outside one) and a trimmed line, it returns the fence open after that line.
func nextFence(fence, t string) string {
	if fence == "" {
		return fenceRun(t)
	}
	if run := fenceRun(t); run == t && run[0] == fence[0] && len(run) >= len(fence) {
		return ""
	}
	return fence
}

// fenceRun returns the leading run of three or more backticks or tildes that
// opens or closes a Markdown code fence, or "" when t does not start one. A
// fence closes only on a bare run of the same character at least as long.
func fenceRun(t string) string {
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

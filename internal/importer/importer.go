// Package importer reverse-syncs a harness's native memory back into canonical
// form. Import is a one-shot migration on-ramp, not the steady-state path, and
// every importer is loop-guarded so engram never re-imports its own rendered
// output back into canonical.
package importer

import (
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// Result is what an importer produced: the canonical memories it mapped, and the
// names/titles it skipped as engram-origin (the loop guard).
type Result struct {
	Memories []*schema.CanonicalMemory
	Skipped  []string
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

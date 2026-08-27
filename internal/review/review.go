// Package review produces health leads over the canonical memory set — the
// judgment inputs for a headless agent. It never mutates: it flags Jaccard-
// similar names as possible duplicates and over-narrow scopes as promotion
// candidates, and leaves the merge/promote decision to the agent.
package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// nearDupeThreshold is the Jaccard similarity over name tokens above which two
// memory names are flagged as possible duplicates.
const nearDupeThreshold = 0.5

// Finding is one review lead.
type Finding struct {
	Kind      string   `json:"kind"`
	Names     []string `json:"names"`
	Detail    string   `json:"detail"`
	Suggested string   `json:"suggested"`
}

// Analyze returns the review findings for a set of canonical memories. It reads
// only; it never changes a memory.
func Analyze(mems []*schema.CanonicalMemory) []Finding {
	var out []Finding

	for i := 0; i < len(mems); i++ {
		for j := i + 1; j < len(mems); j++ {
			sim := jaccard(tokens(mems[i].Name), tokens(mems[j].Name))
			if sim < nearDupeThreshold {
				continue
			}
			names := []string{mems[i].Name, mems[j].Name}
			sort.Strings(names)
			out = append(out, Finding{
				Kind:      "near-duplicate",
				Names:     names,
				Detail:    fmt.Sprintf("names %q and %q share %.0f%% of their tokens", names[0], names[1], sim*100),
				Suggested: "review whether these two memories should be merged",
			})
		}
	}

	for _, m := range mems {
		if strings.HasPrefix(m.Scope, "project:") && len(m.AppliesTo.Cwd) == 0 {
			out = append(out, Finding{
				Kind:      "promotion-candidate",
				Names:     []string{m.Name},
				Detail:    fmt.Sprintf("%q is project-scoped but sets no cwd constraint; it may belong at a wider scope", m.Name),
				Suggested: "engram share " + m.Name + " --to global",
			})
		}
	}
	return out
}

func tokens(name string) map[string]bool {
	t := map[string]bool{}
	for _, p := range strings.Split(name, "-") {
		if p != "" {
			t[p] = true
		}
	}
	return t
}

func jaccard(a, b map[string]bool) float64 {
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

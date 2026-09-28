package cli

import (
	"regexp"
	"sort"
	"strings"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
)

// threadIDRe matches the rollout session ids a Codex Task Group cites. Two Task
// Groups citing one session are the same work, retitled.
var threadIDRe = regexp.MustCompile(`thread_id=([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// findOrphans reports the canonical memories imported from harness whose native
// source the import res no longer contains. It only reports; retiring or keeping
// an orphan is the operator's call (forget or detach). A source with nothing in
// it (a missing Codex MEMORY.md, a Claude home with no memory) could make every
// memory look orphaned, so detection is skipped with a warning instead.
func findOrphans(canon []*schema.CanonicalMemory, harness string, res importer.Result) ([]map[string]any, string) {
	orphans := []map[string]any{}
	if len(res.Memories)+len(res.Skipped)+len(res.Dropped) == 0 {
		return orphans, harness + ": the native source is empty or missing; orphan detection skipped"
	}
	names, sources := map[string]bool{}, map[string]bool{}
	for _, m := range res.Memories {
		names[m.Name] = true
		sources[m.Provenance.Source] = true
	}
	for _, d := range res.Dropped {
		sources[d.Source] = true
	}
	for _, title := range res.Skipped {
		names[importer.Slugify(title)] = true
	}
	for _, m := range canon {
		if originHarness(m) != harness || names[m.Name] {
			continue
		}
		if harness == config.HarnessClaude && (m.Provenance.Source == "" || sources[m.Provenance.Source]) {
			continue
		}
		orphans = append(orphans, map[string]any{
			"name": m.Name, "origin": m.Provenance.Origin, "successors": successors(m, res.Memories),
		})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i]["name"].(string) < orphans[j]["name"].(string) })
	return orphans, ""
}

// successors lists the current candidates that cite a session the orphan cites.
func successors(orphan *schema.CanonicalMemory, cands []*schema.CanonicalMemory) []string {
	ids := map[string]bool{}
	for _, m := range threadIDRe.FindAllStringSubmatch(orphan.Body, -1) {
		ids[m[1]] = true
	}
	out := []string{}
	if len(ids) == 0 {
		return out
	}
	for _, c := range cands {
		for _, m := range threadIDRe.FindAllStringSubmatch(c.Body, -1) {
			if ids[m[1]] {
				out = append(out, c.Name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// orphanNextSteps offers each orphan's two dispositions: forget it (naming the
// successor when there is exactly one), or detach it to keep it.
func orphanNextSteps(harness string, orphans []map[string]any) []NextStep {
	var next []NextStep
	for _, o := range orphans {
		n := o["name"].(string)
		succ := o["successors"].([]string)
		forget := "engram forget " + n
		reason := n + " is no longer in " + harness + "'s native memory"
		switch len(succ) {
		case 0:
			forget += " --reason 'no longer in " + harness + "' --apply"
		case 1:
			forget += " --successor " + succ[0] + " --reason 'superseded by " + succ[0] + "' --apply"
			reason += "; " + succ[0] + " cites the same sessions"
		default:
			forget += " --reason 'no longer in " + harness + "' --apply"
			reason += "; candidate successors: " + strings.Join(succ, ", ")
		}
		next = append(next,
			NextStep{Reason: reason + " (retire it)", Command: forget},
			NextStep{Reason: "or keep " + n + " as a standalone memory", Command: "engram detach " + n + " --apply"})
	}
	return next
}

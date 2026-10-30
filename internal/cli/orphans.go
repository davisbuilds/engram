package cli

import (
	"regexp"
	"sort"

	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/importer"
	"github.com/davisbuilds/engram/internal/schema"
)

// threadIDRe matches the rollout session ids a Codex Task Group cites. Two Task
// Groups citing one session may cover different lessons from that session.
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
		candidates := candidateSuccessors(m, res.Memories)
		names := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			names = append(names, candidate.Name)
		}
		orphans = append(orphans, map[string]any{
			"name": m.Name, "origin": m.Provenance.Origin, "successors": names,
			"candidate_successors": candidates,
		})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i]["name"].(string) < orphans[j]["name"].(string) })
	return orphans, ""
}

type successorCandidate struct {
	Name           string   `json:"name"`
	MatchBasis     string   `json:"match_basis"`
	SharedSessions []string `json:"shared_sessions"`
	Coverage       string   `json:"coverage"`
}

// candidateSuccessors reports provenance overlap, never semantic replacement.
func candidateSuccessors(orphan *schema.CanonicalMemory, cands []*schema.CanonicalMemory) []successorCandidate {
	ids := map[string]bool{}
	for _, m := range threadIDRe.FindAllStringSubmatch(orphan.Body, -1) {
		ids[m[1]] = true
	}
	out := []successorCandidate{}
	if len(ids) == 0 {
		return out
	}
	for _, c := range cands {
		shared := map[string]bool{}
		for _, m := range threadIDRe.FindAllStringSubmatch(c.Body, -1) {
			if ids[m[1]] {
				shared[m[1]] = true
			}
		}
		if len(shared) > 0 {
			sessions := make([]string, 0, len(shared))
			for id := range shared {
				sessions = append(sessions, id)
			}
			sort.Strings(sessions)
			out = append(out, successorCandidate{c.Name, "shared_session", sessions, "unassessed"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// orphanNextSteps requests inspection before any explicit disposition. A shared
// session does not justify retirement, and absence does not justify detachment.
func orphanNextSteps(harness string, orphans []map[string]any) []NextStep {
	var next []NextStep
	for _, o := range orphans {
		n := o["name"].(string)
		succ := o["successors"].([]string)
		reason := n + " is no longer in " + harness + "'s native memory"
		command := "engram diff --native " + harness + " --name " + n
		if len(succ) == 0 {
			next = append(next, NextStep{Reason: reason + "; inspect retained content before choosing a disposition", Command: command + " --json"})
			continue
		}
		for _, candidate := range succ {
			next = append(next, NextStep{
				Reason:  reason + "; candidate " + candidate + " shares session provenance, not verified lesson coverage; compare content before choosing a disposition",
				Command: command + " --source-name " + candidate + " --json",
			})
		}
	}
	return next
}

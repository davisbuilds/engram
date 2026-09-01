package importer

import (
	"errors"
	"os"
	"time"

	"github.com/davisbuilds/engram/internal/schema"
)

// staleAfter is how old a Codex MEMORY.md may be before import flags it as stale.
const staleAfter = 30 * 24 * time.Hour

// ImportCodex reads a Codex MEMORY.md, splits it into Task Groups, and maps each
// group to one canonical memory (the Task Group is the unit, preserving Codex's
// own consolidator curation). Groups that are engram's own output rebounding
// through the consolidator are skipped as the loop guard. A missing file yields
// an empty result.
func ImportCodex(memoryFile string) (Result, error) {
	// Scope derives from each Task Group's recorded `applies_to: cwd=<path>`, a
	// historical path that may not resolve on this machine, so a Codex import is
	// provisional (ScopeAuthoritative stays false): it preserves an existing
	// memory's scope rather than re-deriving it. See cli.decideImportScope.
	var res Result
	data, err := os.ReadFile(memoryFile)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if fi, statErr := os.Stat(memoryFile); statErr == nil && time.Since(fi.ModTime()) > staleAfter {
		res.StaleWarning = true
	}
	for _, g := range splitTaskGroups(string(data)) {
		if isEngramOrigin(g.body) {
			res.Skipped = append(res.Skipped, g.title)
			continue
		}
		name := Slugify(g.title)
		if name == "" {
			res.Dropped = append(res.Dropped, Dropped{Source: g.title, Reason: "Task Group title yields an empty name"})
			continue
		}
		res.Memories = append(res.Memories, &schema.CanonicalMemory{
			Name:        name,
			Description: g.title,
			Type:        schema.TypeReference,
			Scope:       deriveCodexScope(g.body),
			Body:        g.body,
			Provenance:  schema.Provenance{Origin: "import:codex"},
		})
	}
	return res, nil
}

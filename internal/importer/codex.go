package importer

import (
	"errors"
	"os"

	"github.com/davisbuilds/engram/internal/schema"
)

// ImportCodex reads a Codex MEMORY.md, splits it into Task Groups, and maps each
// group to one canonical memory (the Task Group is the unit, preserving Codex's
// own consolidator curation). Groups that are engram's own output rebounding
// through the consolidator are skipped as the loop guard. A missing file yields
// an empty result.
func ImportCodex(memoryFile string) (Result, error) {
	var res Result
	data, err := os.ReadFile(memoryFile)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	for _, g := range splitTaskGroups(string(data)) {
		if isEngramOrigin(g.body) {
			res.Skipped = append(res.Skipped, g.title)
			continue
		}
		name := slugify(g.title)
		if name == "" {
			continue
		}
		res.Memories = append(res.Memories, &schema.CanonicalMemory{
			Name:        name,
			Description: g.title,
			Type:        schema.TypeReference,
			Scope:       "global",
			Body:        g.body,
			Provenance:  schema.Provenance{Origin: "import:codex"},
		})
	}
	return res, nil
}

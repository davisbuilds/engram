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
// group to one canonical memory. A missing file yields an empty result.
func ImportCodex(memoryFile string) (Result, error) {
	// WHY: Scope derives from each Task Group's recorded `applies_to: cwd=<path>`, a
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
		if hasStandaloneCodexNoteMarker(g.body) {
			res.Skipped = append(res.Skipped, g.title)
			continue
		}
		body := withoutEchoes(g.body)
		if body != g.body && !hasContent(body) {
			res.Skipped = append(res.Skipped, g.title)
			continue
		}
		name := Slugify(g.title)
		if name == "" {
			res.Dropped = append(res.Dropped, Dropped{Source: g.title, Reason: "Task Group title yields an empty name"})
			continue
		}
		res.Memories = append(res.Memories, withHash(&schema.CanonicalMemory{
			Name:        name,
			Description: g.title,
			Type:        schema.TypeReference,
			Scope:       deriveCodexScope(body),
			Body:        body,
			Provenance:  schema.Provenance{Origin: "import:codex"},
		}))
	}
	clearAmbiguous(&res, codexSource)
	return res, nil
}

// codexSource names a Codex candidate by its Task Group title, which import
// keeps verbatim as the description.
func codexSource(m *schema.CanonicalMemory) string { return m.Description }

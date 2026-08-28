package importer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/schema"
)

// claudeNative mirrors Claude Code's native memory frontmatter (type lives under
// metadata; there is no scope field).
type claudeNative struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Type   string `yaml:"type"`
		Origin string `yaml:"origin"`
	} `yaml:"metadata"`
}

// ImportClaude reads Claude Code's per-project memory directory and maps each
// native memory file into canonical form. cwd is the project directory the
// memory dir belongs to; it is used to derive a project scope (repo root →
// project:<repo>, non-repo container → global). Files engram itself rendered
// (origin marker) are skipped as the loop guard; the index and non-markdown
// files are ignored. A missing directory yields an empty result.
func ImportClaude(memoryDir, cwd string) (Result, error) {
	scope := projectScopeFromRepo(cwd)
	var res Result
	entries, err := os.ReadDir(memoryDir)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		fname := e.Name()
		data, err := os.ReadFile(filepath.Join(memoryDir, fname))
		if err != nil {
			return res, err
		}
		fileBase := strings.TrimSuffix(fname, ".md")

		front, body, ok := frontmatterAndBody(data)
		if !ok {
			if opensFrontmatterFence(data) {
				// An opening `---` with no parseable closing fence — truncated, or
				// CRLF/alternate line endings we don't split on. This is malformed
				// frontmatter, not an absence of it: report it rather than force-import
				// the raw document (which would also bypass the origin loop guard).
				res.Dropped = append(res.Dropped, Dropped{Source: fname, Reason: "malformed frontmatter: opening fence without a parseable closing fence (check line endings)"})
				continue
			}
			// Genuinely no frontmatter: recover the memory rather than lose it. The
			// filename supplies the name, the first heading the description, and the
			// whole file the body.
			name := slugify(fileBase)
			if name == "" {
				res.Dropped = append(res.Dropped, Dropped{Source: fname, Reason: "no frontmatter and no usable name from the filename"})
				continue
			}
			res.Memories = append(res.Memories, &schema.CanonicalMemory{
				Name:        name,
				Description: deriveTitle(string(data), fileBase),
				Type:        schema.TypeReference,
				Scope:       scope,
				Body:        string(data),
				Provenance:  schema.Provenance{Origin: "import:claude-code:no-frontmatter"},
			})
			continue
		}
		var n claudeNative
		if err := yaml.Unmarshal(front, &n); err != nil {
			res.Dropped = append(res.Dropped, Dropped{Source: fname, Reason: "unparseable frontmatter: " + err.Error()})
			continue
		}
		if n.Metadata.Origin == marker.Origin {
			res.Skipped = append(res.Skipped, n.Name)
			continue
		}
		// Normalize the native name to kebab-case: real Claude names are free text
		// (sentences, snake_case), which canonical rejects. Fall back to the
		// filename when the frontmatter name is absent or all punctuation.
		name := slugify(n.Name)
		if name == "" {
			name = slugify(fileBase)
		}
		if name == "" {
			res.Dropped = append(res.Dropped, Dropped{Source: fname, Reason: "empty name after normalization"})
			continue
		}
		desc := n.Description
		if desc == "" {
			desc = deriveTitle(body, fileBase)
		}
		res.Memories = append(res.Memories, &schema.CanonicalMemory{
			Name:        name,
			Description: desc,
			Type:        importedType(n.Metadata.Type),
			Scope:       scope,
			Body:        body,
			Provenance:  schema.Provenance{Origin: "import:claude-code"},
		})
	}
	return res, nil
}

// deriveTitle pulls a human description from a memory that has none: the text of
// the first markdown heading, else the first non-empty line, else the filename.
// Canonical requires a non-empty description, so recovery must always yield one.
func deriveTitle(body, fileBase string) string {
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		ln = strings.TrimLeft(ln, "#")
		ln = strings.TrimSpace(ln)
		if ln != "" {
			return ln
		}
	}
	return fileBase
}

// importedType defaults an absent or unknown native type to reference.
func importedType(t string) schema.Type {
	if t == "" {
		return schema.TypeReference
	}
	return schema.Type(t)
}

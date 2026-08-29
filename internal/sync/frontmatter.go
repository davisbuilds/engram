package sync

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/marker"
	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

// claudeContent produces the engram-owned file content for memory m, preserving
// any frontmatter keys the existing on-disk file already carries that engram does
// not manage (e.g. Claude Code's own node_type / originSessionId). engram sets
// only its managed fields — name, description, metadata.type, metadata.origin —
// and leaves every other key and its order untouched. When existing has no
// frontmatter (a fresh create, or a frontmatter-less native file), it falls back
// to the pure renderer's schema-only output. The renderer stays pure; this merge
// lives in sync/migrate because only they see what is already on disk.
//
// Preserving keys here is what keeps sync idempotent after a migrate adoption:
// re-deriving an owned file from itself is a no-op, so the extra keys survive
// every subsequent sync instead of being stripped on the next render.
func claudeContent(existing []byte, m *schema.CanonicalMemory) ([]byte, error) {
	front := frontmatterBytes(existing)
	if front == nil {
		rr, err := render.ClaudeRenderer{}.Render(m)
		if err != nil {
			return nil, err
		}
		return rr.Content, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		// Existing frontmatter is unparseable as a mapping; do not risk mangling
		// it — fall back to the schema-only render.
		rr, rerr := render.ClaudeRenderer{}.Render(m)
		if rerr != nil {
			return nil, rerr
		}
		return rr.Content, nil
	}
	root := doc.Content[0]
	upsertScalar(root, "name", m.Name)
	upsertScalar(root, "description", m.Description)
	meta := childMapping(root, "metadata")
	upsertScalar(meta, "type", string(m.Type))
	upsertScalar(meta, "origin", marker.Origin)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("marshal merged frontmatter: %w", err)
	}
	return []byte("---\n" + string(out) + "---\n" + m.Body), nil
}

// upsertScalar sets key to a scalar value in a mapping node, replacing the value
// in place (preserving key position) or appending the pair when absent.
func upsertScalar(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Value: value}
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

// childMapping returns the mapping node stored under key, creating an empty one
// (and appending it, preserving other keys) when the key is absent or not a map.
func childMapping(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key && mapping.Content[i+1].Kind == yaml.MappingNode {
			return mapping.Content[i+1]
		}
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		child,
	)
	return child
}

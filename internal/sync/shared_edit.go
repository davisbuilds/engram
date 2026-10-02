package sync

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/davisbuilds/engram/internal/schema"
)

// SharedEdit is a stamped shared render whose content no longer matches the
// stamp: someone edited it in place since engram wrote it.
type SharedEdit struct {
	Name string
	Path string
	// Base is the stamp: the NativeHash of the memory engram rendered.
	Base        string
	Description string
	Type        schema.Type
	Body        string
}

// Hash is the NativeHash of the edited content.
func (e SharedEdit) Hash() string {
	return schema.NativeHash(&schema.CanonicalMemory{Description: e.Description, Type: e.Type, Body: e.Body})
}

// ScanSharedEdits lists the engram-owned, stamped renders in dir that were
// edited since engram wrote them. An unstamped render has no known base and is
// never reported.
func ScanSharedEdits(dir string) ([]SharedEdit, error) {
	owned, _, err := scanMemoryDir(dir)
	if err != nil {
		return nil, err
	}
	var edits []SharedEdit
	for name, f := range owned {
		if e, ok := editedRender(f.content); ok {
			e.Name, e.Path = name, f.path
			edits = append(edits, e)
		}
	}
	return edits, nil
}

// baseKey is the metadata key holding a stamped render's base.
const baseKey = "engram_base"

// editedRender parses a stamped render and reports whether its content differs
// from its stamp.
func editedRender(content []byte) (SharedEdit, bool) {
	front, body, ok := splitRender(content)
	if !ok {
		return SharedEdit{}, false
	}
	var fm struct {
		Description string `yaml:"description"`
		Metadata    struct {
			Type string `yaml:"type"`
			Base string `yaml:"engram_base"`
		} `yaml:"metadata"`
	}
	if yaml.Unmarshal(front, &fm) != nil || fm.Metadata.Base == "" {
		return SharedEdit{}, false
	}
	e := SharedEdit{Base: fm.Metadata.Base, Description: fm.Description, Type: schema.Type(fm.Metadata.Type), Body: body}
	return e, e.Hash() != e.Base
}

// splitRender returns a render's frontmatter and body, after the same
// line-ending and BOM normalization frontmatterBytes applies.
func splitRender(content []byte) ([]byte, string, bool) {
	s := strings.ReplaceAll(strings.TrimPrefix(string(content), "\ufeff"), "\r\n", "\n")
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

// stamp records base under metadata.engram_base in a render's frontmatter.
func stamp(content []byte, base string) ([]byte, error) {
	front, body, ok := splitRender(content)
	if !ok {
		return nil, fmt.Errorf("render has no frontmatter to stamp")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("render frontmatter is not a mapping")
	}
	upsertScalar(childMapping(doc.Content[0], "metadata"), baseKey, base)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("marshal stamped frontmatter: %w", err)
	}
	return []byte("---\n" + string(out) + "---\n" + body), nil
}

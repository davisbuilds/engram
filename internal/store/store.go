// Package store persists canonical memories. It is the write-side counterpart of
// discover: it saves a memory into the canonical root while protecting a
// hand-edited canonical file from being silently overwritten (SC-13).
package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
)

// Outcome describes what a Save did.
type Outcome string

const (
	Created   Outcome = "created"
	Updated   Outcome = "updated"
	Unchanged Outcome = "unchanged"
	Conflict  Outcome = "conflict"
)

// Save writes m into the canonical root, deciding via Plan. A same-name file with
// differing content is refused (Conflict) unless force is set, so engram never
// silently overwrites a hand-edited canonical memory. Identical content is a
// no-op (Unchanged), and a difference confined to provenance backfills the
// stored file's empty provenance fields without needing force (Updated).
func Save(root string, m *schema.CanonicalMemory, force bool) (Outcome, string, error) {
	rendered, err := m.Render()
	if err != nil {
		return "", "", err
	}
	existing, path, found, err := Load(root, m.Name)
	if errors.Is(err, ErrWithheld) {
		// A file claims this name but discovery withheld it: refuse, even
		// with force, rather than overwrite or add another copy.
		return Conflict, path, nil
	}
	if err != nil {
		return "", "", err
	}
	if !found {
		p := filepath.Join(root, m.Name+".md")
		if _, serr := os.Lstat(p); serr == nil {
			// Something already occupies the path discovery did not index.
			return Conflict, p, nil
		}
		if err := writeAtomic(p, rendered); err != nil {
			return "", p, err
		}
		return Created, p, nil
	}
	outcome, merged := Plan(existing, m)
	switch outcome {
	case Unchanged:
		return Unchanged, path, nil
	case Updated:
		rendered, err = merged.Render()
		if err != nil {
			return "", path, err
		}
	default: // Conflict
		if !force {
			return Conflict, path, nil
		}
	}
	if err := writeAtomic(path, rendered); err != nil {
		return "", path, err
	}
	return Updated, path, nil
}

// Plan decides what saving cand over existing would do, without touching disk.
// It is the single decision rule shared by Save and by dry-run simulations, so a
// preview and the apply that follows it cannot disagree.
//
// Provenance records where a memory came from and is not load-bearing, so a
// difference confined to provenance is never a content conflict: empty stored
// provenance fields are backfilled from cand (Updated), a populated field keeps
// its stored value, and cand can never strip provenance. Any other difference is
// a Conflict, and the returned memory is nil.
func Plan(existing, cand *schema.CanonicalMemory) (Outcome, *schema.CanonicalMemory) {
	if existing == nil {
		return Created, cand
	}
	if sameRender(existing, cand) {
		return Unchanged, existing
	}
	content := *cand
	content.Provenance = existing.Provenance
	if !sameRender(existing, &content) {
		return Conflict, nil
	}
	merged := *existing
	merged.Provenance = fillProvenance(existing.Provenance, cand.Provenance)
	if merged.Provenance == existing.Provenance {
		return Unchanged, existing
	}
	return Updated, &merged
}

// Diff names the fields in which cand differs from existing, in a stable order:
// name, description, type, scope, applies_to, related, provenance, body.
func Diff(existing, cand *schema.CanonicalMemory) []string {
	var out []string
	add := func(differs bool, field string) {
		if differs {
			out = append(out, field)
		}
	}
	add(existing.Name != cand.Name, "name")
	add(existing.Description != cand.Description, "description")
	add(existing.Type != cand.Type, "type")
	add(existing.Scope != cand.Scope, "scope")
	add(!sameStrings(existing.AppliesTo.Cwd, cand.AppliesTo.Cwd) ||
		!sameStrings(existing.AppliesTo.Agents, cand.AppliesTo.Agents) ||
		!sameStrings(existing.AppliesTo.Hosts, cand.AppliesTo.Hosts), "applies_to")
	add(!sameStrings(existing.Related, cand.Related), "related")
	add(existing.Provenance != cand.Provenance, "provenance")
	add(existing.Body != cand.Body, "body")
	return out
}

// sameRender reports whether a and b serialize to identical canonical bytes.
func sameRender(a, b *schema.CanonicalMemory) bool {
	ar, err1 := a.Render()
	br, err2 := b.Render()
	return err1 == nil && err2 == nil && bytes.Equal(ar, br)
}

// fillProvenance returns stored with each empty field filled from cand, provided
// the two agree on where the memory came from. Origin and Source together
// identify the origin; backfilling Source under a different Origin would mint a
// hybrid (say a Codex origin carrying a Claude filename) that origin-based
// propagation and source-based migration would attribute to different harnesses.
// Differing populated origins therefore leave stored untouched.
func fillProvenance(stored, cand schema.Provenance) schema.Provenance {
	if stored.Origin != "" && cand.Origin != "" && stored.Origin != cand.Origin {
		return stored
	}
	fill := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	out := stored
	fill(&out.Origin, cand.Origin)
	fill(&out.Source, cand.Source)
	fill(&out.Session, cand.Session)
	fill(&out.Author, cand.Author)
	fill(&out.Created, cand.Created)
	fill(&out.Modified, cand.Modified)
	return out
}

// sameStrings compares two string slices, treating nil and empty as equal.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Delete removes the canonical memory named name from the root. A missing memory
// is not an error (delete is idempotent); the bool reports whether a file was
// actually removed.
func Delete(root, name string) (bool, error) {
	_, path, found, err := Load(root, name)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

// Replace overwrites the existing canonical memory named m.Name with m exactly,
// provenance included (Save keeps stored provenance on a provenance-only
// difference, so it cannot change an origin). The memory must exist; the caller
// holds the canonical lock.
func Replace(root string, m *schema.CanonicalMemory) (string, error) {
	_, path, found, err := Load(root, m.Name)
	if err != nil {
		return path, err
	}
	if !found {
		return "", fmt.Errorf("no canonical memory named %s", m.Name)
	}
	rendered, err := m.Render()
	if err != nil {
		return path, err
	}
	return path, writeAtomic(path, rendered)
}

// ErrWithheld reports that a canonical file claims the name but discovery
// withheld it (it failed to parse or validate, or another file shares its name).
// The memory is not absent, so no writer may treat it as free to create.
var ErrWithheld = errors.New("canonical memory withheld by discovery")

// Load returns the canonical memory named name, its file path, and whether it was
// found. A withheld file claiming the name is an ErrWithheld error, not "absent".
func Load(root, name string) (*schema.CanonicalMemory, string, bool, error) {
	located, perrs, err := discover.Locate(root)
	if err != nil {
		return nil, "", false, err
	}
	for _, pe := range perrs {
		if pe.Name == name || (pe.Name == "" && filepath.Base(pe.Path) == name+".md") {
			return nil, pe.Path, false, fmt.Errorf("%w: %s: %v", ErrWithheld, pe.Path, pe.Err)
		}
	}
	for _, l := range located {
		if l.Memory.Name == name {
			return l.Memory, l.Path, true, nil
		}
	}
	return nil, "", false, nil
}

// writeAtomic writes data via a temp file and rename, creating the directory as
// needed, so a crash leaves the target either fully prior or fully new.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".engram-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Package discover walks a canonical root and parses every memory file, keeping
// per-file parse failures separate so one malformed file never hides the rest.
package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// ForgottenDir is the directory under the canonical root that holds tombstones
// of forgotten memories (see package tombstone). Discovery never reads it.
const ForgottenDir = ".forgotten"

// ParseError records a single file that could not be read or parsed.
type ParseError struct {
	Path string
	// Name is the memory name when the file parsed but was withheld (it failed
	// validation or shares its name); empty when the file did not parse.
	Name string
	Err  error
}

// Located is a parsed memory together with the file it came from, so callers
// that must rewrite or locate a specific memory (share, remember) know its path.
type Located struct {
	Memory *schema.CanonicalMemory
	Path   string
}

// Locate recursively parses every *.md under root into a Located. It returns the
// located memories and a separate slice of per-file parse errors; a malformed
// file is reported, not fatal. A missing root yields empty results.
func Locate(root string) ([]Located, []ParseError, error) {
	var (
		located []Located
		perrs   []ParseError
	)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ForgottenDir && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			perrs = append(perrs, ParseError{Path: path, Err: rerr})
			return nil
		}
		m, perr := schema.Parse(data)
		if perr != nil {
			perrs = append(perrs, ParseError{Path: path, Err: perr})
			return nil
		}
		// Parsing is lenient; the schema is what every consumer (scope
		// matching, rendered commands, file names) relies on.
		if verr := m.Validate(); verr != nil {
			perrs = append(perrs, ParseError{Path: path, Name: m.Name, Err: verr})
			return nil
		}
		located = append(located, Located{Memory: m, Path: path})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("walk %s: %w", root, err)
	}
	located, dups := withholdDuplicates(located)
	return located, append(perrs, dups...), nil
}

// withholdDuplicates removes every memory whose name another file also claims,
// reporting each copy. Keeping any one would let a name-keyed mutation (remove,
// share) act on it and silently orphan the rest.
func withholdDuplicates(located []Located) ([]Located, []ParseError) {
	paths := map[string][]string{}
	for _, l := range located {
		paths[l.Memory.Name] = append(paths[l.Memory.Name], l.Path)
	}
	var (
		kept  []Located
		perrs []ParseError
	)
	for _, l := range located {
		if others := paths[l.Memory.Name]; len(others) > 1 {
			perrs = append(perrs, ParseError{Path: l.Path, Name: l.Memory.Name, Err: fmt.Errorf("duplicate name %q, claimed by %d files: %s", l.Memory.Name, len(others), strings.Join(others, ", "))})
			continue
		}
		kept = append(kept, l)
	}
	return kept, perrs
}

// Discover recursively parses every *.md under root into a CanonicalMemory,
// discarding source paths. A missing root yields empty results.
func Discover(root string) ([]*schema.CanonicalMemory, []ParseError, error) {
	located, perrs, err := Locate(root)
	if err != nil {
		return nil, perrs, err
	}
	mems := make([]*schema.CanonicalMemory, len(located))
	for i, l := range located {
		mems[i] = l.Memory
	}
	return mems, perrs, nil
}
